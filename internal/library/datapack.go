package library

// Data packs (spec D-201): one ZIP with setups, laps and telemetry for another SetupMan.
// Export only reads (the game's MoTeC files are read, never changed, D-160). Import puts setups
// in the library with the sender's initials (the External badge) and telemetry in SetupMan's own
// folder (telemetry\packs\<pack>); only with the user's tick does staging put setups in the game
// folder (D-07, Q-DP4). External laps never count as the user's own (Q-DP1, see store/packs.go).

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"setupman/internal/compare"
	"setupman/internal/datapack"
	"setupman/internal/folderbackup"
	"setupman/internal/game"
	"setupman/internal/history"
	"setupman/internal/motec"
	"setupman/internal/naming"
	"setupman/internal/store"
	"setupman/internal/telemetry"
	"setupman/internal/version"
)

// Telemetry choices per setup in a data pack.
const (
	PackTeleNone    = "none"
	PackTeleBest    = "best"    // the setup's fastest lap
	PackTeleSession = "session" // its fastest lap of each session
	PackTeleAll     = "all"     // every full lap with telemetry
	PackTeleChosen  = "chosen"  // every lap of the sessions picked in the window (PackSetupReq.Sessions)
)

// PackRequest is what the user ticked in the Create data pack window.
type PackRequest struct {
	Setups []PackSetupReq `json:"setups"`
	Laps   []PackLapReq   `json:"laps"`  // laps picked by hand (a session, Lap analysis)
	Extra  *bool          `json:"extra"` // the extra log with the telemetry (default on)
}

// PackSetupReq is one setup in the pack.
type PackSetupReq struct {
	ID        int64    `json:"id"`
	History   bool     `json:"history"`   // every version, not just the newest (Q-DP2)
	Note      bool     `json:"note"`      // the user's notes (off by default, as D-94)
	LapTimes  bool     `json:"lapTimes"`  // lap time summary
	Telemetry string   `json:"telemetry"` // PackTele…
	Sessions  []string `json:"sessions"`  // with PackTeleChosen: PackSession.Key of each session
}

// PackLapReq is one telemetry lap: the file that holds it and its lap number there.
type PackLapReq struct {
	FileID int64 `json:"fileId"`
	N      int   `json:"n"`
}

// PackPreview is what a pack would hold, for the window before saving.
type PackPreview struct {
	Setups   []PackPreviewSetup `json:"setups"`
	Laps     int                `json:"laps"`  // laps with telemetry
	Bytes    int64              `json:"bytes"` // about this big
	By       string             `json:"by"`    // the user's initials ("" = to be filled in first)
	Warnings []string           `json:"warnings"`
}

// PackPreviewSetup is one setup as the window lists it.
type PackPreviewSetup struct {
	ID        int64         `json:"id"`
	Game      string        `json:"game"`
	Short     string        `json:"short"`
	Name      string        `json:"name"`
	CarName   string        `json:"carName"`
	TrackName string        `json:"trackName"`
	Versions  int           `json:"versions"`
	Laps      int           `json:"laps"`     // laps with telemetry the choice gives
	LapsBest  int           `json:"lapsBest"` // and each other choice, so the window can show them
	LapsSess  int           `json:"lapsSession"`
	LapsAll   int           `json:"lapsAll"`
	Sessions  []PackSession `json:"sessionList"`        // the setup's sessions with telemetry, newest first
	External  string        `json:"external,omitempty"` // a setup from someone else's pack: can't be sent on
}

// PackSession is one session driven with a setup, to pick for the pack.
type PackSession struct {
	Key     string    `json:"key"`
	Started time.Time `json:"started"`
	Kind    string    `json:"kind"`
	Laps    int       `json:"laps"` // full laps with telemetry (invalid ones too)
	BestMs  int       `json:"bestMs"`
}

// ErrNoInitials: a pack names its sender by initials, so they must be set first.
var ErrNoInitials = errors.New("set your initials first (Settings → General → Naming standard): a data pack names its sender by initials")

// packLap is one lap chosen for the pack, with what the library knows about it.
type packLap struct {
	file store.TeleFile
	lap  store.TeleLap
}

// packChoice works out the setups and laps of a request.
type packChoice struct {
	setups   []*store.Setup
	reqs     map[int64]PackSetupReq
	laps     map[[2]int64]packLap // (file, n) → lap
	warnings []string
	counts   map[int64][4]int // setup → laps for best, session, all, and the chosen one
	sessions map[int64][]PackSession
}

