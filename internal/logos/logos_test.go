package logos

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

func TestGetDownloadsOnceAndCaches(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Header.Get("User-Agent") != UserAgent {
			t.Errorf("user agent = %q", r.Header.Get("User-Agent"))
		}
		if !strings.HasSuffix(r.URL.Path, "/BMW.svg") {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`))
	}))
	defer srv.Close()

	s := New(t.TempDir())
	s.baseURL = srv.URL + "/"

	if _, _, err := s.Get("BMW", false); !errors.Is(err, ErrNoLogo) {
		t.Errorf("no download allowed: err = %v", err)
	}
	path, ct, err := s.Get("BMW", true)
	if err != nil || ct != "image/svg+xml" {
		t.Fatalf("get: %v %q", err, ct)
	}
	if b, _ := os.ReadFile(path); !strings.Contains(string(b), "<svg") {
		t.Error("cached file wrong")
	}
	s.Get("BMW", true)
	if hits.Load() != 1 {
		t.Errorf("downloaded %d times, want 1", hits.Load())
	}

	// Failures are remembered: a second try does not hit the server again.
	if _, _, err := s.Get("Audi", true); !errors.Is(err, ErrNoLogo) {
		t.Errorf("404 should give ErrNoLogo: %v", err)
	}
	before := hits.Load()
	s.Get("Audi", true)
	if hits.Load() != before {
		t.Error("failed brand fetched again")
	}

	if _, _, err := s.Get("Ginetta", true); !errors.Is(err, ErrNoLogo) {
		t.Error("brand without a source should give ErrNoLogo")
	}
}
