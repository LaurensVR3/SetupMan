package library

// Telemetry per game session (D-188, A-1). ACC keeps one MoTeC file for as long as you stay on a
// server: practice, qualifying and race end up in one file (rig 2026-11-23). Each lap is linked to
// the lap SetupMan recorded (D-179), so a file whose laps belong to several recorded sessions is
// shown as one entry per session: its own kind, start, laps, best lap and setup.

import (
	"os"

	"setupman/internal/store"
	"setupman/internal/telemetry"
)

// lapSessions says which recorded session each lap belongs to: its linked lap's session, and for
// a lap without a link (an out-lap, a lap SetupMan didn't record) the session of the lap before
// it (the first session for laps before any link). order lists the sessions as they come; of is
// nil when the laps belong to fewer than two sessions (the file isn't split).
func lapSessions(laps []store.TeleLap) (order []int64, of []int64) {
	seen := map[int64]bool{}
	for _, l := range laps {
		if l.SessionID != 0 && !seen[l.SessionID] {
			seen[l.SessionID] = true
			order = append(order, l.SessionID)
		}
	}
	if len(order) < 2 {
		return order, nil
	}
	of = make([]int64, len(laps))
	cur := order[0]
	for i, l := range laps {
		if l.SessionID != 0 {
			cur = l.SessionID
		}
		of[i] = cur
	}
	return order, of
}

// partLaps returns the laps of one recorded session in a file (all laps when the file isn't split
// or session is 0).
func partLaps(laps []store.TeleLap, session int64) []store.TeleLap {
	if session == 0 {
		return laps
	}
	_, of := lapSessions(laps)
	if of == nil {
		return laps
	}
	out := []store.TeleLap{}
	for i, l := range laps {
		if of[i] == session {
			out = append(out, l)
		}
	}
	return out
}

// partView is a file shown as one of its sessions: that session's kind, start and setup, and the
// laps, best lap and driving time of its laps only.
func (l *Library) partView(v TeleFileView, session int64, laps []store.TeleLap) TeleFileView {
	p := v
	p.Part, p.SessionID, p.Match = session, session, store.TeleSession
	if s, _ := l.db.Session(session); s != nil {
		p.Kind, p.StartedAt, p.SetupID, p.VersionID, p.SetupName = s.Kind, s.StartedAt, s.SetupID, s.VersionID, ""
		if s.SetupID != 0 {
			if st, _ := l.db.Setup(s.SetupID); st != nil {
				p.SetupName = st.Name
			}
		}
	}
	p.Laps, p.BestMs, p.DurationMs = 0, 0, 0
	for _, lp := range laps {
		p.DurationMs += int64(lp.LapMs)
		if lp.Complete && lp.Lap > 0 {
			p.Laps++
			if (lp.Valid == nil || *lp.Valid) && (p.BestMs == 0 || lp.LapMs < p.BestMs) {
				p.BestMs = lp.LapMs
			}
		}
	}
	p.HasExtra = false
	if l.TelemetryDir != "" {
		_, err := os.Stat(telemetry.ExtraPath(l.TelemetryDir, v.Game, session))
		p.HasExtra = err == nil
	}
	return p
}

// splitFiles replaces each file whose laps belong to several recorded sessions with one entry
// per session.
func (l *Library) splitFiles(files []TeleFileView) []TeleFileView {
	out := make([]TeleFileView, 0, len(files))
	for _, f := range files {
		if f.Match != store.TeleSession || f.KeptBy != 0 {
			out = append(out, f)
			continue
		}
		laps, err := l.db.TelemetrySessionLaps(f.ID)
		order, of := lapSessions(laps)
		if err != nil || of == nil {
			out = append(out, f)
			continue
		}
		for _, sid := range order {
			out = append(out, l.partView(f, sid, partLaps(laps, sid)))
		}
	}
	return out
}
