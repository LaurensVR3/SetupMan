package telemetry

import (
	"fmt"
	"math"
	"strings"
)

// Section is a named segment along a track (normalized distance 0..1).
type Section struct {
	Name  string  `json:"name"`
	Start float64 `json:"start"`
	End   float64 `json:"end"`
}

// trackSections lists named sections for popular ACC circuits.
// Unknown tracks fall back to 10 equal sectors.
var trackSections = map[string][]Section{
	"nurburgring_24h": {
		{Name: "GP Arena (T1–T4)", Start: 0.000, End: 0.060},
		{Name: "GP Dunlop & Schumacher-S", Start: 0.060, End: 0.170},
		{Name: "Hatzenbach", Start: 0.170, End: 0.246},
		{Name: "Flugplatz", Start: 0.246, End: 0.300},
		{Name: "Schwedenkreuz & Aremberg", Start: 0.300, End: 0.365},
		{Name: "Fuchsröhre & Adenauer Forst", Start: 0.365, End: 0.446},
		{Name: "Metzgesfeld & Kallenhard", Start: 0.446, End: 0.515},
		{Name: "Wehrseifen & Ex-Mühle", Start: 0.515, End: 0.580},
		{Name: "Bergwerk & Kesselchen", Start: 0.580, End: 0.630},
		{Name: "Klostertal & Karussell", Start: 0.630, End: 0.677},
		{Name: "Hohe Acht & Wippermann", Start: 0.677, End: 0.754},
		{Name: "Brünnchen & Eiskurve", Start: 0.754, End: 0.808},
		{Name: "Pflanzgarten & Schwalbenschwanz", Start: 0.808, End: 0.869},
		{Name: "Galgenkopf & Döttinger Höhe", Start: 0.869, End: 0.962},
		{Name: "Tiergarten & Hohenrain", Start: 0.962, End: 1.000},
	},
	"spa": {
		{Name: "La Source", Start: 0.000, End: 0.090},
		{Name: "Eau Rouge & Raidillon", Start: 0.090, End: 0.210},
		{Name: "Kemmel & Les Combes", Start: 0.210, End: 0.360},
		{Name: "Malmedy & Bruxelles", Start: 0.360, End: 0.480},
		{Name: "Pouhon", Start: 0.480, End: 0.600},
		{Name: "Campus & Stavelot", Start: 0.600, End: 0.740},
		{Name: "Blanchimont", Start: 0.740, End: 0.880},
		{Name: "Bus Stop Chicane", Start: 0.880, End: 1.000},
	},
	"monza": {
		{Name: "Main Straight & Prima Variante", Start: 0.000, End: 0.180},
		{Name: "Curva Grande & Roggia", Start: 0.180, End: 0.380},
		{Name: "Lesmo 1 & 2", Start: 0.380, End: 0.560},
		{Name: "Serraglio & Ascari", Start: 0.560, End: 0.780},
		{Name: "Parabolica", Start: 0.780, End: 1.000},
	},
	"nurburgring": {
		{Name: "T1 & Mercedes Arena", Start: 0.000, End: 0.240},
		{Name: "Valvoline & Ford-Kurve", Start: 0.240, End: 0.420},
		{Name: "Dunlop Hairpin", Start: 0.420, End: 0.560},
		{Name: "Schumacher-S", Start: 0.560, End: 0.720},
		{Name: "Bit-Kurve & NGK Chicane", Start: 0.720, End: 1.000},
	},
	"imola": {
		{Name: "Main Straight", Start: 0.000, End: 0.130},
		{Name: "Tamburello (T2–T4)", Start: 0.130, End: 0.220},
		{Name: "Villeneuve (T5–T6)", Start: 0.220, End: 0.315},
		{Name: "Tosa Hairpin (T7)", Start: 0.315, End: 0.420},
		{Name: "Piratella (T9)", Start: 0.420, End: 0.520},
		{Name: "Acque Minerali (T11–T13)", Start: 0.520, End: 0.630},
		{Name: "Variante Alta (T14–T15)", Start: 0.630, End: 0.760},
		{Name: "Rivazza 1 & 2 (T17–T18)", Start: 0.760, End: 0.900},
		{Name: "Traguardo Straight", Start: 0.900, End: 1.000},
	},
	"barcelona": {
		{Name: "Elf & Renault (T1–T3)", Start: 0.000, End: 0.240},
		{Name: "Repsol & Seat (T4–T5)", Start: 0.240, End: 0.440},
		{Name: "Wurth & Campsa (T7–T9)", Start: 0.440, End: 0.640},
		{Name: "La Caixa & Banc Sabadell (T10–T12)", Start: 0.640, End: 0.840},
		{Name: "Final Sector & Straight", Start: 0.840, End: 1.000},
	},
	"paul_ricard": {
		{Name: "S de la Verrerie (T1–T2)", Start: 0.000, End: 0.160},
		{Name: "Hôtel & La Sainte Baume (T3–T6)", Start: 0.160, End: 0.340},
		{Name: "Mistral Straight", Start: 0.340, End: 0.580},
		{Name: "Signes & Beausset (T10–T11)", Start: 0.580, End: 0.780},
		{Name: "Bendor & Pont (T12–T15)", Start: 0.780, End: 1.000},
	},
	"red_bull_ring": {
		{Name: "Niki Lauda Kurve (T1)", Start: 0.000, End: 0.160},
		{Name: "Remus Hairpin (T3)", Start: 0.160, End: 0.410},
		{Name: "Schlossgold (T4)", Start: 0.410, End: 0.590},
		{Name: "Rauch & Würth (T6–T7)", Start: 0.590, End: 0.790},
		{Name: "Rindt & Red Bull Mobile (T9–T10)", Start: 0.790, End: 1.000},
	},
	"silverstone": {
		{Name: "Abbey & Farm (T1–T2)", Start: 0.000, End: 0.140},
		{Name: "Village, The Loop & Wellington", Start: 0.140, End: 0.320},
		{Name: "Brooklands & Luffield (T6–T7)", Start: 0.320, End: 0.460},
		{Name: "Copse (T9)", Start: 0.460, End: 0.580},
		{Name: "Maggotts & Becketts (T10–T14)", Start: 0.580, End: 0.740},
		{Name: "Hangar Straight & Stowe (T15)", Start: 0.740, End: 0.880},
		{Name: "Vale & Club (T16–T18)", Start: 0.880, End: 1.000},
	},
	"hungaroring": {
		{Name: "Turn 1 & Turn 2", Start: 0.000, End: 0.240},
		{Name: "Mansell & Turn 5 (T4–T5)", Start: 0.240, End: 0.420},
		{Name: "Chicane (T6–T7)", Start: 0.420, End: 0.560},
		{Name: "Mid-Sector Esses (T8–T11)", Start: 0.560, End: 0.780},
		{Name: "Final Hairpins (T12–T14)", Start: 0.780, End: 1.000},
	},
	"suzuka": {
		{Name: "First Curve & Esses (T1–T6)", Start: 0.000, End: 0.260},
		{Name: "Dunlop & Degner (T7–T9)", Start: 0.260, End: 0.420},
		{Name: "Hairpin (T11)", Start: 0.420, End: 0.540},
		{Name: "Spoon Curve (T13–T14)", Start: 0.540, End: 0.720},
		{Name: "Back Straight & 130R (T15)", Start: 0.720, End: 0.880},
		{Name: "Casio Triangle (T16–T18)", Start: 0.880, End: 1.000},
	},
	"cota": {
		{Name: "Turn 1 Hill", Start: 0.000, End: 0.140},
		{Name: "The Esses (T2–T9)", Start: 0.140, End: 0.360},
		{Name: "Turn 11 & Back Straight", Start: 0.360, End: 0.620},
		{Name: "Stadium Complex (T12–T15)", Start: 0.620, End: 0.780},
		{Name: "Multi-Apex Right (T16–T18)", Start: 0.780, End: 0.900},
		{Name: "Turns 19–20", Start: 0.900, End: 1.000},
	},
	"zandvoort": {
		{Name: "Tarzan & Gerlach (T1–T2)", Start: 0.000, End: 0.180},
		{Name: "Hugenholtz & Hunzerug (T3–T6)", Start: 0.180, End: 0.380},
		{Name: "Scheivlak (T7)", Start: 0.380, End: 0.520},
		{Name: "Mastersbocht & Turn 9–10", Start: 0.520, End: 0.720},
		{Name: "Hans Ernst Chicane (T11–T12)", Start: 0.720, End: 0.860},
		{Name: "Kumho & Arie Luyendyk (T13–T14)", Start: 0.860, End: 1.000},
	},
	"brands_hatch": {
		{Name: "Paddock Hill & Druids (T1–T2)", Start: 0.000, End: 0.220},
		{Name: "Graham Hill & Surtees (T3–T4)", Start: 0.220, End: 0.420},
		{Name: "Pilgrim's Drop & Hawthorn (T5)", Start: 0.420, End: 0.600},
		{Name: "Westfield & Dingle Dell (T6–T7)", Start: 0.600, End: 0.780},
		{Name: "Stirlings & Clearways (T8–T9)", Start: 0.780, End: 1.000},
	},
	"kyalami": {
		{Name: "Crowthorne & Barbeque (T1–T4)", Start: 0.000, End: 0.260},
		{Name: "Sunset & Clubhouse (T5–T7)", Start: 0.260, End: 0.480},
		{Name: "The Esses & Leeukop (T8–T10)", Start: 0.480, End: 0.720},
		{Name: "The Kink & Mineshaft (T11–T13)", Start: 0.720, End: 0.880},
		{Name: "Crocodiles & Main Straight", Start: 0.880, End: 1.000},
	},
	"watkins_glen": {
		{Name: "Turn 1 & The Esses (T1–T4)", Start: 0.000, End: 0.280},
		{Name: "Back Straight & Bus Stop", Start: 0.280, End: 0.480},
		{Name: "Outer Loop & The Chute", Start: 0.480, End: 0.620},
		{Name: "The Boot (T6–T9)", Start: 0.620, End: 0.840},
		{Name: "Turns 10–11 & Straight", Start: 0.840, End: 1.000},
	},
	"indianapolis": {
		{Name: "Main Straight & T1–T4", Start: 0.000, End: 0.280},
		{Name: "Infield Chicane (T5–T6)", Start: 0.280, End: 0.460},
		{Name: "Hulman Straight & T7", Start: 0.460, End: 0.660},
		{Name: "Infield Complex (T8–T11)", Start: 0.660, End: 0.840},
		{Name: "Final Corners & Banking", Start: 0.840, End: 1.000},
	},
	"mount_panorama": {
		{Name: "Hell Corner & Mountain Straight", Start: 0.000, End: 0.220},
		{Name: "The Cutting & Reid Park", Start: 0.220, End: 0.400},
		{Name: "Skyline & The Dipper", Start: 0.400, End: 0.560},
		{Name: "Forrest's Elbow & Conrod", Start: 0.560, End: 0.840},
		{Name: "The Chase & Murray's", Start: 0.840, End: 1.000},
	},
}