func (l *Library) choosePack(req PackRequest) (*packChoice, error) {
	if l.db == nil {
		return nil, ErrNoHistory
	}
	c := &packChoice{reqs: map[int64]PackSetupReq{}, laps: map[[2]int64]packLap{}, counts: map[int64][4]int{}, sessions: map[int64][]PackSession{}}
	files := map[int64]*store.TeleFile{}
	fileOf := func(id int64) *store.TeleFile {
		if f, ok := files[id]; ok {
			return f
		}
		f, _ := l.db.TelemetryFile(id)
		files[id] = f
		return f
	}
	add := func(f *store.TeleFile, lp store.TeleLap) {
		c.laps[[2]int64{lp.FileID, int64(lp.N)}] = packLap{file: *f, lap: lp}
	}
	for _, r := range req.Setups {
		if _, dup := c.reqs[r.ID]; dup {
			continue
		}
		s, err := l.db.Setup(r.ID)
		if err != nil || s == nil || !s.DeletedAt.IsZero() {
			return nil, history.ErrNotFound
		}
		if s.Ext != "" {
			c.warnings = append(c.warnings, fmt.Sprintf("%s is %s's setup (from a data pack) and isn't sent on.", s.Name, s.Ext))
			continue
		}
		c.setups = append(c.setups, s)
		c.reqs[s.ID] = r
		rows, err := l.db.TelemetryTrackLaps(s.Game, s.Car, s.Track)
		if err != nil {
			return nil, err
		}
		var mine []store.TeleLapRow // valid laps, fastest first
		bySession := map[string][]store.TeleLapRow{}
		var order []PackSession
		for _, row := range rows { // fastest first
			if row.SetupID != s.ID || row.Ext != "" || !row.Timed || row.DistM < 100 {
				continue
			}
			key := fmt.Sprintf("%d-%d", row.SessionFile, row.SessionID)
			if _, ok := bySession[key]; !ok {
				order = append(order, PackSession{Key: key, Started: row.StartedAt, Kind: row.Kind})
			}
			bySession[key] = append(bySession[key], row) // a whole session: invalid laps too
			if row.Valid == nil || *row.Valid {
				mine = append(mine, row)
			}
		}
		for i := range order {
			list := bySession[order[i].Key]
			order[i].Laps = len(list)
			for _, row := range list {
				if (row.Valid == nil || *row.Valid) && (order[i].BestMs == 0 || row.LapMs < order[i].BestMs) {
					order[i].BestMs = row.LapMs
				}
			}
		}
		sort.Slice(order, func(i, j int) bool { return order[i].Started.After(order[j].Started) })
		c.sessions[s.ID] = order
		var picked []store.TeleLapRow
		for _, k := range r.Sessions {
			picked = append(picked, bySession[k]...)
		}
		best := mine[:min(1, len(mine))]
		var perSession []store.TeleLapRow
		seen := map[[2]int64]bool{}
		for _, row := range mine {
			k := [2]int64{row.SessionFile, row.SessionID}
			if !seen[k] {
				seen[k] = true
				perSession = append(perSession, row)
			}
		}
		chosen := map[string][]store.TeleLapRow{PackTeleBest: best, PackTeleSession: perSession, PackTeleAll: mine, PackTeleChosen: picked}[r.Telemetry]
		c.counts[s.ID] = [4]int{len(best), len(perSession), len(mine), len(chosen)}
		for _, row := range chosen {
			if f := fileOf(row.FileID); f != nil && !f.Missing {
				add(f, row.TeleLap)
			}
		}
	}
	for _, r := range req.Laps {
		f := fileOf(r.FileID)
		if f == nil || f.Missing {
			continue
		}
		if f.Ext != "" {
			c.warnings = append(c.warnings, fmt.Sprintf("Laps from %s's data pack aren't sent on.", f.Ext))
			continue
		}
		laps, err := l.db.TelemetryLaps(f.ID)
		if err != nil {
			return nil, err
		}
		for _, lp := range laps {
			if lp.N == r.N {
				if lp.Timed && lp.DistM >= 100 {
					add(f, lp)
				} else {
					c.warnings = append(c.warnings, fmt.Sprintf("Lap %d of the %s session has no telemetry to send.", lp.Lap, f.StartedAt.Format("2 Jan 15:04")))
				}
			}
		}
	}
	if len(c.setups) == 0 && len(c.laps) == 0 {
		return nil, errors.New("choose at least one setup or lap for the data pack")
	}
	return c, nil
}

// PreviewPack says what a pack would hold and about how big it would be.
func (l *Library) PreviewPack(req PackRequest) (*PackPreview, error) {
	c, err := l.choosePack(req)
	if err != nil {
		return nil, err
	}
	p := &PackPreview{Setups: []PackPreviewSetup{}, Warnings: c.warnings, By: l.Settings().Initials}
	if p.Warnings == nil {
		p.Warnings = []string{}
	}
	for _, s := range c.setups {
		ps := PackPreviewSetup{ID: s.ID, Game: s.Game, Name: s.Name, Versions: 1}
		if g := l.game(s.Game); g != nil {
			ps.Short = game.Short(g)
		}
		ps.CarName, ps.TrackName = l.packNames(s.Game, s.Car, s.Track)
		if c.reqs[s.ID].History {
			if vs, err := l.db.Versions(s.ID); err == nil {
				ps.Versions = len(vs)
			}
		}
		n := c.counts[s.ID]
		ps.LapsBest, ps.LapsSess, ps.LapsAll, ps.Laps = n[0], n[1], n[2], n[3]
		ps.Sessions = c.sessions[s.ID]
		if ps.Sessions == nil {
			ps.Sessions = []PackSession{}
		}
		p.Setups = append(p.Setups, ps)
		p.Bytes += 4 << 10 * int64(ps.Versions)
	}
	for _, pl := range c.laps {
		p.Laps++
		if pl.file.DurationMs > 0 { // the lap's share of the file, about half that in the ZIP
			p.Bytes += int64(float64(pl.file.Size) * float64(pl.lap.LapMs+1500) / float64(pl.file.DurationMs) * 0.5)
		}
	}
	return p, nil
}

// packSession is a run of consecutive laps of one file: one cut log in the pack.
type packSession struct {
	file store.TeleFile
	laps []store.TeleLap
}

func packRuns(laps map[[2]int64]packLap) []packSession {
	byFile := map[int64][]packLap{}
	for _, pl := range laps {
		byFile[pl.file.ID] = append(byFile[pl.file.ID], pl)
	}
	var out []packSession
	for _, list := range byFile {
		sort.Slice(list, func(i, j int) bool { return list[i].lap.N < list[j].lap.N })
		for i, pl := range list {
			if i == 0 || pl.lap.N != list[i-1].lap.N+1 {
				out = append(out, packSession{file: pl.file})
			}
			last := &out[len(out)-1]
			last.laps = append(last.laps, pl.lap)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if !a.file.StartedAt.Equal(b.file.StartedAt) {
			return a.file.StartedAt.Before(b.file.StartedAt)
		}
		return a.laps[0].N < b.laps[0].N
	})
	return out
}

