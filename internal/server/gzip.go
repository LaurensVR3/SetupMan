package server

import (
	"compress/gzip"
	"net/http"
	"strings"
)

// Large JSON answers (a track page with 100+ setups is several MB) are compressed when the
// browser accepts it, which matters when SetupMan is opened from another device (D-100).
// Only JSON is compressed: file downloads and the live event stream are passed through as is.
type gzipWriter struct {
	http.ResponseWriter
	gz      *gzip.Writer
	decided bool
}

func (g *gzipWriter) WriteHeader(status int) {
	if !g.decided {
		g.decided = true
		h := g.Header()
		if strings.HasPrefix(h.Get("Content-Type"), "application/json") && h.Get("Content-Encoding") == "" {
			h.Set("Content-Encoding", "gzip")
			h.Add("Vary", "Accept-Encoding")
			h.Del("Content-Length")
			g.gz = gzip.NewWriter(g.ResponseWriter)
		}
	}
	g.ResponseWriter.WriteHeader(status)
}

func (g *gzipWriter) Write(b []byte) (int, error) {
	if !g.decided {
		g.WriteHeader(http.StatusOK)
	}
	if g.gz != nil {
		return g.gz.Write(b)
	}
	return g.ResponseWriter.Write(b)
}

// Flush keeps streaming answers working (the event stream is never compressed).
func (g *gzipWriter) Flush() {
	if g.gz != nil {
		g.gz.Flush()
	}
	if f, ok := g.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (g *gzipWriter) close() {
	if g.gz != nil {
		g.gz.Close()
	}
}

// withGzip serves r, compressing a JSON answer when the browser accepts gzip.
func withGzip(w http.ResponseWriter, r *http.Request, next http.Handler) {
	if r.URL.Path == "/api/events" || !strings.HasPrefix(r.URL.Path, "/api/") ||
		!strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		next.ServeHTTP(w, r)
		return
	}
	g := &gzipWriter{ResponseWriter: w}
	defer g.close()
	next.ServeHTTP(g, r)
}