// SectionsForTrack returns the named sections for a track (or 10 equal sectors).
func SectionsForTrack(trackID string) []Section {
	if s, ok := trackSections[strings.ToLower(strings.TrimSpace(trackID))]; ok {
		out := make([]Section, len(s))
		copy(out, s)
		return out
	}
	const n = 10
	out := make([]Section, n)
	for i := 0; i < n; i++ {
		out[i] = Section{
			Name:  fmt.Sprintf("Sector %d", i+1),
			Start: float64(i) / n,
			End:   float64(i+1) / n,
		}
	}
	return out
}

// TrackLengthHint returns the canonical length in metres for known ACC tracks.
func TrackLengthHint(trackID string) float64 {
	switch strings.ToLower(strings.TrimSpace(trackID)) {
	case "nurburgring_24h":
		return 25378
	case "spa":
		return 7004
	case "mount_panorama":
		return 6213
	case "silverstone":
		return 5891
	case "paul_ricard":
		return 5842
	case "suzuka":
		return 5807
	case "monza":
		return 5793
	case "cota":
		return 5514
	case "watkins_glen":
		return 5430
	case "nurburgring":
		return 5137
	case "imola":
		return 4909
	case "snetterton":
		return 4779
	case "barcelona":
		return 4655
	case "kyalami":
		return 4529
	case "hungaroring":
		return 4381
	case "oulton_park":
		return 4332
	case "red_bull_ring":
		return 4318
	case "zandvoort":
		return 4259
	case "misano":
		return 4226
	case "indianapolis":
		return 4192
	case "donington":
		return 4020
	case "zolder":
		return 4011
	case "valencia":
		return 4005
	case "brands_hatch":
		return 3916
	case "laguna_seca":
		return 3602
	default:
		return 5000
	}
}