// WritePack writes a data pack to w and returns a file name for it.
func (l *Library) WritePack(req PackRequest, w io.Writer) (string, error) {
	by := strings.ToUpper(strings.TrimSpace(l.Settings().Initials))
	if by == "" {
		return "", ErrNoInitials
	}
	c, err := l.choosePack(req)
	if err != nil {
		return "", err
	}
	zw := datapack.NewWriter(w)
	m := datapack.Manifest{ID: datapack.NewID(), Created: time.Now(), By: by, App: version.Number}

	// setups: the newest version, or every version (Q-DP2)
	setupKey := map[int64]string{}
	versionKey := map[int64]string{}
	for i, s := range c.setups {
		r := c.reqs[s.ID]
		key, err := l.db.ShareKey(s.ID, datapack.NewID)
		if err != nil {
			return "", err
		}
		setupKey[s.ID] = key
		vs, err := l.db.Versions(s.ID)
		if err != nil || len(vs) == 0 {
			continue
		}
		if !r.History {
			vs = vs[len(vs)-1:]
		}
		ext := ".json"
		if g := l.game(s.Game); g != nil {
			ext = game.Ext(g)
		}
		ps := datapack.Setup{Key: key, Game: s.Game, Car: s.Car, Track: s.Track, Name: s.Name, Session: s.Session, Meta: s.Meta}
		if r.Note {
			ps.Note = s.Note
		}
		for _, v := range vs {
			name := fmt.Sprintf("setups/%d/%d%s", i+1, v.Seq, ext)
			fw, err := zw.Create(name)
			if err != nil {
				return "", err
			}
			if _, err := fw.Write(v.Content); err != nil {
				return "", err
			}
			vk := fmt.Sprintf("%s-%d", key, v.ID)
			versionKey[v.ID] = vk
			ps.Versions = append(ps.Versions, datapack.Version{Key: vk, File: name, Saved: v.FileModified, Note: v.Note})
		}
		if r.LapTimes {
			if lt, err := l.SetupLapTimes(s.ID); err == nil {
				newest := vs[len(vs)-1].ID
				for _, vl := range lt.Versions {
					if vl.VersionID == newest && vl.Laps > 0 {
						ps.Laps = &datapack.LapSummary{Laps: vl.Laps, Clean: vl.Clean, BestMs: vl.BestMs, AvgMs: vl.AvgMs, FuelPerLap: vl.FuelPerLap}
					}
				}
			}
		}
		m.Setups = append(m.Setups, ps)
	}

	// laps: one cut log per run of consecutive laps
	withExtra := req.Extra == nil || *req.Extra
	usedNames := map[string]bool{}
	for _, run := range packRuns(c.laps) {
		ps, err := l.packSession(zw, run, withExtra, setupKey, versionKey, usedNames)
		if err != nil {
			return "", err
		}
		m.Sessions = append(m.Sessions, ps)
	}
	if err := zw.Close(m); err != nil {
		return "", err
	}
	return packFileName(by, c, l), nil
}

// packFileName: "ABC Imola SetupMan pack 2026-11-25.zip", or "… 3 tracks …".
func packFileName(by string, c *packChoice, l *Library) string {
	tracks := map[string]string{}
	add := func(gameID, car, track string) {
		_, tn := l.packNames(gameID, car, track)
		tracks[strings.ToLower(track)] = tn
	}
	for _, s := range c.setups {
		add(s.Game, s.Car, s.Track)
	}
	for _, pl := range c.laps {
		add(pl.file.Game, pl.file.Car, pl.file.Track)
	}
	where := fmt.Sprintf("%d tracks", len(tracks))
	if len(tracks) == 1 {
		for _, tn := range tracks {
			where = tn
		}
	}
	return cleanName(fmt.Sprintf("%s %s SetupMan pack %s", by, where, time.Now().Format("2006-01-02")), "SetupMan pack") + datapack.Ext
}

