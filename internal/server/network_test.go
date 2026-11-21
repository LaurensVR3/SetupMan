package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// Requests from another computer: refused while network access is off; with it on, the page
// loads but data needs the PIN; a wrong PIN is refused; the right one gives a cookie that works.
func TestNetworkAccess(t *testing.T) {
	s, _ := newTest(t)
	st := s.store
	remote := func(method, path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
		var r *http.Request
		if body != "" {
			r = httptest.NewRequest(method, "http://192.168.1.5:8765"+path, strings.NewReader(body))
			r.Header.Set("Content-Type", "application/json")
		} else {
			r = httptest.NewRequest(method, "http://192.168.1.5:8765"+path, nil)
		}
		r.RemoteAddr = "192.168.1.20:50000"
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	if w := remote("GET", "/api/games", "", nil); w.Code != http.StatusForbidden {
		t.Fatalf("network off: %d", w.Code)
	}
	cfg := st.Load()
	cfg.Network.Allow, cfg.Network.PIN, cfg.Network.Token = true, "123456", "tok"
	st.Save(cfg)
	s.lib.Apply()

	if w := remote("GET", "/", "", nil); w.Code != http.StatusOK {
		t.Errorf("page without PIN: %d", w.Code)
	}
	if w := remote("GET", "/api/games", "", nil); w.Code != http.StatusUnauthorized {
		t.Errorf("data without PIN: %d", w.Code)
	}
	if w := remote("POST", "/api/pin", `{"pin":"000000"}`, nil); w.Code != http.StatusUnauthorized {
		t.Errorf("wrong PIN: %d", w.Code)
	}
	w := remote("POST", "/api/pin", `{"pin":"123456"}`, nil)
	if w.Code != http.StatusOK || len(w.Result().Cookies()) != 1 {
		t.Fatalf("right PIN: %d", w.Code)
	}
	c := w.Result().Cookies()[0]
	if w := remote("GET", "/api/games", "", c); w.Code != http.StatusOK {
		t.Errorf("with cookie: %d", w.Code)
	}
	// With the PIN, another computer has full use, network settings included (D-109).
	if w := remote("GET", "/api/network", "", c); !strings.Contains(w.Body.String(), "123456") {
		t.Errorf("network settings from another computer: %s", w.Body.String())
	}
	if w := remote("GET", "/api/settings", "", c); strings.Contains(w.Body.String(), "123456") {
		t.Error("settings show the PIN to another computer")
	}
	// A made-up name pointing at this PC is refused (DNS rebinding).
	r := httptest.NewRequest("GET", "http://evil.example:8765/api/games", nil)
	r.RemoteAddr = "192.168.1.20:50000"
	r.AddCookie(c)
	ww := httptest.NewRecorder()
	s.ServeHTTP(ww, r)
	if ww.Code != http.StatusForbidden {
		t.Errorf("host name accepted: %d", ww.Code)
	}
}

// A network name set in Settings (D-129) is accepted from other devices and through a proxy on
// this PC, but always needs the PIN; other names stay refused.
func TestNetworkHostName(t *testing.T) {
	s, _ := newTest(t)
	cfg := s.store.Load()
	cfg.Network.Allow, cfg.Network.PIN, cfg.Network.Token, cfg.Network.HostName = true, "123456", "tok", "setupman.lan"
	s.store.Save(cfg)
	s.lib.Apply()
	get := func(url, from string, c *http.Cookie) int {
		r := httptest.NewRequest("GET", url, nil)
		r.RemoteAddr = from
		if c != nil {
			r.AddCookie(c)
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w.Code
	}
	tok := &http.Cookie{Name: pinCookie, Value: "tok"}
	if c := get("http://SetupMan.lan/api/games", "192.168.1.20:5000", tok); c != http.StatusOK {
		t.Errorf("name from another device with PIN: %d", c)
	}
	if c := get("http://setupman.lan/api/games", "127.0.0.1:5000", nil); c != http.StatusUnauthorized {
		t.Errorf("name through a proxy on this PC without PIN: %d", c)
	}
	if c := get("http://setupman.lan/", "127.0.0.1:5000", nil); c != http.StatusOK {
		t.Errorf("page through the name: %d", c)
	}
	if c := get("http://other.lan/api/games", "192.168.1.20:5000", tok); c != http.StatusForbidden {
		t.Errorf("other name: %d", c)
	}
	if c := get("http://setupman.localhost/api/games", "127.0.0.1:5000", nil); c != http.StatusOK {
		t.Errorf("this PC without PIN: %d", c)
	}
}

func TestCleanHostName(t *testing.T) {
	good := map[string]string{"setupman.lan": "setupman.lan", " http://SetupMan.LAN/ ": "setupman.lan", "": "", "rig-1.home": "rig-1.home"}
	for in, want := range good {
		if got, err := cleanHostName(in); err != nil || got != want {
			t.Errorf("%q: %q %v", in, got, err)
		}
	}
	for _, in := range []string{"192.168.1.5", "localhost", "setupman.localhost", "set up.lan", "-x.lan", "a..lan", "setupman.lan:8765"} {
		if _, err := cleanHostName(in); err == nil {
			t.Errorf("%q accepted", in)
		}
	}
}

// Many wrong PINs sent at the same moment: only the allowed number is checked, the rest wait (D-138).
func TestPinTriesAtOnce(t *testing.T) {
	s, _ := newTest(t)
	cfg := s.store.Load()
	cfg.Network.Allow, cfg.Network.PIN, cfg.Network.Token = true, "123456", "tok"
	s.store.Save(cfg)
	s.lib.Apply()
	guard.ok("192.168.1.77")
	var mu sync.Mutex
	codes := map[int]int{}
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := httptest.NewRequest("POST", "http://192.168.1.5:8765/api/pin", strings.NewReader(`{"pin":"000000"}`))
			r.Header.Set("Content-Type", "application/json")
			r.RemoteAddr = "192.168.1.77:50000"
			w := httptest.NewRecorder()
			s.ServeHTTP(w, r)
			mu.Lock()
			codes[w.Code]++
			mu.Unlock()
		}()
	}
	wg.Wait()
	if codes[http.StatusUnauthorized] != pinTries || codes[http.StatusTooManyRequests] != 40-pinTries {
		t.Errorf("checked %d, paused %d (want %d / %d)", codes[http.StatusUnauthorized], codes[http.StatusTooManyRequests], pinTries, 40-pinTries)
	}
	guard.ok("192.168.1.77")
}

