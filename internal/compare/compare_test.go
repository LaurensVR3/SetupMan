package compare_test

import (
	"os"
	"strings"
	"testing"

	. "setupman/internal/compare"
	"setupman/internal/games/acc"
)

func base(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("../games/acc/testdata/base.json")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func parse(t *testing.T, s string) []acc.Field {
	t.Helper()
	p, err := acc.Parse(strings.NewReader(s))
	if err != nil {
		t.Fatal(err)
	}
	return p.Fields
}

// variant returns base with one text replacement applied (must match exactly once).
func variant(t *testing.T, old, new string) string {
	t.Helper()
	b := base(t)
	if strings.Count(b, old) != 1 {
		t.Fatalf("%q occurs %d times in base.json", old, strings.Count(b, old))
	}
	return strings.Replace(b, old, new, 1)
}

func TestIdentical(t *testing.T) {
	a := parse(t, base(t))
	b := parse(t, base(t))
	r := Compare(a, b)
	if !r.Identical || r.Score != 100 || r.Clicks != 0 || len(r.Diffs) != 0 {
		t.Errorf("identical setups: %+v", r)
	}
	if Fingerprint(a) != Fingerprint(b) {
		t.Error("fingerprints differ for identical setups")
	}
}

func TestOneClickBrakeBias(t *testing.T) {
	a := parse(t, base(t))
	b := parse(t, variant(t, `"brakeBias": 40`, `"brakeBias": 41`))
	r := Compare(a, b)
	if r.Identical || r.Clicks != 1 || r.Same != r.Total-1 {
		t.Fatalf("unexpected result: %+v", r)
	}
	if len(r.Diffs) != 1 || r.Diffs[0].Label != "Brake bias" || r.Diffs[0].Delta != "+1" {
		t.Errorf("diffs = %+v", r.Diffs)
	}
	if Fingerprint(a) == Fingerprint(b) {
		t.Error("fingerprints equal for different setups")
	}
}

func TestExcludedDifferenceKeepsIdentical(t *testing.T) {
	a := parse(t, base(t))
	b := parse(t, variant(t, `"trackBopType": 1`, `"trackBopType": 7`))
	r := Compare(a, b)
	if !r.Identical || r.Score != 100 {
		t.Errorf("excluded field changed the score: %+v", r)
	}
	if len(r.Diffs) != 1 || !r.Diffs[0].Excluded {
		t.Errorf("excluded diff should still be listed: %+v", r.Diffs)
	}
	if Fingerprint(a) != Fingerprint(b) {
		t.Error("excluded field changed the fingerprint")
	}
}

func TestPitStrategyPaddingOnlyIsIdentical(t *testing.T) {
	entry := `{ "fuelToAdd": 10, "tyres": { "tyreCompound": 0, "tyrePressure": [ 45, 45, 45, 45 ] }, "tyreSet": 2, "frontBrakePadCompound": 1, "rearBrakePadCompound": 1 }`
	padded := variant(t, `"pitStrategy": [`, `"pitStrategy": [`+strings.Repeat(entry+",", 29))
	r := Compare(parse(t, base(t)), parse(t, padded))
	if !r.Identical {
		t.Errorf("padding-only difference should be identical: %+v", r)
	}
}

// ACC fills unused slots with the main setup's tyres; those are copied slots (D-23).
func TestSlotsCopyingMainTyresAreIgnored(t *testing.T) {
	copySlot := `{ "fuelToAdd": 10, "tyres": { "tyreCompound": 0, "tyrePressure": [ 50, 50, 50, 50 ] }, "tyreSet": 2, "frontBrakePadCompound": 1, "rearBrakePadCompound": 1 }`
	padded := variant(t, `"fuelPerLap": 3.0`, `"fuelPerLap": 3.0`) // base
	padded = strings.Replace(padded, `"rearBrakePadCompound": 1
				}
			],`, `"rearBrakePadCompound": 1
				},`+strings.Repeat(copySlot+",", 28)+copySlot+`
			],`, 1)
	if !strings.Contains(padded, `[ 50, 50, 50, 50 ] }, "tyreSet": 2`) {
		t.Fatal("fixture replacement failed")
	}
	r := Compare(parse(t, base(t)), parse(t, padded))
	if !r.Identical {
		t.Errorf("slots copying the main tyres should be ignored: %+v", r.Diffs)
	}
}

func TestTyreSetIsNotScored(t *testing.T) {
	r := Compare(parse(t, base(t)), parse(t, variant(t, `"tyreSet": 1,`, `"tyreSet": 4,`)))
	if !r.Identical {
		t.Errorf("tyre set should not count: %+v", r)
	}
	r = Compare(parse(t, base(t)), parse(t, variant(t, `"tyreSet": 2,`, `"tyreSet": 7,`)))
	if !r.Identical {
		t.Errorf("pit tyre set should not count: %+v", r)
	}
}

func TestRealStrategyDifferenceCounts(t *testing.T) {
	r := Compare(parse(t, base(t)), parse(t, variant(t, `"fuelToAdd": 10`, `"fuelToAdd": 40`)))
	if r.Identical || r.Clicks != 30 {
		t.Errorf("strategy difference should count: %+v", r)
	}
	if r.Diffs[0].Label != "Pitstop strategy 1: Fuel to add" {
		t.Errorf("diff label = %q", r.Diffs[0].Label)
	}
}

func TestMissingFieldCountsAsDifferent(t *testing.T) {
	b := variant(t, `"splitter": 0,`, ``)
	r := Compare(parse(t, base(t)), parse(t, b))
	if r.Identical || r.Same != r.Total-1 {
		t.Errorf("missing field should count once: %+v", r)
	}
	if r.Diffs[0].B != "" || r.Diffs[0].A != "0" {
		t.Errorf("diff = %+v", r.Diffs[0])
	}
}

func TestFuelPerLapNotScoredButShown(t *testing.T) {
	r := Compare(parse(t, base(t)), parse(t, variant(t, `"fuelPerLap": 3.0`, `"fuelPerLap": 3.6`)))
	if r.Clicks != 0 || !r.Identical {
		t.Errorf("fuelPerLap should not count: %+v", r)
	}
	if len(r.Diffs) != 1 || !r.Diffs[0].Excluded || r.Diffs[0].Delta != "+0.6 L/lap" {
		t.Errorf("diffs = %+v", r.Diffs)
	}
}
