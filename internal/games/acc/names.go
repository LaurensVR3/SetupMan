package acc

import (
	"strings"
	"unicode"
)

// Car classes, in the order they are shown (spec D-29).
const (
	ClassGT3   = "GT3"
	ClassGT4   = "GT4"
	ClassGT2   = "GT2"
	ClassCup   = "Cup"   // Porsche Cup, Lamborghini Super Trofeo, Ferrari Challenge
	ClassTCX   = "TCX"   // BMW M2 CS Racing
	ClassOther = "Other" // unknown cars (e.g. new DLC)
)

type carInfo struct {
	Name  string
	Brand string
	Class string
	Year  int
}

// cars maps ACC's internal car names (the setup folder names) to readable names (spec D-27).
var cars = map[string]carInfo{
	// GT3
	"amr_v12_vantage_gt3":          {"Aston Martin V12 Vantage GT3", "Aston Martin", ClassGT3, 2013},
	"amr_v8_vantage_gt3":           {"Aston Martin V8 Vantage GT3", "Aston Martin", ClassGT3, 2019},
	"audi_r8_lms":                  {"Audi R8 LMS", "Audi", ClassGT3, 2015},
	"audi_r8_lms_evo":              {"Audi R8 LMS Evo", "Audi", ClassGT3, 2019},
	"audi_r8_lms_evo_ii":           {"Audi R8 LMS Evo II", "Audi", ClassGT3, 2022},
	"bentley_continental_gt3_2016": {"Bentley Continental GT3", "Bentley", ClassGT3, 2015},
	"bentley_continental_gt3_2018": {"Bentley Continental GT3", "Bentley", ClassGT3, 2018},
	"bmw_m4_gt3":                   {"BMW M4 GT3", "BMW", ClassGT3, 2021},
	"bmw_m6_gt3":                   {"BMW M6 GT3", "BMW", ClassGT3, 2017},
	"ferrari_296_gt3":              {"Ferrari 296 GT3", "Ferrari", ClassGT3, 2023},
	"ferrari_488_gt3":              {"Ferrari 488 GT3", "Ferrari", ClassGT3, 2018},
	"ferrari_488_gt3_evo":          {"Ferrari 488 GT3 Evo", "Ferrari", ClassGT3, 2020},
	"ford_mustang_gt3":             {"Ford Mustang GT3", "Ford", ClassGT3, 2024},
	"honda_nsx_gt3":                {"Honda NSX GT3", "Honda", ClassGT3, 2017},
	"honda_nsx_gt3_evo":            {"Honda NSX GT3 Evo", "Honda", ClassGT3, 2019},
	"jaguar_g3":                    {"Emil Frey Jaguar G3", "Jaguar", ClassGT3, 2012},
	"lamborghini_gallardo_rex":     {"Reiter Engineering R-EX GT3", "Lamborghini", ClassGT3, 2017},
	"lamborghini_huracan_gt3":      {"Lamborghini Huracán GT3", "Lamborghini", ClassGT3, 2015},
	"lamborghini_huracan_gt3_evo":  {"Lamborghini Huracán GT3 Evo", "Lamborghini", ClassGT3, 2019},
	"lamborghini_huracan_gt3_evo2": {"Lamborghini Huracán GT3 Evo 2", "Lamborghini", ClassGT3, 2023},
	"lexus_rc_f_gt3":               {"Lexus RC F GT3", "Lexus", ClassGT3, 2016},
	"mclaren_650s_gt3":             {"McLaren 650S GT3", "McLaren", ClassGT3, 2015},
	"mclaren_720s_gt3":             {"McLaren 720S GT3", "McLaren", ClassGT3, 2019},
	"mclaren_720s_gt3_evo":         {"McLaren 720S GT3 Evo", "McLaren", ClassGT3, 2023},
	"mercedes_amg_gt3":             {"Mercedes-AMG GT3", "Mercedes-AMG", ClassGT3, 2015},
	"mercedes_amg_gt3_evo":         {"Mercedes-AMG GT3 Evo", "Mercedes-AMG", ClassGT3, 2020},
	"nissan_gt_r_gt3_2017":         {"Nissan GT-R Nismo GT3", "Nissan", ClassGT3, 2015},
	"nissan_gt_r_gt3_2018":         {"Nissan GT-R Nismo GT3", "Nissan", ClassGT3, 2018},
	"porsche_991_gt3_r":            {"Porsche 911 GT3 R", "Porsche", ClassGT3, 2018},
	"porsche_991ii_gt3_r":          {"Porsche 911 II GT3 R", "Porsche", ClassGT3, 2019},
	"porsche_992_gt3_r":            {"Porsche 911 GT3 R (992)", "Porsche", ClassGT3, 2023},
	// GT4
	"alpine_a110_gt4":           {"Alpine A110 GT4", "Alpine", ClassGT4, 2018},
	"amr_v8_vantage_gt4":        {"Aston Martin Vantage AMR GT4", "Aston Martin", ClassGT4, 2018},
	"audi_r8_gt4":               {"Audi R8 LMS GT4", "Audi", ClassGT4, 2018},
	"bmw_m4_gt4":                {"BMW M4 GT4", "BMW", ClassGT4, 2018},
	"chevrolet_camaro_gt4r":     {"Chevrolet Camaro GT4.R", "Chevrolet", ClassGT4, 2017},
	"ginetta_g55_gt4":           {"Ginetta G55 GT4", "Ginetta", ClassGT4, 2012},
	"ktm_xbow_gt4":              {"KTM X-Bow GT4", "KTM", ClassGT4, 2016},
	"maserati_mc_gt4":           {"Maserati MC GT4", "Maserati", ClassGT4, 2016},
	"mclaren_570s_gt4":          {"McLaren 570S GT4", "McLaren", ClassGT4, 2016},
	"mercedes_amg_gt4":          {"Mercedes-AMG GT4", "Mercedes-AMG", ClassGT4, 2016},
	"porsche_718_cayman_gt4_mr": {"Porsche 718 Cayman GT4 MR", "Porsche", ClassGT4, 2019},
	// GT2
	"audi_r8_lms_gt2":       {"Audi R8 LMS GT2", "Audi", ClassGT2, 2021},
	"ktm_xbow_gt2":          {"KTM X-Bow GT2", "KTM", ClassGT2, 2021},
	"maserati_mc20_gt2":     {"Maserati MC20 GT2", "Maserati", ClassGT2, 2023},
	"mercedes_amg_gt2":      {"Mercedes-AMG GT2", "Mercedes-AMG", ClassGT2, 2023},
	"porsche_935":           {"Porsche 935", "Porsche", ClassGT2, 2019},
	"porsche_991_gt2_rs_mr": {"Porsche 911 GT2 RS CS Evo", "Porsche", ClassGT2, 2023},
	// Cup, Super Trofeo, Challenge
	"porsche_991ii_gt3_cup":       {"Porsche 911 II GT3 Cup", "Porsche", ClassCup, 2017},
	"porsche_992_gt3_cup":         {"Porsche 911 GT3 Cup (992)", "Porsche", ClassCup, 2021},
	"lamborghini_huracan_st":      {"Lamborghini Huracán Super Trofeo", "Lamborghini", ClassCup, 2015},
	"lamborghini_huracan_st_evo2": {"Lamborghini Huracán Super Trofeo Evo 2", "Lamborghini", ClassCup, 2021},
	"ferrari_488_challenge_evo":   {"Ferrari 488 Challenge Evo", "Ferrari", ClassCup, 2020},
	// TCX
	"bmw_m2_cs_racing": {"BMW M2 CS Racing", "BMW", ClassTCX, 2020},
}

