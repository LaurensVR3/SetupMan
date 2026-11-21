package acc

// carModels maps ACC's car numbers (carModel in a server's entry list, e.g. on LFM) to ACC's car
// names, as LFM's car list gives them (D-142).
var carModels = map[int]string{
	0: "porsche_991_gt3_r", 1: "mercedes_amg_gt3", 2: "ferrari_488_gt3", 3: "audi_r8_lms",
	4: "lamborghini_huracan_gt3", 5: "mclaren_650s_gt3", 6: "nissan_gt_r_gt3_2018", 7: "bmw_m6_gt3",
	8: "bentley_continental_gt3_2018", 9: "porsche_991ii_gt3_cup", 10: "nissan_gt_r_gt3_2017",
	11: "bentley_continental_gt3_2016", 12: "amr_v12_vantage_gt3", 13: "lamborghini_gallardo_rex",
	14: "jaguar_g3", 15: "lexus_rc_f_gt3", 16: "lamborghini_huracan_gt3_evo", 17: "honda_nsx_gt3",
	18: "lamborghini_huracan_st", 19: "audi_r8_lms_evo", 20: "amr_v8_vantage_gt3", 21: "honda_nsx_gt3_evo",
	22: "mclaren_720s_gt3", 23: "porsche_991ii_gt3_r", 24: "ferrari_488_gt3_evo", 25: "mercedes_amg_gt3_evo",
	26: "ferrari_488_challenge_evo", 27: "bmw_m2_cs_racing", 28: "porsche_992_gt3_cup",
	29: "lamborghini_huracan_st_evo2", 30: "bmw_m4_gt3", 31: "audi_r8_lms_evo_ii", 32: "ferrari_296_gt3",
	33: "lamborghini_huracan_gt3_evo2", 34: "porsche_992_gt3_r", 35: "mclaren_720s_gt3_evo", 36: "ford_mustang_gt3",
	50: "alpine_a110_gt4", 51: "amr_v8_vantage_gt4", 52: "audi_r8_gt4", 53: "bmw_m4_gt4",
	55: "chevrolet_camaro_gt4r", 56: "ginetta_g55_gt4", 57: "ktm_xbow_gt4", 58: "maserati_mc_gt4",
	59: "mclaren_570s_gt4", 60: "mercedes_amg_gt4", 61: "porsche_718_cayman_gt4_mr",
	80: "audi_r8_lms_gt2", 82: "ktm_xbow_gt2", 83: "maserati_mc20_gt2", 84: "mercedes_amg_gt2",
	85: "porsche_991_gt2_rs_mr", 86: "porsche_935",
}

// CarByModel returns ACC's car name for a car number ("" when unknown).
func CarByModel(n int) string { return carModels[n] }