// packSession cuts one run of laps out of its MoTeC file (and the extra log) into the pack.
func (l *Library) packSession(zw *datapack.Writer, run packSession, withExtra bool, setupKey, versionKey map[int64]string, used map[string]bool) (datapack.Session, error) {
	f := run.file
	ps := datapack.Session{Key: datapack.NewID(), Game: f.Game, Car: f.Car, Track: f.Track, Kind: f.Kind}
	lf, err := l.teleOpen(&f)
	if err != nil {
		return ps, fmt.Errorf("%s: %w", f.Name, err)
	}
	defer lf.Close()
	var buf bytes.Buffer
	res, err := lf.Cut(&buf, run.laps[0].N, run.laps[len(run.laps)-1].N)
	if err != nil {
		return ps, fmt.Errorf("%s: %w", f.Name, err)
	}
	start := f.StartedAt.Add(time.Duration(res.Offset * float64(time.Second)))
	ps.Started = start
	// the session kind of the laps' own recorded session (one file can hold a race weekend, D-179)
	if sid := run.laps[0].SessionID; sid != 0 {
		if s, _ := l.db.Session(sid); s != nil {
			ps.Kind = s.Kind
		}
	}
	// ACC's file name, so MoTeC i2 users recognise it and SetupMan reads car and track from it
	track := f.Track
	if i := strings.IndexByte(f.Name, '-'); i > 0 && strings.EqualFold(f.Name[:i], f.Track) {
		track = f.Name[:i]
	}
	saved := start.Add(time.Duration(res.Duration * float64(time.Second))).Truncate(time.Second)
	name := ""
	for {
		name = fmt.Sprintf("telemetry/%s-%s-%d-%s.ld", track, f.Car, len(run.laps), saved.Format("2006.01.02-15.04.05"))
		if !used[name] {
			break
		}
		saved = saved.Add(time.Second)
	}
	used[name] = true
	if err := writePackFile(zw, name, buf.Bytes()); err != nil {
		return ps, err
	}
	var ldx bytes.Buffer
	motec.WriteLDX(&ldx, res.Crossings)
	if err := writePackFile(zw, name+"x", ldx.Bytes()); err != nil {
		return ps, err
	}
	ps.Log = &datapack.Log{LD: name, LDX: name + "x"}

	all, _ := l.db.TelemetryLaps(f.ID)
	for i, lp := range run.laps {
		pl := datapack.Lap{N: lp.Lap, TimeMs: lp.LapMs, Valid: lp.Valid == nil || *lp.Valid, Full: lp.Complete,
			Fuel: lp.FuelUsed, Note: lp.Note, LogLap: 1 + i}
		if pl.N == 0 {
			pl.N = lp.N
		}
		if lp.HasTemps {
			pl.Air, pl.Road = lp.Air, lp.Road
		}
		setupID := lp.LapSetupID
		if setupID == 0 {
			setupID = f.SetupID
		}
		pl.Setup = setupKey[setupID] // only when that setup is in the pack too
		if pl.Setup != "" {
			if vid := lapVersion(l, lp, f); vid != 0 {
				pl.SetupV = versionKey[vid]
			}
		}
		ps.Laps = append(ps.Laps, pl)
	}

	if withExtra {
		idx := lapIndex(all, run.laps[0].N)
		if idx >= 0 {
			if x := l.extraLog(f.Game, lapExtraSession(all, idx, f.SessionID)); x != nil {
				first, ok1 := x.FindLap(run.laps[0].LapMs)
				last, ok2 := x.FindLap(run.laps[len(run.laps)-1].LapMs)
				if ok1 && ok2 && last.To > first.From {
					var xb bytes.Buffer
					if err := telemetry.WriteExtra(&xb, x.Cut(first.From, last.To)); err == nil {
						xn := "extra/" + strings.TrimSuffix(strings.TrimPrefix(name, "telemetry/"), ".ld") + ".smx.gz"
						if err := writePackFile(zw, xn, xb.Bytes()); err != nil {
							return ps, err
						}
						ps.Log.Extra = xn
					}
				}
			}
		}
	}
	return ps, nil
}

// lapVersion: the setup version a lap was driven with (0 = not known).
func lapVersion(l *Library, lp store.TeleLap, f store.TeleFile) int64 {
	if lp.LapID != 0 && lp.SessionID != 0 {
		if laps, err := l.db.LapsForSession(lp.SessionID); err == nil {
			for _, rl := range laps {
				if rl.ID == lp.LapID {
					return rl.VersionID
				}
			}
		}
	}
	return f.VersionID
}

func writePackFile(zw *datapack.Writer, name string, data []byte) error {
	fw, err := zw.Create(name)
	if err != nil {
		return err
	}
	_, err = fw.Write(data)
	return err
}

// ---------- import ----------

// PackContents is what an opened pack holds, for the import window.
type PackContents struct {
	ID       string            `json:"id"`
	By       string            `json:"by"`
	Created  time.Time         `json:"created"`
	App      string            `json:"app"`
	Known    bool              `json:"known"` // imported before
	Setups   []PackItemSetup   `json:"setups"`
	Sessions []PackItemSession `json:"sessions"`
	Laps     int               `json:"laps"`
	CanStage bool              `json:"canStage"` // folder management is on for every game in it
	Problems []string          `json:"problems"`
}

// PackItemSetup is one setup of a pack and what importing does with it.
type PackItemSetup struct {
	Name      string `json:"name"`
	Short     string `json:"short"`
	CarName   string `json:"carName"`
	TrackName string `json:"trackName"`
	Versions  int    `json:"versions"`
	Result    string `json:"result"` // "new", "update" (new versions of a setup from them), "present", "skip"
	Reason    string `json:"reason,omitempty"`
	SetupID   int64  `json:"setupId,omitempty"`
}

// PackItemSession is one session of a pack.
type PackItemSession struct {
	CarName   string    `json:"carName"`
	TrackName string    `json:"trackName"`
	Kind      string    `json:"kind"`
	Started   time.Time `json:"started"`
	Laps      int       `json:"laps"`
	BestMs    int       `json:"bestMs"`
	Telemetry bool      `json:"telemetry"`
	Extra     bool      `json:"extra"`
}

// packPlan is what importing a pack would do with each setup.
type packPlan struct {
	item     PackItemSetup
	existing *store.Setup // the setup it adds to or already is
	fields   [][]byte     // the versions' files
	fps      []string
	skip     bool
}

