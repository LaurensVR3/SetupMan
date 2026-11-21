// Package lfm reads public race information from Low Fuel Motorsport (spec D-142): a driver's next
// race, and per race its track, sessions, mandatory stops and weather settings as LFM sets them.
// Only the race details and the race numbers of the next race are used; nothing is sent to LFM but
// the driver's LFM ID and race numbers. Answers are cached for a few minutes; LFM is only asked when
// a page needs it (no polling).
package lfm

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Base addresses (unofficial but public; see the spec, D-142). Tests point them at a local server.
var (
	RaceURL = "https://api3.lowfuelmotorsport.com/api/race/%d"
	NextURL = "https://api.lowfuelmotorsport.de/api/licenseWidgetUserData/%s"
)

// Session is one session of a race weekend.
type Session struct {
	Type       string  `json:"type"`       // "P", "Q", "R"
	Day        int     `json:"day"`        // day of the weekend (ACC: 1 Friday … 3 Sunday)
	Hour       int     `json:"hour"`       // time of day in the game
	Minutes    int     `json:"minutes"`    // length
	Multiplier float64 `json:"multiplier"` // time speed-up
}

// Weather is the weather as the race server is set (ACC's names; empty for other games).
type Weather struct {
	Known       bool    `json:"known"`
	AmbientTemp float64 `json:"ambientTemp"` // °C
	CloudLevel  float64 `json:"cloudLevel"`  // 0 … 1
	Rain        float64 `json:"rain"`        // 0 … 1
	Randomness  int     `json:"randomness"`  // 0 … 7
	// Other games give their weather as named fields (e.g. "RaceWeatherSlot1": "LightCloud").
	Other map[string]string `json:"other,omitempty"`
}

// Race is what Race prep shows of one LFM race.
type Race struct {
	ID          int64     `json:"id"`
	Event       string    `json:"event"`
	SimID       int       `json:"simId"` // 1 = ACC
	Track       string    `json:"track"`
	ServerTrack string    `json:"serverTrack"` // the game's track name, e.g. "paul_ricard_2024"
	Start       time.Time `json:"start"`       // UTC
	Sessions    []Session `json:"sessions"`
	Weather     Weather   `json:"weather"`
	Stops       int       `json:"stops"` // mandatory pit stops (-1 = not known)
	CarModel    int       `json:"carModel"`
	CarClass    string    `json:"carClass,omitempty"`
	HasCar      bool      `json:"hasCar"` // the driver is in the entry list: CarModel is their car
	Entries     int       `json:"entries"` // number of cars in the entry list
}

// SignedOff reports whether the race has an entry list without the driver in it (they signed off,
// or never signed up). Only meaningful when Race was asked with the driver's LFM ID.
func (r Race) SignedOff() bool { return r.Entries > 0 && !r.HasCar }

// Client fetches from LFM with a short cache.
type Client struct {
	HTTP  *http.Client
	mu    sync.Mutex
	cache map[string]cached
}

type cached struct {
	at   time.Time
	body []byte
}

const cacheFor = 3 * time.Minute

// New makes a client.
func New() *Client {
	return &Client{HTTP: &http.Client{Timeout: 10 * time.Second}, cache: map[string]cached{}}
}

func (c *Client) get(url string, fresh bool) ([]byte, error) {
	c.mu.Lock()
	if e, ok := c.cache[url]; ok && !fresh && time.Since(e.at) < cacheFor {
		c.mu.Unlock()
		return e.body, nil
	}
	c.mu.Unlock()
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "SetupMan") // a generic name: nothing about the user (D-135)
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, errors.New("LFM can't be reached right now")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("LFM answered %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.cache[url] = cached{time.Now(), body}
	c.mu.Unlock()
	return body, nil
}

var reID = regexp.MustCompile(`^[0-9]{1,12}$`)

// ValidID reports whether s looks like an LFM ID (digits only).
func ValidID(s string) bool { return reID.MatchString(s) }

