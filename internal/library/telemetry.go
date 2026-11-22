package library

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"setupman/internal/game"
	"setupman/internal/games/acc"
	"setupman/internal/history"
	"setupman/internal/telemetry"
)

// traceRecorder buffers high-rate (~20 Hz) driving samples for one game until a lap finishes.
type traceRecorder struct {
	mu            sync.Mutex
	car, track    string
	session       string
	lastLaps      int
	lastCurTimeMs int
	lapStartAt    time.Time
	lapPit        bool
	lapInvalid    bool
	lastAt        time.Time
	lastSpeed     float64
	cumDist       float64
	samples       []telemetry.RawSample
	active        bool
	lastSavedMs   int
}

// TelemetryLive describes whether high-rate lap telemetry is currently being captured from ACC.
type TelemetryLive struct {
	Active    bool   `json:"active"`
	Car       string `json:"car,omitempty"`
	Track     string `json:"track,omitempty"`
	CarName   string `json:"carName,omitempty"`
	TrackName string `json:"trackName,omitempty"`
	Samples   int    `json:"samples,omitempty"`
	LapNumber int    `json:"lapNumber,omitempty"`
}

// startTraceRecorders subscribes to any game implementing game.TraceSource (ACC).
func (l *Library) startTraceRecorders() {
	if l.db == nil {
		return
	}
	for _, g := range l.games {
		ts, ok := g.(game.TraceSource)
		if !ok {
			continue
		}
		g := g
		tr := &traceRecorder{}
		l.traceMu.Lock()
		if l.traceRecs == nil {
			l.traceRecs = map[string]*traceRecorder{}
		}
		l.traceRecs[g.ID()] = tr
		l.traceMu.Unlock()
		ts.OnTrace(func(s game.TraceSample) {
			l.feedTrace(g, tr, s)
		})
	}
}

func (l *Library) feedTrace(g game.Game, tr *traceRecorder, s game.TraceSample) {
	if l.db == nil {
		return
	}
	now := time.Now()

	tr.mu.Lock()
	if !s.Live || s.Car == "" || s.Track == "" {
		tr.active = false
		tr.mu.Unlock()
		return
	}

	track := strings.ToLower(strings.TrimSpace(s.Track))
	car := strings.TrimSpace(s.Car)

	// New session or car/track change resets the lap buffer.
	newSess := !tr.active || !strings.EqualFold(tr.car, car) || !strings.EqualFold(tr.track, track) ||
		tr.session != s.Session || s.CompletedLaps < tr.lastLaps
	if newSess {
		tr.car, tr.track, tr.session = car, track, s.Session
		tr.lastLaps = s.CompletedLaps
		tr.lastCurTimeMs = s.CurrentTimeMs
		tr.lapStartAt = now
		tr.lastAt = now
		tr.lastSpeed = s.SpeedKmh
		tr.cumDist = 0
		tr.lapPit = s.InPit
		tr.lapInvalid = false
		tr.samples = tr.samples[:0]
		tr.active = true
		tr.mu.Unlock()
		return
	}

	// Check for lap completion.
	var finished []telemetry.RawSample
	var finishedLapMs, finishedLapNum int
	var finishedValid, finishedPit bool
	if s.CompletedLaps > tr.lastLaps {
		if s.LastLapMs >= 15000 && len(tr.samples) >= 80 && !tr.lapPit {
			finished = append([]telemetry.RawSample(nil), tr.samples...)
			finishedLapMs = s.LastLapMs
			finishedLapNum = s.CompletedLaps
			finishedValid = !tr.lapInvalid
			finishedPit = tr.lapPit
		}
		// Start buffering the next lap.
		tr.lastLaps = s.CompletedLaps
		tr.lastCurTimeMs = s.CurrentTimeMs
		tr.lapStartAt = now
		tr.lastAt = now
		tr.lastSpeed = s.SpeedKmh
		tr.cumDist = 0
		tr.lapPit = s.InPit
		tr.lapInvalid = false
		tr.samples = tr.samples[:0]
	} else if !s.InPit && tr.lastCurTimeMs > 5000 && s.CurrentTimeMs >= 0 && s.CurrentTimeMs+5000 < tr.lastCurTimeMs {
		// In ACC Practice/Qualifying, finishing an out-lap or taking the Nürburgring 24h GP
		// shortcut resets CurrentTimeMs to 0 at Start/Finish WITHOUT incrementing CompletedLaps.
		// Reset the buffer cleanly here so the upcoming flying lap starts at 0 m with lapPit=false.
		tr.lapStartAt = now
		tr.lastAt = now
		tr.lastSpeed = s.SpeedKmh
		tr.cumDist = 0
		tr.lapPit = false
		tr.lapInvalid = false
		tr.samples = tr.samples[:0]
	}

	if s.InPit {
		tr.lapPit = true
	}
	// Ignore brief out-lap/previous-lap invalid flag during the first 2 seconds of a newly started lap.
	if !s.ValidLap && (s.CurrentTimeMs == 0 || s.CurrentTimeMs > 2000) && len(tr.samples) > 10 {
		tr.lapInvalid = true
	}

	// Only append samples when moving on track (outside pits) and cap buffer size safely (~45 min max).
	if !s.InPit && s.SpeedKmh > 3 && len(tr.samples) < 60000 {
		dt := now.Sub(tr.lastAt).Seconds()
		if dt > 0 && dt < 2.5 {
			tr.cumDist += ((tr.lastSpeed + s.SpeedKmh) / 2.0 / 3.6) * dt
		}
		tSec := now.Sub(tr.lapStartAt).Seconds()
		if s.CurrentTimeMs > 0 {
			tSec = float64(s.CurrentTimeMs) / 1000.0
		}
		tr.samples = append(tr.samples, telemetry.RawSample{
			TimeS:    tSec,
			DistM:    tr.cumDist,
			NormDist: s.LapProgress,
			SpeedKmh: s.SpeedKmh,
			Throttle: s.Throttle,
			Brake:    s.Brake,
			SteerDeg: s.SteerDeg,
			Gear:     s.Gear,
			RPM:      s.RPM,
			TC:       s.TCActive,
			ABS:      s.ABSActive,
			GLat:     s.GLat,
			GLon:     s.GLon,
			X:        s.X,
			Y:        s.Y,
			HasXY:    s.HasXY,
		})
	}
	tr.lastAt = now
	tr.lastSpeed = s.SpeedKmh
	if s.CurrentTimeMs > 0 {
		tr.lastCurTimeMs = s.CurrentTimeMs
	}
	tr.mu.Unlock()

	if len(finished) > 0 && !finishedPit {
		l.saveLiveTelemetryLap(g.ID(), car, track, finished, finishedLapMs, finishedLapNum, finishedValid)
	}
}