func (l *Library) planPack(p *datapack.Pack) ([]packPlan, error) {
	var out []packPlan
	for _, s := range p.Manifest.Setups {
		pl := packPlan{item: PackItemSetup{Name: s.Name, Versions: len(s.Versions)}}
		g := l.game(s.Game)
		if g != nil {
			pl.item.Short = game.Short(g)
			pl.item.CarName, pl.item.TrackName = l.packNames(s.Game, s.Car, s.Track)
		}
		skip := func(reason string) {
			pl.item.Result, pl.item.Reason, pl.skip = "skip", reason, true
		}
		switch {
		case g == nil:
			skip("it is for a game SetupMan doesn't support here (" + s.Game + ")")
		case game.ReadOnlyReason(g) != "":
			skip("SetupMan can't add setups for " + game.Short(g))
		case s.Car == "" || s.Track == "" || strings.ContainsAny(s.Car+s.Track, `\/:*?"<>|`) || len(s.Versions) == 0:
			skip("the pack doesn't say which car and track it is for")
		}
		if pl.skip {
			out = append(out, pl)
			continue
		}
		for _, v := range s.Versions {
			data, err := p.ReadFile(v.File)
			if err != nil {
				return nil, err
			}
			fields, err := g.ParseSetup(data)
			if err != nil {
				skip("a setup file inside is damaged")
				break
			}
			pl.fields = append(pl.fields, data)
			pl.fps = append(pl.fps, compare.Fingerprint(fields))
		}
		if pl.skip {
			out = append(out, pl)
			continue
		}
		if ex, err := l.db.SetupByPackKey(s.Key); err != nil {
			return nil, err
		} else if ex != nil {
			pl.existing = ex
			have := map[string]bool{}
			if vs, err := l.db.Versions(ex.ID); err == nil {
				for _, v := range vs {
					have[v.Fingerprint] = true
				}
			}
			pl.item.Result, pl.item.SetupID = "present", ex.ID
			for _, fp := range pl.fps {
				if !have[fp] {
					pl.item.Result = "update"
				}
			}
			out = append(out, pl)
			continue
		}
		newest := pl.fps[len(pl.fps)-1]
		same, err := l.db.SetupsForTrack(s.Game, strings.ToLower(s.Car), strings.ToLower(s.Track))
		if err != nil {
			return nil, err
		}
		for i := range same {
			if v, _ := l.db.LatestVersion(same[i].ID); v != nil && v.Fingerprint == newest {
				pl.existing = &same[i]
				pl.item.Result, pl.item.SetupID = "present", same[i].ID
				pl.item.Reason = "already in your library as " + same[i].Name
				break
			}
		}
		if pl.existing == nil {
			pl.item.Result = "new"
		}
		out = append(out, pl)
	}
	return out, nil
}

// InspectPack opens a pack and says what importing it would do (nothing is changed).
func (l *Library) InspectPack(r io.ReaderAt, size int64) (*PackContents, error) {
	if l.db == nil {
		return nil, ErrNoHistory
	}
	p, err := datapack.Open(r, size)
	if err != nil {
		return nil, err
	}
	plans, err := l.planPack(p)
	if err != nil {
		return nil, err
	}
	m := p.Manifest
	c := &PackContents{ID: m.ID, By: m.By, Created: m.Created, App: m.App, Setups: []PackItemSetup{}, Sessions: []PackItemSession{}, Problems: []string{}}
	c.Known, _ = l.db.PackKnown(m.ID)
	c.CanStage = true
	for _, pl := range plans {
		c.Setups = append(c.Setups, pl.item)
	}
	for _, s := range m.Setups {
		if !l.managed(s.Game) {
			c.CanStage = false
		}
	}
	if len(m.Setups) == 0 {
		c.CanStage = false
	}
	for _, s := range m.Sessions {
		is := PackItemSession{Kind: s.Kind, Started: s.Started, Laps: len(s.Laps), Telemetry: s.Log != nil, Extra: s.Log != nil && s.Log.Extra != ""}
		is.CarName, is.TrackName = l.packNames(s.Game, s.Car, s.Track)
		for _, lp := range s.Laps {
			if lp.Valid && lp.Full && (is.BestMs == 0 || lp.TimeMs < is.BestMs) {
				is.BestMs = lp.TimeMs
			}
		}
		if l.game(s.Game) == nil {
			c.Problems = append(c.Problems, "Laps for a game SetupMan doesn't support here are left out ("+s.Game+").")
			continue
		}
		c.Laps += len(s.Laps)
		c.Sessions = append(c.Sessions, is)
	}
	return c, nil
}

// PackImportResult says what an import did.
type PackImportResult struct {
	Setups   int            `json:"setups"`  // new external setups
	Updated  int            `json:"updated"` // setups that got new versions
	Present  int            `json:"present"`
	Laps     int            `json:"laps"`
	Sessions int            `json:"sessions"`
	Staged   int            `json:"staged"` // put in the game folder (Q-DP4)
	Items    []ImportItem   `json:"items"`
	Messages []string       `json:"messages"`
	Track    *PackTrackLink `json:"track,omitempty"` // where to look at the laps
}

// PackTrackLink points to the Telemetry page of the pack's laps.
type PackTrackLink struct {
	Game  string `json:"game"`
	Track string `json:"track"`
}

var packIDPattern = regexp.MustCompile(`^[0-9a-f]{24}$`)

// PacksDir is where the telemetry of imported packs is kept (in SetupMan's data folder).
func (l *Library) PacksDir() string {
	if l.TelemetryDir == "" {
		return ""
	}
	return filepath.Join(l.TelemetryDir, "packs")
}

