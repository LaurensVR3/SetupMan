package telemetry

import (
	"math"
	"time"
)

type zoneMod struct {
	start, end float64
	gripMul    float64 // >1 = faster apex, <1 = slower apex
	brakeMul   float64 // <1 = braked earlier / weaker decel
	accelMul   float64 // <1 = slower exit / TC cut
	addTC      bool
	addABS     bool
}

// BuildNords24Samples synthesises two physically consistent Nürburgring 24h laps
// (McLaren 720S GT3 Evo, 25.378 km) so users can explore lap comparison immediately
// before driving live laps or importing real MoTeC .ld files.
func BuildNords24Samples() (refMeta, cmpMeta LapMeta, refTrace, cmpTrace *Trace) {
	const (
		bins      = 1200
		trackLenM = 25378.0
		refLapMs  = 492430 // 8:12.430
		cmpLapMs  = 494890 // 8:14.890 (+2.460 s)
	)
	x, y, curv := Nords24Centerline(bins)

	// Per-zone grip/braking/exit modifiers for the comparison lap so it has realistic
	// sections where the driver is FASTER than reference and clear places where time is lost.
	cmpMods := []zoneMod{
		{start: 0.008, end: 0.058, gripMul: 1.045, brakeMul: 1.04, accelMul: 1.02},               // Faster in GP Arena
		{start: 0.248, end: 0.298, gripMul: 1.038, brakeMul: 1.02, accelMul: 1.01},               // Faster through Flugplatz
		{start: 0.305, end: 0.362, gripMul: 0.935, brakeMul: 0.86, accelMul: 0.97},               // Early brake & slower at Aremberg
		{start: 0.368, end: 0.405, gripMul: 1.035, brakeMul: 1.02, accelMul: 1.01},               // Faster through Fuchsröhre
		{start: 0.408, end: 0.444, gripMul: 0.910, brakeMul: 0.88, accelMul: 0.93},               // Slower apex + late throttle at Adenauer Forst
		{start: 0.518, end: 0.578, gripMul: 0.925, brakeMul: 0.84, accelMul: 0.96, addABS: true}, // ABS lockup into Wehrseifen
		{start: 0.632, end: 0.675, gripMul: 0.930, brakeMul: 0.95, accelMul: 0.89, addTC: true},  // TC cut exiting Karussell
		{start: 0.682, end: 0.750, gripMul: 1.032, brakeMul: 1.02, accelMul: 1.01},               // Faster through Hohe Acht
		{start: 0.812, end: 0.865, gripMul: 0.950, brakeMul: 0.92, accelMul: 0.96},               // Slight lift at Pflanzgarten
		{start: 0.870, end: 0.958, gripMul: 0.935, brakeMul: 0.94, accelMul: 0.955, addTC: true}, // Slower exit at Galgenkopf costing Döttinger Höhe speed
	}

	refTrace = synthNordsLap(x, y, curv, trackLenM, refLapMs, nil)
	cmpTrace = synthNordsLap(x, y, curv, trackLenM, cmpLapMs, cmpMods)

	now := time.Now().Truncate(time.Second)
	refMeta = LapMeta{
		Game:         "acc",
		Car:          "mclaren_720s_gt3_evo",
		Track:        "nurburgring_24h",
		Source:       SourceSample,
		Label:        "Sample Reference (8:12.430)",
		LapMs:        refLapMs,
		LapNumber:    1,
		Valid:        true,
		IsReference:  true,
		TrackLengthM: trackLenM,
		RecordedAt:   now.Add(-15 * time.Minute),
		HasCoords:    true,
		Stats:        ComputeStats(refTrace),
	}
	cmpMeta = LapMeta{
		Game:         "acc",
		Car:          "mclaren_720s_gt3_evo",
		Track:        "nurburgring_24h",
		Source:       SourceSample,
		Label:        "Sample Comparison (8:14.890)",
		LapMs:        cmpLapMs,
		LapNumber:    2,
		Valid:        true,
		IsReference:  false,
		TrackLengthM: trackLenM,
		RecordedAt:   now.Add(-6 * time.Minute),
		HasCoords:    true,
		Stats:        ComputeStats(cmpTrace),
	}
	return refMeta, cmpMeta, refTrace, cmpTrace
}

