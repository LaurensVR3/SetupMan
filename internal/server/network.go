package server

// Access from other computers on the local network (spec D-100, D-109). Off by default. When
// on, other computers can load the page, but every API call needs the PIN; a browser that
// entered it gets a cookie and then has full use of SetupMan, Settings included. The user can
// switch the PIN off for their own network (D-139); the address check (DNS rebinding) and the
// JSON-only rule for changes stay in force either way.

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const pinCookie = "setupman_pin"

// isLoopback reports whether a request comes from this PC.
func isLoopback(remoteAddr string) bool {
	h, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		h = remoteAddr
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

// checkRemote decides about a request from another computer. It returns false after answering.
func (s *Server) checkRemote(w http.ResponseWriter, r *http.Request, host string) bool {
	nw := s.lib.Settings().Network
	if !nw.Allow {
		http.Error(w, "SetupMan only accepts connections from its own PC", http.StatusForbidden)
		return false
	}
	// Only addresses and the network name the user set (D-129): a web page can't point a
	// made-up domain at this PC (DNS rebinding).
	if net.ParseIP(host) == nil && (nw.HostName == "" || !strings.EqualFold(host, nw.HostName)) {
		http.Error(w, "use the IP address to open SetupMan from another computer", http.StatusForbidden)
		return false
	}
	p := r.URL.Path
	if !strings.HasPrefix(p, "/api/") || p == "/api/ping" || p == "/api/pin" {
		return true // the page itself holds no data; it asks for the PIN
	}
	if nw.NoPIN {
		return true // the user switched the PIN off for their own network (D-139)
	}
	if c, err := r.Cookie(pinCookie); err == nil && nw.Token != "" &&
		subtle.ConstantTimeCompare([]byte(c.Value), []byte(nw.Token)) == 1 {
		return true
	}
	writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "Enter the PIN shown in SetupMan's Settings on the PC it runs on.", "needPin": true})
	return false
}

// PIN tries per address: a few, then a pause. A try is counted before the PIN is checked, so
// many guesses sent at the same moment can't get past the limit (D-138).
type pinGuard struct {
	mu    sync.Mutex
	fails map[string][]time.Time
}

var guard = &pinGuard{fails: map[string][]time.Time{}}

const (
	pinTries = 5
	pinPause = 5 * time.Minute
)

// try records a try for ip and reports whether it may go ahead.
func (g *pinGuard) try(ip string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	recent := g.fails[ip][:0]
	for _, t := range g.fails[ip] {
		if time.Since(t) < pinPause {
			recent = append(recent, t)
		}
	}
	if len(recent) >= pinTries {
		g.fails[ip] = recent
		return false
	}
	g.fails[ip] = append(recent, time.Now())
	return true
}

// ok forgets the tries of ip after a correct PIN.
func (g *pinGuard) ok(ip string) {
	g.mu.Lock()
	delete(g.fails, ip)
	g.mu.Unlock()
}

func (s *Server) registerNetwork() {
	s.mux.HandleFunc("POST /api/pin", s.handlePin)
	s.mux.HandleFunc("GET /api/network", s.handleNetwork)
	s.mux.HandleFunc("POST /api/network", s.handleSetNetwork)
}