// NextRaceIDs returns the race numbers of the driver's next race(s). Only the race numbers are read
// from LFM's answer; the rest of it (which includes account fields) is dropped unread (D-142).
// It always asks LFM (no cache), so signing up or off on LFM shows at once (D-143).
func (c *Client) NextRaceIDs(lfmID string) ([]int64, error) {
	if !ValidID(lfmID) {
		return nil, errors.New("the LFM ID must be a number")
	}
	body, err := c.get(fmt.Sprintf(NextURL, lfmID), true)
	if err != nil {
		return nil, err
	}
	// Keep the cache from holding the account fields: replace the entry with the race part only.
	var only struct {
		Race json.RawMessage `json:"race"`
	}
	if err := json.Unmarshal(body, &only); err != nil {
		return nil, errors.New("LFM's answer could not be read")
	}
	c.mu.Lock()
	c.cache[fmt.Sprintf(NextURL, lfmID)] = cached{time.Now(), mustJSON(map[string]json.RawMessage{"race": only.Race})}
	c.mu.Unlock()
	return raceIDs(only.Race), nil
}

func mustJSON(v any) []byte { b, _ := json.Marshal(v); return b }

// raceIDs finds race numbers in the "race" part, which may be one race or a list of races.
func raceIDs(raw json.RawMessage) []int64 {
	var list []map[string]any
	if json.Unmarshal(raw, &list) != nil {
		var one map[string]any
		if json.Unmarshal(raw, &one) != nil || one == nil {
			return nil
		}
		list = []map[string]any{one}
	}
	var out []int64
	for _, r := range list {
		for _, k := range []string{"race_id", "raceId", "id"} {
			if id := toInt(r[k]); id > 0 {
				out = append(out, id)
				break
			}
		}
	}
	return out
}

// Race returns one race's details; lfmID (may be "") finds the driver's car in the entry list.
// fresh skips the cache (used for the driver's next races, whose entry list may just have changed).
func (c *Client) Race(id int64, lfmID string, fresh bool) (Race, error) {
	if id <= 0 {
		return Race{}, errors.New("enter an LFM race number")
	}
	body, err := c.get(fmt.Sprintf(RaceURL, id), fresh)
	if err != nil {
		return Race{}, err
	}
	return parseRace(id, body, lfmID)
}

func parseRace(id int64, body []byte, lfmID string) (Race, error) {
	var d struct {
		RaceID   json.Number `json:"race_id"`
		RaceDate string      `json:"race_date"`
		SimID    any         `json:"sim_id"`
		Track    struct {
			Name    string `json:"track_name"`
			ACCName string `json:"acc_track_name"`
			SimID   any    `json:"sim_id"`
		} `json:"track"`
		Event struct {
			ID    any    `json:"event_id"`
			Name  string `json:"event_name"`
			SimID any    `json:"sim_id"`
		} `json:"event"`
		ServerSettings json.RawMessage `json:"server_settings"`
		Participants   struct {
			Entries []struct {
				UserID   any    `json:"user_id"`
				CarModel any    `json:"car_model"`
				CarClass string `json:"car_class"`
			} `json:"entries"`
		} `json:"participants"`
	}
	if err := json.Unmarshal(body, &d); err != nil {
		return Race{}, errors.New("LFM has no race with this number")
	}
	r := Race{ID: id, Event: d.Event.Name, Track: d.Track.Name, ServerTrack: d.Track.ACCName, Stops: -1}
	r.SimID = int(toInt(d.Event.SimID))
	if r.SimID == 0 {
		r.SimID = int(toInt(d.Track.SimID))
	}
	if t, err := time.ParseInLocation("2006-01-02 15:04:05", d.RaceDate, time.UTC); err == nil {
		r.Start = t
	}
	if d.RaceDate == "" && d.Track.Name == "" {
		return Race{}, errors.New("LFM has no race with this number")
	}
	readSettings(&r, d.ServerSettings)
	r.Entries = len(d.Participants.Entries)
	for _, e := range d.Participants.Entries {
		if lfmID != "" && fmt.Sprint(toInt(e.UserID)) == lfmID {
			r.CarModel, r.CarClass, r.HasCar = int(toInt(e.CarModel)), e.CarClass, true
		}
	}
	return r, nil
}