func (l *Library) saveLiveTelemetryLap(gameID, car, track string, raw []telemetry.RawSample, lapMs, lapNum int, valid bool) {
	if len(raw) < 2 {
		return
	}
	estLen := raw[len(raw)-1].DistM
	if estLen < 500 {
		return
	}
	bins := telemetry.BinsForTrackLength(estLen)
	tr, stats := telemetry.Resample(raw, lapMs, bins)
	if tr == nil {
		return
	}
	if !tr.HasCoords {
		telemetry.ReconstructMap(tr)
	}
	gz, err := telemetry.EncodeTrace(tr)
	if err != nil {
		log.Printf("telemetry encode: %v", err)
		return
	}
	rec := l.Recording()
	var sessID, setupID int64
	if rec.Active && strings.EqualFold(rec.Game, gameID) && strings.EqualFold(rec.Track, track) {
		sessID = rec.SessionID
		setupID = rec.SetupID
	}
	hasRef, _ := l.db.HasTelemetryReference(gameID, track)
	lap := &telemetry.LapMeta{
		Game:         gameID,
		Car:          car,
		Track:        track,
		Source:       telemetry.SourceLive,
		LapMs:        lapMs,
		LapNumber:    lapNum,
		Valid:        valid,
		IsReference:  valid && !hasRef,
		TrackLengthM: tr.TrackLengthM,
		SessionID:    sessID,
		SetupID:      setupID,
		RecordedAt:   time.Now(),
		HasCoords:    tr.HasCoords,
		Stats:        stats,
	}
	if err := l.db.InsertTelemetryLap(lap, gz); err != nil {
		log.Printf("telemetry save: %v", err)
		return
	}
	log.Printf("telemetry: saved live %s lap %d (%s / %s, %d ms, %d bins)", gameID, lapNum, car, track, lapMs, tr.N)
	l.publish(Event{Type: "telemetry"})
}

// TelemetryLiveStatus returns the live telemetry capture state if ACC is currently on track.
func (l *Library) TelemetryLiveStatus() TelemetryLive {
	l.traceMu.Lock()
	defer l.traceMu.Unlock()
	for id, tr := range l.traceRecs {
		tr.mu.Lock()
		active := tr.active
		car, track := tr.car, tr.track
		n := len(tr.samples)
		lap := tr.lastLaps + 1
		tr.mu.Unlock()
		if active && track != "" {
			cn, tn := l.telemetryReadableNames(id, car, track)
			return TelemetryLive{
				Active:    true,
				Car:       car,
				Track:     track,
				CarName:   cn,
				TrackName: tn,
				Samples:   n,
				LapNumber: lap,
			}
		}
	}
	return TelemetryLive{}
}

