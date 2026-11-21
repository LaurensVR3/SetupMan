package server

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"setupman/internal/folderbackup"
	"setupman/internal/game"
	"setupman/internal/games/acc"
	"setupman/internal/library"
	"setupman/internal/logos"
	"setupman/internal/settings"
	histstore "setupman/internal/store"
)

// testGame is ACC without real detection: never "installed", so tests control visibility.
type testGame struct{ *acc.Game }

func (testGame) Detect() game.Detection { return game.Detection{} }

func newTest(t *testing.T) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	base, err := os.ReadFile("../games/acc/testdata/base.json")
	if err != nil {
		t.Fatal(err)
	}
	setups := filepath.Join(dir, "Setups")
	track := filepath.Join(setups, "ferrari_296_gt3", "Spa")
	os.MkdirAll(track, 0o755)
	os.WriteFile(filepath.Join(track, "a.json"), base, 0o644)
	os.WriteFile(filepath.Join(track, "b.json"), base, 0o644)

	store := settings.NewStore(filepath.Join(dir, "cfg", "settings.json"))
	db, err := histstore.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	lib := library.New([]game.Game{testGame{acc.New()}}, store, db)
	lib.Backups = &folderbackup.Store{Dir: filepath.Join(dir, "backups")}
	lib.Apply()
	t.Cleanup(lib.Stop)
	return New(lib, store, logos.New(filepath.Join(dir, "logos"))), setups
}

func do(t *testing.T, s *Server, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, "http://127.0.0.1:8765"+path, strings.NewReader(body))
	req.RemoteAddr = "127.0.0.1:50000" // from this PC
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return rec
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return v
}

func TestStaticFiles(t *testing.T) {
	s, _ := newTest(t)
	for _, f := range []string{"/", "/app.js", "/style.css", "/vendor/vue.esm-browser.prod.js"} {
		if rec := do(t, s, "GET", f, ""); rec.Code != 200 {
			t.Errorf("%s: %d", f, rec.Code)
		}
	}
}

func TestProtections(t *testing.T) {
	s, _ := newTest(t)
	req := httptest.NewRequest("GET", "http://evil.example.com/api/games", nil)
	req.RemoteAddr = "127.0.0.1:50000"
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("foreign host: %d", rec.Code)
	}
	for host, want := range map[string]int{"setupman.localhost": 200, "SetupMan.localhost:8765": 200, "evil.setupman.localhost": 403} {
		req = httptest.NewRequest("GET", "http://"+host+"/api/games", nil)
		req.RemoteAddr = "127.0.0.1:50000"
		rec = httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Errorf("host %s: %d, want %d", host, rec.Code, want)
		}
	}
	req = httptest.NewRequest("PUT", "http://127.0.0.1/api/settings", strings.NewReader("theme=dark"))
	req.RemoteAddr = "127.0.0.1:50000"
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Errorf("form post: %d", rec.Code)
	}
}