// handlePin checks a PIN typed on another computer and remembers it in that browser.
func (s *Server) handlePin(w http.ResponseWriter, r *http.Request) {
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	if !guard.try(ip) {
		writeError(w, http.StatusTooManyRequests, "too many incorrect PINs; try again in 5 minutes")
		return
	}
	var in struct {
		PIN string `json:"pin"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	nw := s.lib.Settings().Network
	if !nw.Allow || nw.PIN == "" || subtle.ConstantTimeCompare([]byte(strings.TrimSpace(in.PIN)), []byte(nw.PIN)) != 1 {
		time.Sleep(time.Second)
		writeError(w, http.StatusUnauthorized, "incorrect PIN")
		return
	}
	guard.ok(ip)
	setPinCookie(w, nw.Token)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// setPinCookie remembers in this browser that it may use SetupMan (for a year).
func setPinCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{Name: pinCookie, Value: token, Path: "/", MaxAge: 365 * 24 * 3600, HttpOnly: true, SameSite: http.SameSiteStrictMode})
}

// handleNetwork tells the Settings page the state, including the PIN (callers from other
// computers already entered it).
func (s *Server) handleNetwork(w http.ResponseWriter, r *http.Request) {
	nw := s.lib.Settings().Network
	// Through the network name the request comes from a proxy, so it is not "this PC" (D-129).
	host, _, err := net.SplitHostPort(r.Host)
	if err != nil {
		host = r.Host
	}
	local := isLoopback(r.RemoteAddr) && !(nw.HostName != "" && strings.EqualFold(host, nw.HostName))
	writeJSON(w, http.StatusOK, map[string]any{"allow": nw.Allow, "local": local, "pin": nw.PIN, "requirePin": !nw.NoPIN,
		"addresses": lanAddresses(), "port": s.Port, "listening": s.Listening, "hostName": nw.HostName})
}

// cleanHostName checks a network name typed in Settings (D-129). It accepts a pasted address
// such as "http://setupman.lan/" and returns just the name, in lower case.
func cleanHostName(in string) (string, error) {
	h := strings.ToLower(strings.TrimSpace(in))
	h = strings.TrimPrefix(strings.TrimPrefix(h, "http://"), "https://")
	h = strings.TrimSuffix(h, "/")
	if h == "" {
		return "", nil
	}
	if len(h) > 253 || net.ParseIP(h) != nil || h == "localhost" || strings.HasSuffix(h, ".localhost") {
		return "", fmt.Errorf("enter a name such as setupman.lan (not an IP address or localhost)")
	}
	for _, label := range strings.Split(h, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", fmt.Errorf("%q is not a valid network name; use letters, digits, hyphens and dots, such as setupman.lan", h)
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return "", fmt.Errorf("%q is not a valid network name; use letters, digits, hyphens and dots, such as setupman.lan", h)
			}
		}
	}
	return h, nil
}

// handleSetNetwork switches network access on or off, makes a new PIN or changes the port.
// SetupMan restarts so it listens on the right addresses.
func (s *Server) handleSetNetwork(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Allow    bool    `json:"allow"`
		NewPIN   bool    `json:"newPin"`
		Port     int     `json:"port"`     // 0 = keep
		HostName *string `json:"hostName"` // nil = keep, "" = remove (D-129)
		// RequirePIN: nil = keep. false lets devices on the network in without the PIN (D-139).
		RequirePIN *bool `json:"requirePin"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	st := s.store.Load()
	if in.HostName != nil {
		h, err := cleanHostName(*in.HostName)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		st.Network.HostName = h // no restart needed: it is checked per request
	}
	restart := st.Network.Allow != in.Allow
	st.Network.Allow = in.Allow
	if in.Port != 0 {
		if in.Port < 1024 || in.Port > 65535 {
			writeError(w, http.StatusBadRequest, "choose a port number between 1024 and 65535")
			return
		}
		if in.Port != s.Port {
			restart = true
		}
		st.Network.Port = in.Port
	}
	pinOn := false
	if in.RequirePIN != nil { // no restart needed: it is checked per request
		pinOn = *in.RequirePIN && st.Network.NoPIN
		st.Network.NoPIN = !*in.RequirePIN
	}
	if in.Allow && (in.NewPIN || st.Network.PIN == "" || st.Network.Token == "") {
		st.Network.PIN, st.Network.Token = newPIN(), newToken()
	}
	if err := s.store.Save(st); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.lib.Apply()
	if pinOn || (in.NewPIN && !st.Network.NoPIN) {
		// The device that switched the PIN on (or made a new one) keeps access: it was allowed to
		// make this change, so it gets the PIN cookie straight away instead of being locked out.
		setPinCookie(w, st.Network.Token)
	}
	writeJSON(w, http.StatusOK, map[string]any{"pin": st.Network.PIN, "port": st.Network.Port, "hostName": st.Network.HostName,
		"requirePin": !st.Network.NoPIN, "restarting": restart && s.Restart != nil})
	if restart && s.Restart != nil {
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		go s.Restart()
	}
}

func newPIN() string {
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		panic(err)
	}
	return fmt.Sprintf("%06d", n.Int64())
}

func newToken() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// lanAddresses are this PC's IPv4 addresses on the local network (virtual adapters of WSL,
// Hyper-V, VMware or VirtualBox left out: other computers can't reach those).
func lanAddresses() []string {
	out := []string{}
	ifs, err := net.Interfaces()
	if err != nil {
		return out
	}
	for _, ifc := range ifs {
		name := strings.ToLower(ifc.Name)
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 || strings.Contains(name, "vethernet") ||
			strings.Contains(name, "virtual") || strings.Contains(name, "vmware") || strings.Contains(name, "wsl") {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil && n.IP.IsPrivate() {
				out = append(out, n.IP.String())
			}
		}
	}
	return out
}