type pt2 struct{ x, y float64 }

// nords24Waypoints is the real-proportion Nürburgring 24h (GP-Strecke + Nordschleife)
// centerline in metres (+X = East, +Y = North), starting at the GP Start/Finish line
// heading SW into T1 and following the exact 24h loop (adapted from Wikimedia Commons
// "Circuit Nürburgring-2002-24h.svg", CC BY-SA 3.0).
var nords24Waypoints = []pt2{
	{-1070.1, -1836.2}, {-1137.3, -1906.9}, {-1204.6, -1977.7}, {-1271.9, -2048.4}, {-1345.4, -2112.1}, {-1423.1, -2171.2},
	{-1507.3, -2160.9}, {-1571.4, -2212.2}, {-1594.3, -2307.0}, {-1623.6, -2400.1}, {-1651.4, -2493.7}, {-1651.1, -2588.1},
	{-1567.9, -2637.1}, {-1532.4, -2707.2}, {-1614.6, -2758.9}, {-1697.5, -2809.9}, {-1771.6, -2873.4}, {-1839.9, -2943.1},
	{-1899.7, -3020.2}, {-1984.3, -3030.1}, {-1961.1, -2947.6}, {-1891.6, -2879.2}, {-1826.4, -2806.5}, {-1777.2, -2723.7},
	{-1795.3, -2628.1}, {-1777.1, -2532.7}, {-1747.5, -2439.7}, {-1716.2, -2347.2}, {-1687.3, -2254.0}, {-1722.9, -2174.6},
	{-1809.0, -2128.7}, {-1830.6, -2050.9}, {-1772.3, -1972.7}, {-1707.7, -1899.6}, {-1639.8, -1829.5}, {-1566.7, -1765.0},
	{-1473.2, -1737.5}, {-1377.8, -1716.9}, {-1282.2, -1697.2}, {-1186.7, -1677.3}, {-1091.4, -1655.8}, {-1028.7, -1587.7},
	{-948.7, -1541.1}, {-894.9, -1464.0}, {-929.3, -1376.1}, {-1018.5, -1380.8}, {-1074.2, -1460.7}, {-1147.9, -1524.3},
	{-1238.0, -1559.2}, {-1332.0, -1539.2}, {-1421.8, -1501.4}, {-1518.4, -1489.5}, {-1612.0, -1511.5}, {-1693.6, -1468.0},
	{-1752.8, -1390.6}, {-1815.5, -1317.5}, {-1895.3, -1273.3}, {-1990.8, -1254.3}, {-2038.0, -1172.2}, {-2101.4, -1099.9},
	{-2180.5, -1042.8}, {-2259.5, -985.5}, {-2330.1, -918.4}, {-2394.9, -845.5}, {-2459.9, -772.7}, {-2522.0, -697.5},
	{-2554.4, -606.8}, {-2538.7, -510.8}, {-2508.2, -418.1}, {-2484.0, -323.7}, {-2478.4, -226.3}, {-2478.9, -128.7},
	{-2488.2, -31.6}, {-2509.0, 63.5}, {-2545.0, 154.2}, {-2582.7, 244.2}, {-2622.9, 333.2}, {-2665.7, 420.9},
	{-2711.6, 507.0}, {-2777.1, 578.6}, {-2862.1, 626.2}, {-2950.3, 668.1}, {-2971.0, 752.5}, {-2885.5, 796.8},
	{-2795.4, 834.1}, {-2709.0, 879.6}, {-2630.0, 936.6}, {-2555.2, 999.3}, {-2479.5, 1061.0}, {-2407.4, 1126.7},
	{-2338.7, 1195.9}, {-2287.9, 1278.9}, {-2253.1, 1370.0}, {-2239.9, 1466.4}, {-2234.8, 1563.4}, {-2169.6, 1634.3},
	{-2181.5, 1727.9}, {-2133.9, 1806.2}, {-2055.1, 1863.8}, {-1989.4, 1935.5}, {-1923.2, 2007.0}, {-1851.5, 2073.2},
	{-1785.3, 2144.9}, {-1735.1, 2228.0}, {-1727.8, 2324.1}, {-1760.2, 2415.9}, {-1833.1, 2469.0}, {-1925.0, 2495.4},
	{-1999.6, 2557.6}, {-1998.7, 2642.9}, {-1912.3, 2688.0}, {-1833.1, 2744.9}, {-1792.2, 2829.0}, {-1755.6, 2918.2},
	{-1681.6, 2979.4}, {-1586.4, 2975.1}, {-1500.0, 2929.9}, {-1421.8, 2871.7}, {-1369.7, 2792.2}, {-1294.8, 2837.6},
	{-1203.4, 2870.5}, {-1106.3, 2878.2}, {-1010.1, 2863.4}, {-915.6, 2840.6}, {-839.6, 2892.7}, {-828.1, 2989.2},
	{-785.8, 3073.4}, {-698.7, 3116.6}, {-606.2, 3147.8}, {-511.1, 3169.4}, {-413.9, 3178.0}, {-321.0, 3205.2},
	{-242.9, 3263.4}, {-164.6, 3321.4}, {-72.6, 3312.8}, {-43.9, 3223.1}, {-49.6, 3125.7}, {-54.3, 3028.3},
	{-44.5, 2931.3}, {-16.3, 2838.1}, {40.4, 2759.6}, {116.2, 2698.3}, {201.0, 2650.2}, {289.8, 2609.8},
	{385.1, 2590.9}, {482.7, 2589.5}, {580.3, 2586.7}, {677.6, 2578.7}, {773.6, 2561.4}, {867.7, 2535.8},
	{956.0, 2494.8}, {1033.1, 2435.0}, {1113.8, 2380.4}, {1203.0, 2341.3}, {1299.6, 2336.6}, {1388.5, 2375.9},
	{1468.1, 2432.3}, {1539.7, 2498.6}, {1619.6, 2553.3}, {1715.6, 2570.0}, {1812.2, 2583.1}, {1905.5, 2610.2},
	{1950.6, 2542.1}, {1881.3, 2475.0}, {1803.5, 2416.1}, {1721.5, 2363.6}, {1661.1, 2298.5}, {1730.1, 2247.4},
	{1796.9, 2311.6}, {1876.7, 2367.4}, {1966.2, 2406.2}, {2059.8, 2433.3}, {2152.4, 2459.6}, {2222.1, 2527.5},
	{2279.1, 2606.7}, {2287.4, 2699.7}, {2303.1, 2793.7}, {2368.0, 2864.2}, {2464.7, 2860.1}, {2559.5, 2836.9},
	{2652.2, 2806.7}, {2725.3, 2744.0}, {2804.8, 2690.2}, {2892.9, 2650.7}, {2952.3, 2573.5}, {2988.3, 2484.9},
	{2958.8, 2392.0}, {2935.4, 2297.6}, {2985.3, 2221.1}, {3075.5, 2184.5}, {3111.6, 2097.8}, {3111.7, 2000.4},
	{3049.6, 1935.9}, {2952.6, 1941.9}, {2855.6, 1946.7}, {2800.2, 1874.4}, {2749.1, 1791.6}, {2702.5, 1706.1},
	{2679.7, 1611.8}, {2681.9, 1514.2}, {2682.3, 1416.6}, {2653.6, 1325.5}, {2566.7, 1284.8}, {2478.2, 1246.3},
	{2409.0, 1177.6}, {2347.4, 1102.1}, {2305.2, 1014.2}, {2250.4, 933.8}, {2172.0, 877.0}, {2092.4, 820.9},
	{1999.5, 791.1}, {1906.6, 761.4}, {1812.9, 734.0}, {1717.0, 716.7}, {1627.7, 746.4}, {1548.8, 802.3},
	{1455.9, 783.7}, {1375.3, 728.8}, {1350.3, 644.4}, {1420.9, 579.9}, {1511.2, 543.0}, {1600.2, 503.4},
	{1665.8, 433.1}, {1694.9, 340.8}, {1672.2, 248.5}, {1600.1, 183.3}, {1518.4, 130.0}, {1435.1, 79.1},
	{1351.3, 29.1}, {1267.2, -20.5}, {1183.2, -70.2}, {1099.7, -120.7}, {1016.7, -172.2}, {933.8, -223.7},
	{850.9, -275.1}, {767.9, -326.6}, {685.0, -378.1}, {602.1, -429.5}, {519.1, -481.0}, {436.2, -532.5},
	{353.2, -583.9}, {270.3, -635.4}, {187.2, -686.6}, {103.9, -737.5}, {20.7, -788.5}, {-62.1, -840.2},
	{-143.5, -894.1}, {-217.7, -957.3}, {-284.6, -1028.3}, {-346.3, -1104.0}, {-403.9, -1182.8}, {-458.5, -1263.7},
	{-522.7, -1336.6}, {-584.1, -1410.0}, {-616.8, -1499.2}, {-710.9, -1518.2}, {-800.2, -1554.1}, {-868.2, -1624.1},
	{-935.5, -1694.8}, {-1002.8, -1765.5},
}