func (l *Library) telemetryReadableNames(gameID, car, track string) (carName, trackName string) {
	carName, trackName = l.readableNames(gameID, car, track)
	if strings.EqualFold(gameID, "acc") || gameID == "" {
		if car != "" && (carName == "" || strings.EqualFold(carName, car)) {
			cn, _, _, _ := acc.CarInfo(car)
			if cn != "" {
				carName = cn
			}
		}
		if track != "" && (trackName == "" || strings.EqualFold(trackName, track)) {
			if tn := acc.TrackName(track); tn != "" {
				trackName = tn
			}
		}
	}
	return carName, trackName
}

// TelemetryLaps lists stored telemetry laps with readable car and track names.
func (l *Library) TelemetryLaps(gameID, car, track string) ([]telemetry.LapMeta, error) {
	if l.db == nil {
		return nil, ErrNoHistory
	}
	laps, err := l.db.ListTelemetryLaps(gameID, car, track)
	if err != nil {
		return nil, err
	}
	for i := range laps {
		laps[i].CarName, laps[i].TrackName = l.telemetryReadableNames(laps[i].Game, laps[i].Car, laps[i].Track)
	}
	return laps, nil
}

// CompareTelemetry loads two stored laps and returns their aligned distance comparison.
func (l *Library) CompareTelemetry(refID, cmpID int64) (telemetry.Comparison, error) {
	if l.db == nil {
		return telemetry.Comparison{}, ErrNoHistory
	}
	refMeta, refGz, err := l.db.GetTelemetryLap(refID)
	if err != nil {
		return telemetry.Comparison{}, err
	}
	if refMeta == nil {
		return telemetry.Comparison{}, history.ErrNotFound
	}
	cmpMeta, cmpGz, err := l.db.GetTelemetryLap(cmpID)
	if err != nil {
		return telemetry.Comparison{}, err
	}
	if cmpMeta == nil {
		return telemetry.Comparison{}, history.ErrNotFound
	}
	refTr, err := telemetry.DecodeTrace(refGz)
	if err != nil {
		return telemetry.Comparison{}, fmt.Errorf("decoding reference lap: %w", err)
	}
	cmpTr, err := telemetry.DecodeTrace(cmpGz)
	if err != nil {
		return telemetry.Comparison{}, fmt.Errorf("decoding comparison lap: %w", err)
	}
	refMeta.CarName, refMeta.TrackName = l.telemetryReadableNames(refMeta.Game, refMeta.Car, refMeta.Track)
	cmpMeta.CarName, cmpMeta.TrackName = l.telemetryReadableNames(cmpMeta.Game, cmpMeta.Car, cmpMeta.Track)
	return telemetry.Compare(*refMeta, *cmpMeta, refTr, cmpTr), nil
}

// SetTelemetryReference marks a lap as the reference lap for its track.
func (l *Library) SetTelemetryReference(id int64) error {
	if l.db == nil {
		return ErrNoHistory
	}
	if err := l.db.SetTelemetryReference(id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return history.ErrNotFound
		}
		return err
	}
	l.publish(Event{Type: "telemetry"})
	return nil
}

// RenameTelemetryLap sets a custom label on a stored telemetry lap.
func (l *Library) RenameTelemetryLap(id int64, label string) error {
	if l.db == nil {
		return ErrNoHistory
	}
	if err := l.db.RenameTelemetryLap(id, label); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return history.ErrNotFound
		}
		return err
	}
	l.publish(Event{Type: "telemetry"})
	return nil
}

// DeleteTelemetryLap removes a stored telemetry lap.
func (l *Library) DeleteTelemetryLap(id int64) error {
	if l.db == nil {
		return ErrNoHistory
	}
	if err := l.db.DeleteTelemetryLap(id); err != nil {
		return err
	}
	l.publish(Event{Type: "telemetry"})
	return nil
}

// LoadNords24Samples inserts (or refreshes) the two sample Nürburgring 24h laps.
func (l *Library) LoadNords24Samples() (refID, cmpID int64, err error) {
	if l.db == nil {
		return 0, 0, ErrNoHistory
	}
	refMeta, cmpMeta, refTr, cmpTr := telemetry.BuildNords24Samples()
	refGz, err := telemetry.EncodeTrace(refTr)
	if err != nil {
		return 0, 0, err
	}
	cmpGz, err := telemetry.EncodeTrace(cmpTr)
	if err != nil {
		return 0, 0, err
	}
	_ = l.db.DeleteSampleTelemetryLaps("nurburgring_24h")
	// Keep an existing real reference lap if the user already has one.
	if hasRef, _ := l.db.HasTelemetryReference("acc", "nurburgring_24h"); hasRef {
		refMeta.IsReference = false
	}
	if err := l.db.InsertTelemetryLap(&refMeta, refGz); err != nil {
		return 0, 0, err
	}
	if err := l.db.InsertTelemetryLap(&cmpMeta, cmpGz); err != nil {
		return 0, 0, err
	}
	l.publish(Event{Type: "telemetry"})
	return refMeta.ID, cmpMeta.ID, nil
}