// ImportPack imports a pack into the library. stage = also put its setups in the game folder
// (only with folder management on; a folder backup is made first, Q-DP4).
func (l *Library) ImportPack(r io.ReaderAt, size int64, stage bool) (*PackImportResult, error) {
	if l.db == nil {
		return nil, ErrNoHistory
	}
	p, err := datapack.Open(r, size)
	if err != nil {
		return nil, err
	}
	m := p.Manifest
	if !packIDPattern.MatchString(m.ID) {
		return nil, datapack.ErrDamaged
	}
	if known, _ := l.db.PackKnown(m.ID); known {
		return nil, errors.New("this data pack is already in your library (Settings → Data packs)")
	}
	by := strings.ToUpper(strings.TrimSpace(m.By))
	if by == "" || len([]rune(by)) > 8 {
		by = "?"
	}
	plans, err := l.planPack(p)
	if err != nil {
		return nil, err
	}
	res := &PackImportResult{Items: []ImportItem{}, Messages: []string{}}
	setupID := map[string]int64{}   // sender's setup key → setup here
	versionID := map[string]int64{} // sender's version key → version here
	var stageIDs []int64
	when := m.Created.Format("2 Jan 2006")
	for i, pl := range plans {
		s := m.Setups[i]
		if pl.skip {
			res.Items = append(res.Items, ImportItem{Name: s.Name, Result: "skipped", Reason: pl.item.Reason})
			continue
		}
		target := pl.existing
		if target == nil {
			names := map[string]bool{}
			if same, err := l.db.SetupsForTrack(s.Game, strings.ToLower(s.Car), strings.ToLower(s.Track)); err == nil {
				for _, x := range same {
					names[strings.ToLower(x.Name)] = true
				}
			}
			name := cleanName(s.Name, "Setup from "+by)
			final := name
			for k := 2; names[strings.ToLower(final)]; k++ {
				final = fmt.Sprintf("%s (%d)", name, k)
			}
			note := fmt.Sprintf("From %s's data pack (%s).", by, when)
			if s.Laps != nil && s.Laps.Laps > 0 {
				note += " Their lap times: " + lapSummary(LapStats{Laps: s.Laps.Laps, Clean: s.Laps.Clean, BestMs: s.Laps.BestMs, AvgMs: s.Laps.AvgMs, FuelPerLap: s.Laps.FuelPerLap, FuelLaps: boolInt(s.Laps.FuelPerLap > 0)}) + "."
			}
			if s.Note != "" {
				note += "\nTheir notes: " + s.Note
			}
			session := s.Session
			if session != naming.SessionQ && session != naming.SessionR && session != naming.SessionEndu {
				session = ""
			}
			target = &store.Setup{Game: s.Game, Car: strings.ToLower(s.Car), Track: strings.ToLower(s.Track), Name: final, Meta: s.Meta, MetaManual: true,
				Session: session, SessionManual: session != "", Note: note, Ext: by, Pack: m.ID, PackKey: s.Key}
			if err := l.db.InsertSetup(target); err != nil {
				return nil, err
			}
			res.Setups++
			res.Items = append(res.Items, ImportItem{Name: final, SetupID: target.ID, Result: "imported"})
			l.db.InsertEvent(store.Event{Game: s.Game, Kind: "imported-file", SetupID: target.ID, Details: map[string]any{"name": final, "from": by}})
		}
		have := map[string]int64{}
		if vs, err := l.db.Versions(target.ID); err == nil {
			for _, v := range vs {
				have[v.Fingerprint] = v.ID
			}
		}
		added := 0
		for k, v := range s.Versions {
			if id, ok := have[pl.fps[k]]; ok {
				versionID[v.Key] = id
				continue
			}
			nv := &store.Version{SetupID: target.ID, Fingerprint: pl.fps[k], Content: pl.fields[k], FileModified: v.Saved,
				Source: "imported", Note: v.Note, Pack: m.ID}
			if nv.Note == "" {
				nv.Note = "From " + by + "'s data pack"
			}
			if err := l.db.InsertVersion(nv); err != nil {
				return nil, err
			}
			have[pl.fps[k]] = nv.ID
			versionID[v.Key] = nv.ID
			added++
		}
		setupID[s.Key] = target.ID
		switch {
		case pl.existing != nil && added > 0:
			res.Updated++
			res.Items = append(res.Items, ImportItem{Name: target.Name, SetupID: target.ID, Result: "imported", Reason: fmt.Sprintf("%d new version%s", added, plural(added, "", "s"))})
		case pl.existing != nil:
			res.Present++
			res.Items = append(res.Items, ImportItem{Name: s.Name, SetupID: target.ID, Result: "skipped", Reason: firstNonEmpty(pl.item.Reason, "already in your library")})
		}
		if stage && (pl.existing == nil || added > 0) && target.Ext != "" {
			stageIDs = append(stageIDs, target.ID)
		}
	}

	// sessions and their telemetry
	dir := ""
	if pd := l.PacksDir(); pd != "" {
		dir = filepath.Join(pd, m.ID)
	}
	games := map[string]bool{}
	for _, s := range m.Sessions {
		if l.game(s.Game) == nil || len(s.Laps) == 0 {
			continue
		}
		n, err := l.importPackSession(p, m.ID, by, dir, s, setupID, versionID)
		if err != nil {
			log.Printf("data pack %s: session %s: %v", m.ID, s.Started.Format(time.DateTime), err)
			res.Messages = append(res.Messages, fmt.Sprintf("A session (%s) could not be added: %v", s.Started.Format("2 Jan 15:04"), err))
			continue
		}
		res.Sessions++
		res.Laps += n
		games[s.Game] = true
		if res.Track == nil && s.Log != nil {
			res.Track = &PackTrackLink{Game: s.Game, Track: strings.ToLower(s.Track)}
		}
	}
	for g := range games {
		if err := l.db.RefreshTelemetryFull(g); err != nil {
			log.Printf("data pack %s: %v", m.ID, err)
		}
		l.publish(Event{Type: "telemetry", Game: g})
	}
	sum := l.packSummary(res, m)
	if err := l.db.SavePack(store.Pack{ID: m.ID, By: by, Created: m.Created, Imported: time.Now(), App: m.App, Summary: sum}); err != nil {
		return nil, err
	}

	if len(stageIDs) > 0 {
		staged, msg := l.stagePack(stageIDs)
		res.Staged = staged
		if msg != "" {
			res.Messages = append(res.Messages, msg)
		}
	}
	log.Printf("data pack %s from %s imported: %d new setups, %d updated, %d sessions, %d laps, %d put in the game folder",
		m.ID, by, res.Setups, res.Updated, res.Sessions, res.Laps, res.Staged)
	l.publish(Event{Type: "history-changed"})
	return res, nil
}