// TrackCenterline returns a smooth, arc-length-parameterized closed loop for any supported
// ACC track (e.g. "imola", "nurburgring_24h", "spa", "monza", ...), with N bins and signed
// local curvature. Returns ok=false if the track has no built-in centerline map.
func TrackCenterline(trackID string, bins int) (x, y []float32, curv []float64, ok bool) {
	wp, exists := trackWaypoints[strings.ToLower(strings.TrimSpace(trackID))]
	if !exists || len(wp) < 10 {
		return nil, nil, nil, false
	}
	x, y, curv = buildCenterline(wp, bins)
	return x, y, curv, true
}

// KnownTrackMaps lists all track IDs that have built-in real-world GPS centerlines.
func KnownTrackMaps() []string {
	out := make([]string, 0, len(trackWaypoints))
	for id := range trackWaypoints {
		out = append(out, id)
	}
	return out
}

// Nords24Centerline returns a smooth, arc-length-parameterized closed loop
// for Nürburgring 24h with N bins, plus signed local curvature at each bin.
func Nords24Centerline(bins int) (x, y []float32, curv []float64) {
	return buildCenterline(nords24Waypoints, bins)
}

func buildCenterline(waypoints []pt2, bins int) (x, y []float32, curv []float64) {
	if bins < 100 {
		bins = 600
	}
	// Dense Catmull-Rom spline over closed waypoints.
	const sub = 12
	m := len(waypoints)
	dense := make([]pt2, 0, m*sub+1)
	for i := 0; i < m; i++ {
		p0 := waypoints[(i-1+m)%m]
		p1 := waypoints[i]
		p2 := waypoints[(i+1)%m]
		p3 := waypoints[(i+2)%m]
		for s := 0; s < sub; s++ {
			t := float64(s) / float64(sub)
			dense = append(dense, catmullRom(p0, p1, p2, p3, t))
		}
	}
	dense = append(dense, dense[0])

	// Arc-length parameterization.
	cum := make([]float64, len(dense))
	for i := 1; i < len(dense); i++ {
		cum[i] = cum[i-1] + math.Hypot(dense[i].x-dense[i-1].x, dense[i].y-dense[i-1].y)
	}
	total := cum[len(cum)-1]

	px := make([]float64, bins)
	py := make([]float64, bins)
	j := 0
	for i := 0; i < bins; i++ {
		target := (float64(i) / float64(bins-1)) * total
		for j+1 < len(cum)-1 && cum[j+1] < target {
			j++
		}
		a := 0.0
		if span := cum[j+1] - cum[j]; span > 1e-9 {
			a = (target - cum[j]) / span
		}
		px[i] = dense[j].x + (dense[j+1].x-dense[j].x)*a
		py[i] = dense[j].y + (dense[j+1].y-dense[j].y)*a
	}

	// Signed curvature via finite differences on the closed loop.
	x = make([]float32, bins)
	y = make([]float32, bins)
	curv = make([]float64, bins)
	for i := 0; i < bins; i++ {
		x[i] = float32(px[i])
		y[i] = float32(py[i])
		prev := (i - 2 + bins) % bins
		next := (i + 2) % bins
		dx1, dy1 := px[i]-px[prev], py[i]-py[prev]
		dx2, dy2 := px[next]-px[i], py[next]-py[i]
		l1 := math.Hypot(dx1, dy1)
		l2 := math.Hypot(dx2, dy2)
		if l1 > 1e-6 && l2 > 1e-6 {
			cross := (dx1*dy2 - dy1*dx2) / (l1 * l2)
			dot := clamp((dx1*dx2+dy1*dy2)/(l1*l2), -1, 1)
			ang := math.Atan2(cross, dot)
			curv[i] = ang / ((l1 + l2) * 0.5)
		}
	}
	return x, y, curv
}