// readSettings takes the sessions, weather and mandatory stops from the server settings. ACC keeps
// them in event.json / eventRules.json (nested under server_settings once or twice); other games
// keep weather as named fields in "default".
func readSettings(r *Race, raw json.RawMessage) {
	var top map[string]json.RawMessage
	if json.Unmarshal(raw, &top) != nil {
		return
	}
	if inner, ok := top["server_settings"]; ok {
		var m map[string]json.RawMessage
		if json.Unmarshal(inner, &m) == nil && m["event"] != nil {
			top = m
		}
	}
	var ev struct {
		Data struct {
			Track       string   `json:"track"`
			AmbientTemp *float64 `json:"ambientTemp"`
			CloudLevel  float64  `json:"cloudLevel"`
			Rain        float64  `json:"rain"`
			Randomness  int      `json:"weatherRandomness"`
			Sessions    []struct {
				Hour       int     `json:"hourOfDay"`
				Day        int     `json:"dayOfWeekend"`
				Multiplier float64 `json:"timeMultiplier"`
				Type       string  `json:"sessionType"`
				Minutes    int     `json:"sessionDurationMinutes"`
			} `json:"sessions"`
		} `json:"data"`
	}
	if json.Unmarshal(top["event"], &ev) == nil && ev.Data.AmbientTemp != nil {
		r.Weather = Weather{Known: true, AmbientTemp: *ev.Data.AmbientTemp, CloudLevel: ev.Data.CloudLevel, Rain: ev.Data.Rain, Randomness: ev.Data.Randomness}
		if ev.Data.Track != "" {
			r.ServerTrack = ev.Data.Track
		}
		for _, s := range ev.Data.Sessions {
			r.Sessions = append(r.Sessions, Session{Type: s.Type, Day: s.Day, Hour: s.Hour, Minutes: s.Minutes, Multiplier: s.Multiplier})
		}
	}
	var rules struct {
		Data struct {
			Stops *int `json:"mandatoryPitstopCount"`
		} `json:"data"`
	}
	if json.Unmarshal(top["eventRules"], &rules) == nil && rules.Data.Stops != nil {
		r.Stops = *rules.Data.Stops
	}
	// Other games: weather and session lengths as named fields.
	var def map[string]any
	if json.Unmarshal(top["default"], &def) == nil && len(def) > 0 && !r.Weather.Known {
		r.Weather.Other = map[string]string{}
		for k, v := range def {
			lk := strings.ToLower(k)
			if strings.Contains(lk, "weather") || strings.Contains(lk, "datehour") || strings.HasSuffix(lk, "length") {
				r.Weather.Other[k] = fmt.Sprint(v)
			}
		}
		for _, s := range []struct{ key, typ string }{{"PracticeLength", "P"}, {"QualifyLength", "Q"}, {"RaceLength", "R"}} {
			if m := toInt(def[s.key]); m > 0 {
				r.Sessions = append(r.Sessions, Session{Type: s.typ, Minutes: int(m), Hour: int(toInt(def[strings.TrimSuffix(s.key, "Length")+"DateHour"]))})
			}
		}
		if n := toInt(def["RaceMandatoryPitStops"]); n >= 0 && def["RaceMandatoryPitStops"] != nil {
			r.Stops = int(n)
		}
	}
}

func toInt(v any) int64 {
	switch x := v.(type) {
	case float64:
		return int64(x)
	case json.Number:
		n, _ := x.Int64()
		return n
	case string:
		n, _ := strconv.ParseInt(strings.TrimSpace(x), 10, 64)
		return n
	}
	return 0
}
