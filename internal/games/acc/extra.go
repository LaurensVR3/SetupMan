package acc

// SetupMan's extra telemetry log for ACC (spec D-162): values ACC's shared memory has and its
// MoTeC files lack. Offsets from Kunos' "ACC Shared Memory Documentation" (structs packed to 4
// bytes); they line up with the offsets already confirmed on the rig (fuel @12, pressures @88,
// airTemp @288, brakeBias @564; isInPitLane @1236, TC @1268, isValidLap @1408, mfdFuelToAdd @1536).
//
//	physics:  fuel 12 · wheelSlip[4] 56 · heading 208 · pitch 212 · roll 216 · rideHeight[2] 268 ·
//	          airTemp 288 · roadTemp 292 · brakeBias 564 · slipRatio[4] 640 · slipAngle[4] 656 ·
//	          brakeTemp[4] 348 · tcinAction 672 · absInAction 676 · waterTemp 712 · brakePressure[4] 716 ·
//	          padLife[4] 740 · discLife[4] 756
//	graphics: status 4 · completedLaps 132 · iCurrentTime 140 · iLastTime 144 · normalizedCarPosition 248 ·
//	          activeCars 252 · carCoordinates[60][3] 256 · carID[60] 976 · playerCarID 1216 ·
//	          isInPitLane 1236 · TC 1268 · TCCut 1272 · EngineMap 1276 · ABS 1280 · isValidLap 1408 ·
//	          trackGripStatus 1556 · rainIntensity 1560

import (
	"path/filepath"

	"setupman/internal/game"
)

const (
	extraPhysicsSize  = 772
	extraGraphicsSize = 1564
)

type extraField struct {
	ch    game.ExtraChannel
	page  byte // 'p' physics, 'g' graphics
	off   int
	isInt bool
}

var extraWheels = [4]string{"FL", "FR", "RL", "RR"}

func extraFields() []extraField {
	f := []extraField{
		{game.ExtraChannel{Name: "LAP"}, 'g', offCompletedLaps, true},
		{game.ExtraChannel{Name: "LAP_TIME", Unit: "ms"}, 'g', 140, true},
		{game.ExtraChannel{Name: "LAST_LAP", Unit: "ms"}, 'g', offLastLapMs, true},
		{game.ExtraChannel{Name: "LAP_POS"}, 'g', offLapProgress, false},
		{game.ExtraChannel{Name: "LAP_VALID"}, 'g', offIsValidLap, true},
		{game.ExtraChannel{Name: "PIT_LANE"}, 'g', offIsInPitLane, true},
		{game.ExtraChannel{Name: "FUEL", Unit: "L"}, 'p', offFuel, false},
		{game.ExtraChannel{Name: "WATER_TEMP", Unit: "°C"}, 'p', 712, false},
		{game.ExtraChannel{Name: "HEADING", Unit: "rad"}, 'p', 208, false},
		{game.ExtraChannel{Name: "PITCH", Unit: "rad"}, 'p', 212, false},
		{game.ExtraChannel{Name: "ROLL", Unit: "rad"}, 'p', 216, false},
		{game.ExtraChannel{Name: "RIDE_HEIGHT_F"}, 'p', 268, false},
		{game.ExtraChannel{Name: "RIDE_HEIGHT_R"}, 'p', 272, false},
		{game.ExtraChannel{Name: "AIR_TEMP", Unit: "°C"}, 'p', offAirTemp, false},
		{game.ExtraChannel{Name: "ROAD_TEMP", Unit: "°C"}, 'p', offRoadTemp, false},
		{game.ExtraChannel{Name: "BRAKE_BIAS"}, 'p', offBrakeBias, false},
		{game.ExtraChannel{Name: "TC_ACTIVE"}, 'p', 672, true},
		{game.ExtraChannel{Name: "ABS_ACTIVE"}, 'p', 676, true},
		{game.ExtraChannel{Name: "TC_LEVEL"}, 'g', offTC, true},
		{game.ExtraChannel{Name: "TC_CUT"}, 'g', 1272, true},
		{game.ExtraChannel{Name: "ENGINE_MAP"}, 'g', offEngineMap, true},
		{game.ExtraChannel{Name: "ABS_LEVEL"}, 'g', offABS, true},
		{game.ExtraChannel{Name: "TRACK_GRIP"}, 'g', 1556, true},
		{game.ExtraChannel{Name: "RAIN"}, 'g', 1560, true},
	}
	for _, w := range []struct {
		name string
		off  int
	}{{"WHEEL_SLIP", 56}, {"BRAKE_TEMP", 348}, {"SLIP_RATIO", 640}, {"SLIP_ANGLE", 656}, {"BRAKE_PRESS", 716}, {"PAD_LIFE", 740}, {"DISC_LIFE", 756}} {
		for i, s := range extraWheels {
			f = append(f, extraField{game.ExtraChannel{Name: w.name + "_" + s}, 'p', w.off + 4*i, false})
		}
	}
	// car position last (POS_X, POS_Y, POS_Z), filled from carCoordinates
	return append(f,
		extraField{ch: game.ExtraChannel{Name: "POS_X", Unit: "m"}, page: 'c', off: 0},
		extraField{ch: game.ExtraChannel{Name: "POS_Y", Unit: "m"}, page: 'c', off: 4},
		extraField{ch: game.ExtraChannel{Name: "POS_Z", Unit: "m"}, page: 'c', off: 8})
}

var accExtra = extraFields()

// ExtraChannels lists what the extra log holds for ACC.
func (g *Game) ExtraChannels() []game.ExtraChannel {
	out := make([]game.ExtraChannel, len(accExtra))
	for i, f := range accExtra {
		out[i] = f.ch
	}
	return out
}

// parseExtra fills dst from the two pages. live = a session is running.
func parseExtra(phys, graph []byte, dst []float32) (live bool) {
	if len(phys) < extraPhysicsSize || len(graph) < extraGraphicsSize {
		return false
	}
	// the player's car among the cars on track
	carOff := -1
	if n := int(i32(graph, 252)); n > 0 && n <= 60 {
		player := i32(graph, 1216)
		for i := 0; i < n; i++ {
			if i32(graph, 976+4*i) == player {
				carOff = 256 + 12*i
				break
			}
		}
	}
	for i, f := range accExtra {
		var b []byte
		off := f.off
		switch f.page {
		case 'p':
			b = phys
		case 'g':
			b = graph
		default:
			if carOff < 0 {
				dst[i] = 0
				continue
			}
			b, off = graph, carOff+f.off
		}
		if f.isInt {
			dst[i] = float32(i32(b, off))
		} else {
			dst[i] = float32(f32(b, off))
		}
	}
	return i32(graph, offStatus) == statusLive
}

// TelemetryFolder is where ACC writes its MoTeC files (D-160); "" when it doesn't exist.
func (g *Game) TelemetryFolder() string {
	doc := documentsDir()
	if doc == "" {
		return ""
	}
	p := filepath.Join(doc, "Assetto Corsa Competizione", "MoTeC")
	if isDir(p) {
		return p
	}
	return ""
}

// CarInfo and TrackName give readable names for telemetry of cars and tracks without setups.
func (g *Game) CarInfo(id string) (name, brand, class string) {
	name, brand, class, _ = CarInfo(id)
	return
}

// TrackName returns ACC's readable track name.
func (g *Game) TrackName(id string) string { return TrackName(id) }