func catmullRom(p0, p1, p2, p3 pt2, t float64) pt2 {
	t2 := t * t
	t3 := t2 * t
	return pt2{
		x: 0.5 * ((2 * p1.x) + (-p0.x+p2.x)*t + (2*p0.x-5*p1.x+4*p2.x-p3.x)*t2 + (-p0.x+3*p1.x-3*p2.x+p3.x)*t3),
		y: 0.5 * ((2 * p1.y) + (-p0.y+p2.y)*t + (2*p0.y-5*p1.y+4*p2.y-p3.y)*t2 + (-p0.y+3*p1.y-3*p2.y+p3.y)*t3),
	}
}

// ReconstructMap integrates speed + lateral G / yaw rate into a closed 2D track map
// when source telemetry (such as MoTeC .ld) has no explicit GPS/X-Y channels.
func ReconstructMap(tr *Trace) {
	if tr == nil || tr.N < 20 || tr.HasCoords {
		return
	}
	hasLat := false
	for _, g := range tr.GLat {
		if math.Abs(float64(g)) > 0.15 {
			hasLat = true
			break
		}
	}
	if !hasLat {
		return
	}
	n := tr.N
	xs := make([]float64, n)
	ys := make([]float64, n)
	heading := 0.0
	for i := 1; i < n; i++ {
		ds := float64(tr.DistM[i] - tr.DistM[i-1])
		dt := float64(tr.TimeS[i] - tr.TimeS[i-1])
		v := float64(tr.SpeedKmh[i]) / 3.6
		if dt > 0 && dt < 5 && v > 2 {
			// lateral acceleration a_lat (m/s^2) = v * omega  =>  dHeading = (gLat * 9.81 / v) * dt
			omega := (float64(tr.GLat[i]) * 9.81) / v
			heading += omega * dt
		}
		xs[i] = xs[i-1] + ds*math.Cos(heading)
		ys[i] = ys[i-1] + ds*math.Sin(heading)
	}
	// Linear loop-closure drift correction so start and finish meet cleanly.
	errX := xs[n-1] - xs[0]
	errY := ys[n-1] - ys[0]
	tr.X = make([]float32, n)
	tr.Y = make([]float32, n)
	for i := 0; i < n; i++ {
		f := float64(i) / float64(n-1)
		tr.X[i] = float32(xs[i] - errX*f)
		tr.Y[i] = float32(ys[i] - errY*f)
	}
	tr.HasCoords = true
}
