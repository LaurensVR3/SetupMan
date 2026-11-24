package library

// Tyre temperatures for laps recorded before SetupMan logged them (D-189): taken once from the
// game's telemetry files (ACC's MoTeC TYRE_TAIR channels, the temperature its HUD shows), for each
// recorded lap linked to a lap in a file. Read in the background after indexing, a few files per
// pass, never while driving.

import (
	"log"
	"strconv"

	"setupman/internal/game"
)

// tyreTempChannels are the MoTeC channels with the tyre temperature, FL FR RL RR.
var tyreTempChannels = []string{"TYRE_TAIR_LF", "TYRE_TAIR_RF", "TYRE_TAIR_LR", "TYRE_TAIR_RR"}

// tyreTempFilesPerPass: how many files one pass reads (each lap reads 4 channels of one lap).
const tyreTempFilesPerPass = 20

// backfillTyreTemps fills in recorded laps' tyre temperatures from the telemetry files; returns how
// many laps it filled in.
func (l *Library) backfillTyreTemps(gameID string) int {
	if l.db == nil {
		return 0
	}
	need, err := l.db.TeleLapsWithoutTyreTemps(gameID)
	if err != nil || len(need) == 0 {
		return 0
	}
	filled, files := 0, 0
	byFile := map[int64][]int{}
	var order []int64
	for i, n := range need {
		if _, ok := byFile[n.FileID]; !ok {
			order = append(order, n.FileID)
		}
		byFile[n.FileID] = append(byFile[n.FileID], i)
	}
	for _, fid := range order {
		key := "telemetry/tyretemps/" + strconv.FormatInt(fid, 10)
		if v, _ := l.GetSaved(key); v != "" {
			continue // read before: its other laps have no tyre data
		}
		if files >= tyreTempFilesPerPass || l.Live() == game.LiveDriving {
			break
		}
		files++
		for _, i := range byFile[fid] {
			n := need[i]
			d, err := l.TelemetryLap(n.FileID, n.N, tyreTempChannels, false)
			if err != nil || len(d.Channels) != 4 {
				continue
			}
			var avg [4]float64
			ok := true
			for _, c := range d.Channels {
				w := -1
				for k, name := range tyreTempChannels {
					if c.Name == name {
						w = k
					}
				}
				if w < 0 {
					ok = false
					break
				}
				sum, cnt := 0.0, 0
				for _, v := range c.Data {
					if v > 1 && v < 200 {
						sum += float64(v)
						cnt++
					}
				}
				if cnt < 10 {
					ok = false
					break
				}
				avg[w] = sum / float64(cnt)
			}
			if ok && l.db.SetLapTyreTemps(n.LapID, avg) == nil {
				filled++
			}
		}
		_ = l.SetSaved(key, "done")
	}
	if filled > 0 {
		log.Printf("telemetry %s: tyre temperatures filled in for %d recorded laps", gameID, filled)
	}
	return filled
}