// stagePack puts imported setups in the game folder, after a folder backup per game (Q-DP4).
func (l *Library) stagePack(ids []int64) (int, string) {
	byGame := map[string][]int64{}
	for _, id := range ids {
		if s, _ := l.db.Setup(id); s != nil && l.managed(s.Game) {
			byGame[s.Game] = append(byGame[s.Game], id)
		}
	}
	n := 0
	var msgs []string
	for gameID, list := range byGame {
		if _, err := l.createBackup(gameID, folderbackup.ReasonDataPack); err != nil {
			msgs = append(msgs, "The setups were not put in the game folder: the folder backup failed ("+err.Error()+").")
			continue
		}
		res, out, err := l.StageMany(list)
		if err != nil {
			msgs = append(msgs, "The setups were not put in the game folder: "+err.Error())
			continue
		}
		for _, r := range out {
			if r.Error == "" {
				n++
			} else {
				msgs = append(msgs, r.Name+": "+r.Error)
			}
		}
		if res.Queued {
			msgs = append(msgs, ErrQueued.Error())
		}
	}
	return n, strings.Join(msgs, " ")
}

// importPackSession adds one session of a pack: a recorded session with its laps (marked with the
// sender's initials), its telemetry file in SetupMan's own folder and its extra log.
func (l *Library) importPackSession(p *datapack.Pack, packID, by, dir string, s datapack.Session, setupID, versionID map[string]int64) (int, error) {
	car, track := strings.ToLower(s.Car), strings.ToLower(s.Track)
	if car == "" || track == "" || strings.ContainsAny(car+track, `\/:*?"<>|`) {
		return 0, errors.New("the pack doesn't say which car and track")
	}
	sess := &store.Session{Game: s.Game, Car: car, Track: track, Kind: s.Kind, StartedAt: s.Started, Match: "auto", Ext: by, Pack: packID}
	for _, lp := range s.Laps {
		if id := setupID[lp.Setup]; id != 0 {
			sess.SetupID, sess.VersionID = id, versionID[lp.SetupV]
			break
		}
	}
	if sess.SetupID == 0 {
		sess.Match = "none"
	}
	if err := l.db.InsertSession(sess); err != nil {
		return 0, err
	}
	at := s.Started.Add(time.Second) // the cut log starts a second before the first lap
	lapIDs := map[int]int64{}
	for _, lp := range s.Laps {
		at = at.Add(time.Duration(lp.TimeMs) * time.Millisecond)
		rl := &store.Lap{SessionID: sess.ID, SetupID: setupID[lp.Setup], VersionID: versionID[lp.SetupV], At: at, LapMs: lp.TimeMs,
			Valid: lp.Valid, FuelUsed: lp.Fuel, Air: lp.Air, Road: lp.Road, HasTemps: lp.Air != 0 || lp.Road != 0}
		if err := l.db.InsertLap(rl); err != nil {
			return 0, err
		}
		if lp.LogLap > 0 {
			lapIDs[lp.LogLap] = rl.ID
		}
	}
	if s.Log == nil || dir == "" {
		return len(s.Laps), nil
	}
	base := filepath.Base(s.Log.LD)
	if _, _, _, ok := telemetry.ParseName(base); !ok {
		return len(s.Laps), errors.New("its telemetry file has an unknown name")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return len(s.Laps), err
	}
	for _, name := range []string{s.Log.LD, s.Log.LDX} {
		if name == "" {
			continue
		}
		data, err := p.ReadFile(name)
		if err != nil {
			return len(s.Laps), err
		}
		if err := os.WriteFile(filepath.Join(dir, filepath.Base(name)), data, 0o644); err != nil {
			return len(s.Laps), err
		}
	}
	f, laps, err := telemetry.IndexFile(dir, base, s.Game)
	if err != nil {
		return len(s.Laps), fmt.Errorf("its telemetry can't be read: %w", err)
	}
	f.Name = filepath.Base(dir) + "/" + base
	if err := l.db.SaveTelemetryFile(f, laps); err != nil {
		return len(s.Laps), err
	}
	if err := l.db.MarkTelemetryExternal(f.ID, by, packID); err != nil {
		return len(s.Laps), err
	}
	if err := l.db.LinkTelemetry(f.ID, sess.ID, lapIDs); err != nil {
		return len(s.Laps), err
	}
	for _, lp := range s.Laps {
		if lp.LogLap == 0 {
			continue
		}
		if lp.N > 0 {
			l.db.SetPackLapNumber(f.ID, lp.LogLap, lp.N) // "lap 8", as in the sender's session
		}
		if lp.Note != "" {
			l.db.SetTelemetryLapNote(f.ID, lp.LogLap, lp.Note)
		}
	}
	if s.Log.Extra != "" && l.TelemetryDir != "" {
		if data, err := p.ReadFile(s.Log.Extra); err == nil {
			if err := l.writePackExtra(data, s.Game, sess.ID); err != nil {
				log.Printf("data pack %s: extra log: %v", packID, err)
			}
		}
	}
	return len(s.Laps), nil
}