func TestSettingsShowGameAndBrowsePages(t *testing.T) {
	s, setups := newTest(t)

	games := decode[struct{ Games []library.GameSummary }](t, do(t, s, "GET", "/api/games", ""))
	if len(games.Games) != 1 || games.Games[0].Visible {
		t.Fatalf("game should start hidden: %+v", games.Games)
	}

	check := decode[library.FolderCheck](t, do(t, s, "POST", "/api/games/acc/check-folder", `{"folder":`+jsonString(setups)+`}`))
	if !check.OK || !strings.Contains(check.Message, "2 setups") {
		t.Errorf("check folder: %+v", check)
	}
	bad := decode[library.FolderCheck](t, do(t, s, "POST", "/api/games/acc/check-folder", `{"folder":`+jsonString(filepath.Dir(setups))+`}`))
	if bad.OK {
		t.Errorf("parent folder should not look like a setups folder: %+v", bad)
	}

	body := `{"theme":"dark","threshold":90,"downloadLogos":false,"games":{"acc":{"visibility":"show","folder":` + jsonString(setups) + `}}}`
	if rec := do(t, s, "PUT", "/api/settings", body); rec.Code != 200 {
		t.Fatalf("put settings: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(t, s, "PUT", "/api/settings", `{"theme":"pink","threshold":90}`); rec.Code != 400 {
		t.Errorf("invalid theme accepted: %d", rec.Code)
	}

	games = decode[struct{ Games []library.GameSummary }](t, do(t, s, "GET", "/api/games", ""))
	if g := games.Games[0]; !g.Visible || g.Setups != 2 || g.Duplicates != 1 {
		t.Fatalf("after settings: %+v", g)
	}
	gd := decode[library.GameDetail](t, do(t, s, "GET", "/api/games/acc", ""))
	if len(gd.Classes) != 1 || gd.Classes[0].Cars[0].Name != "Ferrari 296 GT3" {
		t.Errorf("game detail: %+v", gd)
	}
	cd := decode[library.CarDetail](t, do(t, s, "GET", "/api/games/acc/cars/ferrari_296_gt3", ""))
	if len(cd.Tracks) != 1 || cd.Tracks[0].Name != "Spa-Francorchamps" {
		t.Errorf("car detail: %+v", cd)
	}
	td := decode[library.TrackDetail](t, do(t, s, "GET", "/api/games/acc/cars/ferrari_296_gt3/tracks/spa", ""))
	if len(td.Setups) != 2 || len(td.Pairs) != 1 || td.Threshold != 90 || len(td.Setups[0].Fields) == 0 {
		t.Errorf("track detail: %+v", td)
	}
	if rec := do(t, s, "GET", "/api/games/acc/cars/nope", ""); rec.Code != 404 {
		t.Errorf("unknown car: %d", rec.Code)
	}
	if rec := do(t, s, "GET", "/api/logos/BMW", ""); rec.Code != 404 {
		t.Errorf("logos switched off and not cached: want 404, got %d", rec.Code)
	}
}

func TestEventsStream(t *testing.T) {
	s, setups := newTest(t)
	if rec := do(t, s, "PUT", "/api/settings", `{"theme":"system","threshold":95,"games":{"acc":{"visibility":"show","folder":`+jsonString(setups)+`}}}`); rec.Code != 200 {
		t.Fatalf("show game: %d %s", rec.Code, rec.Body.String())
	}
	srv := httptest.NewServer(s)
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", "http://127.0.0.1:"+u.Port()+"/api/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	r := bufio.NewReader(resp.Body)
	if line, _ := r.ReadString('\n'); !strings.HasPrefix(line, ": connected") {
		t.Fatalf("first line = %q", line)
	}
	go do(t, s, "POST", "/api/games/acc/rescan", "{}")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			t.Fatalf("no event: %v", err)
		}
		if strings.HasPrefix(line, "data: ") && strings.Contains(line, "game-updated") {
			return
		}
	}
}

