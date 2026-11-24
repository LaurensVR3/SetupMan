package lfm

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// A real ACC race answer (trimmed; the entry list replaced by two invented entries).
func TestParseACCRace(t *testing.T) {
	body, err := os.ReadFile("testdata/acc_race.json")
	if err != nil {
		t.Fatal(err)
	}
	r, err := parseRace(100001, body, "111")
	if err != nil {
		t.Fatal(err)
	}
	if r.SimID != 1 || r.Track != "Circuit de Paul Ricard" || r.ServerTrack != "paul_ricard_2024" || r.Event != "GO Setups GT3 Sprint Series" {
		t.Errorf("race: %+v", r)
	}
	if !r.Start.Equal(time.Date(2026, 11, 20, 10, 40, 0, 0, time.UTC)) {
		t.Errorf("start %v", r.Start)
	}
	w := r.Weather
	if !w.Known || w.AmbientTemp != 15 || w.CloudLevel != 0.28 || w.Rain != 0.12 || w.Randomness != 1 {
		t.Errorf("weather: %+v", w)
	}
	if len(r.Sessions) != 3 || r.Sessions[2].Type != "R" || r.Sessions[2].Minutes != 40 || r.Sessions[2].Hour != 15 {
		t.Errorf("sessions: %+v", r.Sessions)
	}
	if r.Stops != 1 {
		t.Errorf("stops %d", r.Stops)
	}
	if !r.HasCar || r.CarModel != 32 {
		t.Errorf("driver's car: %+v", r)
	}
	if r.SignedOff() || r.Entries != 2 {
		t.Errorf("driver in the entry list counted as signed off: %+v", r)
	}
	if r2, _ := parseRace(100001, body, "999"); r2.HasCar || !r2.SignedOff() {
		t.Error("car found for a driver who is not in the entry list")
	}
	if (Race{}).SignedOff() {
		t.Error("a race without an entry list counted as signed off")
	}
}

// The next-race answer: only the race numbers are used, and the account fields are not kept.
func TestNextRaceIDsKeepsOnlyRaces(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"user":{"id":111,"email":"x@example.com","password":"hash"},"race":[{"race_id":100001,"track":"x"},{"race_id":100002}]}`)
	}))
	defer srv.Close()
	old := NextURL
	NextURL = srv.URL + "/%s"
	defer func() { NextURL = old }()
	c := New()
	ids, err := c.NextRaceIDs("111")
	if err != nil || len(ids) != 2 || ids[0] != 100001 || ids[1] != 100002 {
		t.Fatalf("ids %v %v", ids, err)
	}
	for _, e := range c.cache {
		if strings.Contains(string(e.body), "email") || strings.Contains(string(e.body), "password") {
			t.Errorf("account fields kept in the cache: %s", e.body)
		}
	}
	if _, err := c.NextRaceIDs("12a"); err == nil {
		t.Error("an LFM ID with letters was accepted")
	}
	// One race instead of a list, and no race at all.
	if got := raceIDs([]byte(`{"race_id": 5}`)); len(got) != 1 || got[0] != 5 {
		t.Errorf("single race: %v", got)
	}
	if got := raceIDs([]byte(`[]`)); len(got) != 0 {
		t.Errorf("no race: %v", got)
	}
}