// writePackExtra stores a pack's extra log as the extra log of the imported session.
func (l *Library) writePackExtra(data []byte, gameID string, sessionID int64) error {
	tmp, err := os.CreateTemp("", "setupman-pack-*.smx.gz")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	tmp.Write(data)
	tmp.Close()
	x, err := telemetry.ReadExtra(tmp.Name())
	if err != nil {
		return err
	}
	x.Header.Game, x.Header.Session = gameID, sessionID
	if err := os.MkdirAll(l.TelemetryDir, 0o755); err != nil {
		return err
	}
	out, err := os.Create(telemetry.ExtraPath(l.TelemetryDir, gameID, sessionID))
	if err != nil {
		return err
	}
	if err := telemetry.WriteExtra(out, x); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func (l *Library) packSummary(r *PackImportResult, m datapack.Manifest) string {
	var parts []string
	if n := r.Setups + r.Updated + r.Present; n > 0 {
		parts = append(parts, fmt.Sprintf("%d setup%s", n, plural(n, "", "s")))
	}
	if r.Laps > 0 {
		parts = append(parts, fmt.Sprintf("%d lap%s", r.Laps, plural(r.Laps, "", "s")))
	}
	tracks := map[string]bool{}
	var names []string
	add := func(gameID, car, track string) {
		if !tracks[strings.ToLower(track)] {
			tracks[strings.ToLower(track)] = true
			_, tn := l.packNames(gameID, car, track)
			names = append(names, tn)
		}
	}
	for _, s := range m.Setups {
		add(s.Game, s.Car, s.Track)
	}
	for _, s := range m.Sessions {
		add(s.Game, s.Car, s.Track)
	}
	out := strings.Join(parts, ", ")
	if len(names) > 0 && len(names) <= 3 {
		out += " (" + strings.Join(names, ", ") + ")"
	}
	return out
}

// Packs lists the imported data packs.
func (l *Library) Packs() ([]store.Pack, error) {
	if l.db == nil {
		return []store.Pack{}, nil
	}
	return l.db.Packs()
}

// RemovePack removes everything an imported pack brought (after the user confirmed).
func (l *Library) RemovePack(id string) (store.PackRemoval, error) {
	if l.db == nil {
		return store.PackRemoval{}, ErrNoHistory
	}
	if !packIDPattern.MatchString(id) {
		return store.PackRemoval{}, history.ErrNotFound
	}
	games := map[string]bool{}
	r, err := l.db.RemovePack(id)
	if err != nil {
		return r, err
	}
	for sid, g := range r.Sessions {
		games[g] = true
		if l.TelemetryDir != "" {
			os.Remove(telemetry.ExtraPath(l.TelemetryDir, g, sid))
		}
	}
	if pd := l.PacksDir(); pd != "" {
		os.RemoveAll(filepath.Join(pd, id)) // SetupMan's own folder for this pack
	}
	for g := range games {
		l.publish(Event{Type: "telemetry", Game: g})
	}
	l.publish(Event{Type: "history-changed"})
	log.Printf("data pack %s removed: %d setups, %d laps (%d setups kept: in the game folder)", id, r.Setups, r.Laps, r.Kept)
	return r, nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// packNames: readable car and track names, also for cars and tracks not in this PC's folder.
func (l *Library) packNames(gameID, car, track string) (string, string) {
	if l.game(gameID) == nil {
		return car, track
	}
	cn, _, _, tn := l.teleNames(gameID, car, track)
	return cn, tn
}

// BestLap is the user's fastest lap of one car on one track, for a best-laps pack (D-201): a base
// lap new users can compare their laps with.
type BestLap struct {
	FileID    int64     `json:"fileId"`
	N         int       `json:"n"`
	Lap       int       `json:"lap"`
	LapMs     int       `json:"lapMs"`
	Car       string    `json:"car"`
	CarName   string    `json:"carName"`
	Class     string    `json:"class"`
	Track     string    `json:"track"`
	TrackName string    `json:"trackName"`
	Started   time.Time `json:"started"`
	Extra     bool      `json:"extra"` // SetupMan's extra log goes along
}

// BestLaps returns the user's own fastest valid full lap with telemetry for every car on every
// track of a game, by track and then fastest first.
func (l *Library) BestLaps(gameID string) ([]BestLap, error) {
	out := []BestLap{}
	if l.db == nil || l.game(gameID) == nil {
		return out, ErrNoHistory
	}
	rows, err := l.db.TelemetryTrackLaps(gameID, "", "")
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, r := range rows { // fastest first
		k := strings.ToLower(r.Car + "|" + r.Track)
		if seen[k] || r.Ext != "" || !r.Timed || r.DistM < 100 || (r.Valid != nil && !*r.Valid) {
			continue
		}
		seen[k] = true
		b := BestLap{FileID: r.FileID, N: r.N, Lap: r.Lap, LapMs: r.LapMs, Car: r.Car, Track: strings.ToLower(r.Track), Started: r.StartedAt}
		b.CarName, _, b.Class, b.TrackName = l.teleNames(gameID, r.Car, r.Track)
		if r.SessionID != 0 && l.TelemetryDir != "" {
			_, err := os.Stat(telemetry.ExtraPath(l.TelemetryDir, gameID, r.SessionID))
			b.Extra = err == nil
		}
		out = append(out, b)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].TrackName != out[j].TrackName {
			return out[i].TrackName < out[j].TrackName
		}
		return out[i].LapMs < out[j].LapMs
	})
	return out, nil
}