func TestHistoryAPI(t *testing.T) {
	s, setups := newTest(t)
	body := `{"theme":"system","threshold":95,"historyThreshold":80,"nameThreshold":65,"games":{"acc":{"visibility":"show","folder":` + jsonString(setups) + `}}}`
	if rec := do(t, s, "PUT", "/api/settings", body); rec.Code != 200 {
		t.Fatalf("show game: %d %s", rec.Code, rec.Body.String())
	}
	td := decode[library.TrackDetail](t, do(t, s, "GET", "/api/games/acc/cars/ferrari_296_gt3/tracks/spa", ""))
	if len(td.SetupIDs) != 2 || td.SetupIDs[0] == 0 || td.SetupIDs[1] == 0 {
		t.Fatalf("setup ids missing: %+v", td.SetupIDs)
	}
	id := strconv.FormatInt(td.SetupIDs[1], 10)

	type detail struct {
		Setup struct {
			Name, Note, Session, LinkKind string
		}
		Versions []struct{ ID int64 }
		Parent   *struct{ SetupID int64 }
		Family   *struct{ SetupID int64 }
	}
	d := decode[detail](t, do(t, s, "GET", "/api/setups/"+id, ""))
	if len(d.Versions) != 1 || d.Family == nil {
		t.Fatalf("setup detail: %+v", d)
	}
	// b.json is identical to a.json and newer → probably made from a.
	if d.Parent == nil || d.Parent.SetupID != td.SetupIDs[0] || d.Setup.LinkKind != "probable" {
		t.Errorf("probable parent wrong: %+v", d)
	}

	if rec := do(t, s, "PUT", "/api/setups/"+id, `{"note":"TC 2 and 5","session":"Q"}`); rec.Code != 200 {
		t.Fatalf("update: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(t, s, "PUT", "/api/setups/"+id+"/parent", `{"parentVersionId":0}`); rec.Code != 200 {
		t.Fatalf("relink: %d %s", rec.Code, rec.Body.String())
	}
	d = decode[detail](t, do(t, s, "GET", "/api/setups/"+id, ""))
	if d.Setup.Note != "TC 2 and 5" || d.Setup.Session != "Q" || d.Parent != nil || d.Setup.LinkKind != "manual" {
		t.Errorf("after update/relink: %+v", d)
	}

	v := decode[struct {
		Fields []struct{ Path string }
	}](t, do(t, s, "GET", "/api/versions/"+strconv.FormatInt(d.Versions[0].ID, 10), ""))
	if len(v.Fields) == 0 {
		t.Error("version fields missing")
	}

	ch := decode[struct {
		Events []struct{ Kind string }
		Unseen int
	}](t, do(t, s, "GET", "/api/changes", ""))
	if ch.Unseen == 0 || len(ch.Events) == 0 {
		t.Errorf("changes: %+v", ch)
	}
	do(t, s, "POST", "/api/changes/seen", "{}")
	if ch := decode[struct{ Unseen int }](t, do(t, s, "GET", "/api/changes", "")); ch.Unseen != 0 {
		t.Error("not marked seen")
	}

	p := decode[struct {
		Summary struct{ Families, Linked int }
	}](t, do(t, s, "POST", "/api/games/acc/history/preview", `{"threshold":80,"nameThreshold":65}`))
	if p.Summary.Families != 2 || p.Summary.Linked != 0 { // manual "no parent" link is kept
		t.Errorf("preview: %+v", p)
	}
	if rec := do(t, s, "POST", "/api/games/acc/history/preview", `{"threshold":20,"nameThreshold":65}`); rec.Code != 400 {
		t.Errorf("bad threshold accepted: %d", rec.Code)
	}
	if rec := do(t, s, "GET", "/api/setups/999999", ""); rec.Code != 404 {
		t.Errorf("unknown setup: %d", rec.Code)
	}
}

func TestStagingAPI(t *testing.T) {
	s, setups := newTest(t)
	spa := filepath.Join(setups, "ferrari_296_gt3", "Spa")
	body := `{"theme":"system","threshold":95,"historyThreshold":80,"nameThreshold":65,"games":{"acc":{"visibility":"show","folder":` + jsonString(setups) + `}}}`
	do(t, s, "PUT", "/api/settings", body)
	td := decode[library.TrackDetail](t, do(t, s, "GET", "/api/games/acc/cars/ferrari_296_gt3/tracks/spa", ""))
	idA := strconv.FormatInt(td.SetupIDs[0], 10)

	// Not managed: staging is refused, and settings can't switch management on behind the backup's back.
	if rec := do(t, s, "POST", "/api/setups/"+idA+"/unstage", "{}"); rec.Code != 409 {
		t.Fatalf("unstage while not managed: %d", rec.Code)
	}
	do(t, s, "PUT", "/api/settings", strings.Replace(body, `"visibility":"show"`, `"visibility":"show","managed":true`, 1))
	if g := decode[struct{ Games []library.GameSummary }](t, do(t, s, "GET", "/api/games", "")).Games[0]; g.Managed {
		t.Fatal("settings switched management on without a backup")
	}

	mr := decode[library.ManageResult](t, do(t, s, "POST", "/api/games/acc/manage", `{"on":true}`))
	if !mr.Managed || mr.BackupID == "" {
		t.Fatalf("manage: %+v", mr)
	}
	list := decode[struct{ Backups []folderbackup.Summary }](t, do(t, s, "GET", "/api/games/acc/backups", ""))
	if len(list.Backups) != 1 || list.Backups[0].Reason != folderbackup.ReasonBeforeManaging {
		t.Fatalf("backups: %+v", list)
	}

	if rec := do(t, s, "POST", "/api/games/acc/cars/ferrari_296_gt3/tracks/spa/keep-only", `{"setupIds":[`+idA+`]}`); rec.Code != 200 {
		t.Fatalf("keep-only: %d %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(spa, "b.json")); !os.IsNotExist(err) {
		t.Fatal("b.json should be moved to the library")
	}
	td = decode[library.TrackDetail](t, do(t, s, "GET", "/api/games/acc/cars/ferrari_296_gt3/tracks/spa", ""))
	if !td.Managed || len(td.Removed) != 1 || len(td.Setups) != 1 {
		t.Fatalf("track after keep-only: managed=%v libraryOnly=%d inFolder=%d", td.Managed, len(td.Removed), len(td.Setups))
	}

	plan := decode[folderbackup.Plan](t, do(t, s, "GET", "/api/games/acc/backups/"+mr.BackupID+"/plan", ""))
	if plan.Return != 1 || plan.Remove != 0 {
		t.Errorf("plan: %+v", plan)
	}
	if rec := do(t, s, "POST", "/api/games/acc/backups/"+mr.BackupID+"/restore", "{}"); rec.Code != 200 {
		t.Fatalf("restore: %d %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(spa, "b.json")); err != nil {
		t.Error("b.json not restored")
	}
	if rec := do(t, s, "GET", "/api/games/acc/backups/nope/plan", ""); rec.Code != 404 {
		t.Errorf("unknown backup: %d", rec.Code)
	}
}

func TestFuelAPI(t *testing.T) {
	s, _ := newTest(t)
	p := decode[struct {
		Laps   int
		Stints []struct {
			Laps int
			Fuel float64
		}
	}](t, do(t, s, "POST", "/api/fuel/race", `{"minutes":60,"avgLap":105,"fuelPerLap":3.2,"stops":1,"pitLoss":25,"safety":2}`))
	if p.Laps != 35 || len(p.Stints) != 2 || p.Stints[0].Fuel != 60 || p.Stints[1].Fuel != 58 {
		t.Errorf("race: %+v", p)
	}
	if rec := do(t, s, "POST", "/api/fuel/race", `{"minutes":0}`); rec.Code != 400 {
		t.Errorf("empty race: %d", rec.Code)
	}
	q := decode[struct{ Laps int }](t, do(t, s, "POST", "/api/fuel/qualifying", `{"minutes":15,"bestLap":104,"fuelPerLap":3.2}`))
	if q.Laps != 9 {
		t.Errorf("qualifying: %+v", q)
	}
	if rec := do(t, s, "GET", "/api/fuel/acc/ferrari_296_gt3/spa", ""); rec.Code != 200 || rec.Body.String() != "{}" {
		t.Errorf("empty inputs: %d %s", rec.Code, rec.Body.String())
	}
	do(t, s, "PUT", "/api/fuel/acc/ferrari_296_gt3/spa", `{"race":{"pitLoss":25}}`)
	if rec := do(t, s, "GET", "/api/fuel/acc/Ferrari_296_GT3/Spa", ""); !strings.Contains(rec.Body.String(), `"pitLoss":25`) {
		t.Errorf("saved inputs: %s", rec.Body.String())
	}
}

func TestBrowse(t *testing.T) {
	s, _ := newTest(t)
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "Alpha"), 0o755)
	os.MkdirAll(filepath.Join(dir, "beta"), 0o755)
	os.MkdirAll(filepath.Join(dir, ".hidden"), 0o755)
	res := decode[browseResult](t, do(t, s, "GET", "/api/browse?path="+url.QueryEscape(dir), ""))
	if len(res.Dirs) != 2 || res.Dirs[0].Name != "Alpha" || res.Parent == nil {
		t.Errorf("browse: %+v", res)
	}
	top := decode[browseResult](t, do(t, s, "GET", "/api/browse", ""))
	if len(top.Dirs) == 0 || top.Path != "" {
		t.Errorf("drive list: %+v", top)
	}
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