// tracks maps ACC's track folder names (lower case) to readable names.
var tracksTable = map[string]string{
	"barcelona":       "Barcelona-Catalunya",
	"brands_hatch":    "Brands Hatch",
	"cota":            "Circuit of the Americas",
	"donington":       "Donington Park",
	"hungaroring":     "Hungaroring",
	"imola":           "Imola",
	"indianapolis":    "Indianapolis",
	"kyalami":         "Kyalami",
	"laguna_seca":     "Laguna Seca",
	"misano":          "Misano",
	"monza":           "Monza",
	"mount_panorama":  "Mount Panorama",
	"nurburgring":     "Nürburgring",
	"nurburgring_24h": "Nürburgring 24h",
	"oulton_park":     "Oulton Park",
	"paul_ricard":     "Paul Ricard",
	"red_bull_ring":   "Red Bull Ring",
	"silverstone":     "Silverstone",
	"snetterton":      "Snetterton",
	"spa":             "Spa-Francorchamps",
	"suzuka":          "Suzuka",
	"valencia":        "Valencia",
	"watkins_glen":    "Watkins Glen",
	"zandvoort":       "Zandvoort",
	"zolder":          "Zolder",
}

// CarInfo returns readable details for a car folder name. Unknown cars get a tidy name.
func CarInfo(id string) (name, brand, class string, year int) {
	if c, ok := cars[strings.ToLower(id)]; ok {
		return c.Name, c.Brand, c.Class, c.Year
	}
	name = Tidy(id)
	brand, _, _ = strings.Cut(name, " ")
	return name, brand, ClassOther, 0
}

// TrackName returns the readable name for a track folder name.
func TrackName(id string) string {
	if t, ok := tracksTable[strings.ToLower(id)]; ok {
		return t
	}
	return Tidy(id)
}

// Tidy turns an id like "laguna_seca" or "new_car_gt3" into "Laguna Seca" / "New Car GT3".
func Tidy(id string) string {
	words := strings.FieldsFunc(id, func(r rune) bool { return r == '_' || r == '-' || r == ' ' })
	for i, w := range words {
		hasDigit := strings.IndexFunc(w, unicode.IsDigit) >= 0
		hasLetter := strings.IndexFunc(w, unicode.IsLetter) >= 0
		switch {
		case hasDigit && hasLetter: // gt3, m4, 720s
			words[i] = strings.ToUpper(w)
		case w != "":
			words[i] = strings.ToUpper(w[:1]) + strings.ToLower(w[1:])
		}
	}
	return strings.Join(words, " ")
}