func synthNordsLap(x, y []float32, curv []float64, trackLenM float64, targetLapMs int, mods []zoneMod) *Trace {
	n := len(x)
	ds := trackLenM / float64(n-1)

	// Smooth curvature slightly so speed limits feel natural.
	sc := make([]float64, n)
	for i := 0; i < n; i++ {
		var sum float64
		for k := -2; k <= 2; k++ {
			sum += curv[(i+k+n)%n]
		}
		sc[i] = sum / 5.0
	}

	// Base GT3 speed limit (m/s) from real-metre curvature (kappa = 1/R in 1/m): v_max = sqrt(a_lat_max / |kappa|).
	const (
		topSpeedMS = 274.0 / 3.6 // ~76.1 m/s on Döttinger Höhe
		baseLatG   = 1.65        // GT3 aero + slick grip
		baseBrakeG = 1.55        // GT3 threshold braking
	)

	vLimit := make([]float64, n)
	brakeG := make([]float64, n)
	accelMul := make([]float64, n)
	tcZone := make([]bool, n)
	absZone := make([]bool, n)

	for i := 0; i < n; i++ {
		frac := float64(i) / float64(n-1)
		gMul, bMul, aMul := 1.0, 1.0, 1.0
		for _, m := range mods {
			if frac >= m.start && frac <= m.end {
				gMul *= m.gripMul
				bMul *= m.brakeMul
				aMul *= m.accelMul
				if m.addTC {
					tcZone[i] = true
				}
				if m.addABS {
					absZone[i] = true
				}
			}
		}
		k := math.Abs(sc[i])
		vMax := topSpeedMS
		if k > 0.0005 {
			aLat := baseLatG * gMul * 9.81
			vMax = math.Sqrt(aLat / (k * 1.18))
		}
		// Keep Döttinger Höhe cleanly flat-out.
		if frac > 0.885 && frac < 0.956 {
			vMax = topSpeedMS * (0.985 + 0.015*gMul)
		}
		vLimit[i] = clamp(vMax, 18.0*gMul, topSpeedMS)
		brakeG[i] = baseBrakeG * bMul
		accelMul[i] = aMul
	}

	// Forward-backward quasi-steady-state lap physics (run 2 passes on closed loop).
	v := make([]float64, n)
	copy(v, vLimit)
	for pass := 0; pass < 2; pass++ {
		// Backward pass: braking deceleration limit.
		for i := n - 2; i >= 0; i-- {
			aDecel := brakeG[i] * 9.81
			vNext := v[i+1]
			vAllow := math.Sqrt(vNext*vNext + 2*aDecel*ds)
			if vAllow < v[i] {
				v[i] = vAllow
			}
		}
		// Wrap finish -> start.
		vWrap := math.Sqrt(v[0]*v[0] + 2*brakeG[n-1]*9.81*ds)
		if vWrap < v[n-1] {
			v[n-1] = vWrap
		}

		// Forward pass: GT3 acceleration (drops with aero drag at higher speeds).
		for i := 1; i < n; i++ {
			vPrev := v[i-1]
			// GT3 acceleration: ~0.95 G in 1st/2nd tapering to ~0.16 G near 265 km/h.
			dragFrac := clamp01(vPrev / topSpeedMS)
			aAccel := (0.92*(1-0.82*dragFrac*dragFrac) + 0.05) * 9.81 * accelMul[i]
			vAllow := math.Sqrt(vPrev*vPrev + 2*aAccel*ds)
			if vAllow < v[i] {
				v[i] = vAllow
			}
		}
	}

	// Integrate time along distance, then calibrate speed slightly so total lap time matches targetLapMs cleanly.
	rawTime := make([]float64, n)
	for i := 1; i < n; i++ {
		vAvg := (v[i-1] + v[i]) * 0.5
		if vAvg < 5 {
			vAvg = 5
		}
		rawTime[i] = rawTime[i-1] + ds/vAvg
	}
	targetSec := float64(targetLapMs) / 1000.0
	scaleV := rawTime[n-1] / targetSec // if rawTime > targetSec, scale speeds slightly up
	for i := 0; i < n; i++ {
		v[i] *= scaleV
	}

	// Re-integrate exact TimeS after gentle velocity scaling so speed & delta stay 100% physically linked!
	tr := &Trace{
		N:            n,
		TrackLengthM: trackLenM,
		HasCoords:    true,
		DistM:        make([]float32, n),
		TimeS:        make([]float32, n),
		SpeedKmh:     make([]float32, n),
		Throttle:     make([]float32, n),
		Brake:        make([]float32, n),
		SteerDeg:     make([]float32, n),
		Gear:         make([]int8, n),
		RPM:          make([]int32, n),
		TC:           make(FlagSlice, n),
		ABS:          make(FlagSlice, n),
		GLat:         make([]float32, n),
		GLon:         make([]float32, n),
		X:            append([]float32(nil), x...),
		Y:            append([]float32(nil), y...),
	}

	var tCum float64
	for i := 0; i < n; i++ {
		tr.DistM[i] = float32(float64(i) * ds)
		if i > 0 {
			vAvg := (v[i-1] + v[i]) * 0.5
			tCum += ds / vAvg
		}
		tr.TimeS[i] = float32(tCum)
		kmh := v[i] * 3.6
		tr.SpeedKmh[i] = float32(round1(kmh))

		// Longitudinal acceleration in G.
		iPrev := i - 1
		if iPrev < 0 {
			iPrev = 0
		}
		iNext := i + 1
		if iNext >= n {
			iNext = n - 1
		}
		dv := v[iNext] - v[iPrev]
		spanD := float64(iNext-iPrev) * ds
		aLon := 0.0
		if spanD > 0 {
			aLon = (v[i] * dv / spanD) / 9.81
		}
		tr.GLon[i] = float32(clamp(aLon, -2.0, 1.4))

		// Lateral G and steering angle from real-metre curvature (1/m).
		aLat := (v[i] * v[i] * sc[i]) / 9.81
		tr.GLat[i] = float32(clamp(aLat, -2.5, 2.5))
		// Steering angle in degrees (McLaren 720S GT3 typical racing range -115..+115°).
		steer := clamp(sc[i]*9200.0, -115, 115)
		tr.SteerDeg[i] = float32(round1(steer))

		// Realistic throttle & brake pedal traces derived from longitudinal acceleration & cornering.
		var thr, brk float64
		switch {
		case aLon < -0.18:
			// Braking zone with trail braking into apex.
			brk = clamp((-aLon-0.12)/1.35*100.0, 8, 100)
			thr = 0
		case aLon < 0.03 && math.Abs(aLat) > 0.75:
			// Mid-corner coast / maintenance throttle.
			thr = clamp(35+(aLon+0.18)*180, 15, 70)
			if aLon < -0.04 {
				brk = clamp((-aLon-0.03)*160, 0, 22)
			}
		default:
			// Accelerating or flat-out straight.
			thr = 100
			if math.Abs(aLat) > 1.25 && aLon < 0.25 {
				thr = clamp(72+aLon*90, 55, 100)
			}
		}
		tr.Throttle[i] = float32(round1(thr))
		tr.Brake[i] = float32(round1(brk))

		// Realistic McLaren 720S GT3 Evo 6-speed gearbox & RPM sawtooth (shift ~7600 RPM).
		gear, rpm := gt3GearRPM(kmh)
		tr.Gear[i] = int8(gear)
		tr.RPM[i] = int32(rpm)

		// TC and ABS blips.
		if brk > 82 && (absZone[i] || math.Abs(aLat) > 0.85) {
			tr.ABS[i] = 1
		}
		if thr > 85 && math.Abs(aLat) > 0.95 && kmh < 165 && (tcZone[i] || kmh < 95) {
			tr.TC[i] = 1
		}
	}
	return tr
}

func gt3GearRPM(kmh float64) (gear int, rpm int) {
	// Realistic GT3 gear speed ranges (km/h): 1:0-102, 2:98-140, 3:135-178, 4:172-215, 5:210-252, 6:246-290
	type gr struct {
		g         int
		loV, hiV  float64
		loR, hiR  float64
	}
	gears := []gr{
		{1, 0, 102, 3200, 7750},
		{2, 96, 140, 5400, 7750},
		{3, 134, 178, 5600, 7750},
		{4, 172, 215, 5750, 7750},
		{5, 210, 252, 5850, 7750},
		{6, 246, 290, 5950, 7800},
	}
	for _, g := range gears {
		if kmh <= g.hiV || g.g == 6 {
			f := clamp01((kmh - g.loV) / (g.hiV - g.loV))
			return g.g, int(math.Round(g.loR + f*(g.hiR-g.loR)))
		}
	}
	return 6, 7200
}