// MoTeCFolder returns ACC's detected MoTeC folder on this PC ("" if not found).
func (l *Library) MoTeCFolder() string {
	for _, g := range l.games {
		if p, ok := g.(game.MoTeCFolderProvider); ok {
			if dir := p.MoTeCFolder(); dir != "" {
				return dir
			}
		}
	}
	return ""
}

// ListMoTeCFiles scans ACC's MoTeC folder (read-only) for .ld logs, newest first.
func (l *Library) ListMoTeCFiles() (string, []telemetry.MoTeCFileInfo, error) {
	dir := l.MoTeCFolder()
	if dir == "" {
		return "", []telemetry.MoTeCFileInfo{}, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return dir, nil, err
	}
	imported := map[string]bool{}
	if l.db != nil {
		if m, err := l.db.TelemetrySourceFiles(); err == nil {
			imported = m
		}
	}
	var out []telemetry.MoTeCFileInfo
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".ld") {
			continue
		}
		info, err := telemetry.InspectMoTeC(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		info.Imported = imported[strings.ToLower(info.Name)]
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Modified.After(out[j].Modified) })
	return dir, out, nil
}

// ImportMoTeCFromDisk imports one .ld file (and its paired .ldx if present) from ACC's MoTeC folder.
func (l *Library) ImportMoTeCFromDisk(fileName string) ([]telemetry.LapMeta, error) {
	if l.db == nil {
		return nil, ErrNoHistory
	}
	dir := l.MoTeCFolder()
	if dir == "" {
		return nil, errors.New("ACC MoTeC folder was not found on this PC")
	}
	clean := filepath.Base(fileName)
	if clean == "" || clean == "." || !strings.EqualFold(filepath.Ext(clean), ".ld") {
		return nil, errors.New("choose a .ld MoTeC file")
	}
	ldPath := filepath.Join(dir, clean)
	ldBytes, err := os.ReadFile(ldPath)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", clean, err)
	}
	ldxPath := strings.TrimSuffix(ldPath, filepath.Ext(ldPath)) + ".ldx"
	ldxBytes, _ := os.ReadFile(ldxPath)
	return l.ImportMoTeCUpload(clean, ldBytes, ldxBytes)
}

// ImportMoTeCUpload parses uploaded MoTeC .ld (and optional .ldx) bytes and stores each lap.
func (l *Library) ImportMoTeCUpload(ldName string, ldBytes, ldxBytes []byte) ([]telemetry.LapMeta, error) {
	if l.db == nil {
		return nil, ErrNoHistory
	}
	parsed, err := telemetry.ParseMoTeC(ldBytes, ldxBytes, ldName)
	if err != nil {
		return nil, err
	}
	track := parsed.Track
	if track == "" {
		track = "unknown_track"
	}
	car := parsed.Car
	if car == "" {
		car = "unknown_car"
	}
	hasRef, _ := l.db.HasTelemetryReference("acc", track)
	// Pick fastest imported lap as reference if no reference exists yet for this track.
	fastestIdx := 0
	for i, lp := range parsed.Laps {
		if lp.LapMs < parsed.Laps[fastestIdx].LapMs {
			fastestIdx = i
		}
	}

	saved := make([]telemetry.LapMeta, 0, len(parsed.Laps))
	for i, lp := range parsed.Laps {
		gz, err := telemetry.EncodeTrace(lp.Trace)
		if err != nil {
			return saved, err
		}
		meta := telemetry.LapMeta{
			Game:         "acc",
			Car:          car,
			Track:        track,
			Source:       telemetry.SourceMoTeC,
			SourceFile:   parsed.SourceFile,
			Label:        fmt.Sprintf("MoTeC Lap %d", lp.LapNumber),
			LapMs:        lp.LapMs,
			LapNumber:    lp.LapNumber,
			Valid:        lp.Valid,
			IsReference:  !hasRef && i == fastestIdx,
			TrackLengthM: lp.Trace.TrackLengthM,
			RecordedAt:   lp.RecordedAt,
			HasCoords:    lp.Trace.HasCoords,
			Stats:        lp.Stats,
		}
		if err := l.db.InsertTelemetryLap(&meta, gz); err != nil {
			return saved, err
		}
		meta.CarName, meta.TrackName = l.telemetryReadableNames(meta.Game, meta.Car, meta.Track)
		saved = append(saved, meta)
	}
	l.publish(Event{Type: "telemetry"})
	return saved, nil
}