// With the PIN switched off (D-139), devices on the network get in without it; network access
// itself and the address check still apply. Switching the PIN back on from such a device keeps
// that device in (it gets the cookie) and locks the others out until they enter the PIN.
func TestNetworkWithoutPIN(t *testing.T) {
	s, _ := newTest(t)
	cfg := s.store.Load()
	cfg.Network.Allow, cfg.Network.PIN, cfg.Network.Token, cfg.Network.NoPIN = true, "123456", "tok", true
	s.store.Save(cfg)
	s.lib.Apply()
	do := func(method, url, body string, c *http.Cookie) *httptest.ResponseRecorder {
		var r *http.Request
		if body != "" {
			r = httptest.NewRequest(method, url, strings.NewReader(body))
			r.Header.Set("Content-Type", "application/json")
		} else {
			r = httptest.NewRequest(method, url, nil)
		}
		r.RemoteAddr = "192.168.1.30:5000"
		if c != nil {
			r.AddCookie(c)
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	if w := do("GET", "http://192.168.1.5:8765/api/games", "", nil); w.Code != http.StatusOK {
		t.Fatalf("no PIN needed: %d", w.Code)
	}
	if w := do("GET", "http://evil.example:8765/api/games", "", nil); w.Code != http.StatusForbidden {
		t.Errorf("made-up name accepted without PIN: %d", w.Code)
	}
	if w := do("POST", "http://192.168.1.5:8765/api/games/acc/rescan", "", nil); w.Code != http.StatusUnsupportedMediaType {
		t.Errorf("non-JSON change accepted: %d", w.Code)
	}
	if w := do("GET", "http://192.168.1.5:8765/api/network", "", nil); !strings.Contains(w.Body.String(), `"requirePin":false`) {
		t.Errorf("network state: %s", w.Body.String())
	}

	// Switch the PIN back on from this device.
	w := do("POST", "http://192.168.1.5:8765/api/network", `{"allow":true,"requirePin":true}`, nil)
	if w.Code != http.StatusOK || len(w.Result().Cookies()) != 1 {
		t.Fatalf("PIN on: %d, cookies %d", w.Code, len(w.Result().Cookies()))
	}
	c := w.Result().Cookies()[0]
	if s.store.Load().Network.NoPIN {
		t.Error("PIN not switched on in the settings")
	}
	if w := do("GET", "http://192.168.1.5:8765/api/games", "", c); w.Code != http.StatusOK {
		t.Errorf("the device that switched the PIN on lost access: %d", w.Code)
	}
	if w := do("GET", "http://192.168.1.5:8765/api/games", "", nil); w.Code != http.StatusUnauthorized {
		t.Errorf("other devices need the PIN again: %d", w.Code)
	}

	// Network access off: refused, PIN or not.
	cfg = s.store.Load()
	cfg.Network.Allow, cfg.Network.NoPIN = false, true
	s.store.Save(cfg)
	s.lib.Apply()
	if w := do("GET", "http://192.168.1.5:8765/api/games", "", nil); w.Code != http.StatusForbidden {
		t.Errorf("network access off: %d", w.Code)
	}
}

// No other web site may show SetupMan inside its own page (clickjacking, D-140).
func TestNoFraming(t *testing.T) {
	s, _ := newTest(t)
	for _, path := range []string{"/", "/api/games"} {
		r := httptest.NewRequest("GET", "http://127.0.0.1:8765"+path, nil)
		r.RemoteAddr = "127.0.0.1:5000"
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Header().Get("X-Frame-Options") != "DENY" || !strings.Contains(w.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
			t.Errorf("%s may be framed: %v", path, w.Header())
		}
	}
}
