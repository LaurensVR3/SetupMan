package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"setupman/internal/telemetry"
)

func doJSON(t *testing.T, s *Server, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, "http://localhost"+path, &buf)
	req.RemoteAddr = "127.0.0.1:12345"
	if method != http.MethodGet && method != http.MethodHead {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	return w
}

func TestTelemetryAPIEndToEnd(t *testing.T) {
	s, _ := newTest(t)

	// 1. Load Nords24 sample laps.
	w := doJSON(t, s, http.MethodPost, "/api/telemetry/sample/nords24", map[string]any{})
	if w.Code != http.StatusOK {
		t.Fatalf("sample load status %d: %s", w.Code, w.Body.String())
	}
	var sampleRes struct {
		RefID int64 `json:"refId"`
		CmpID int64 `json:"cmpId"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &sampleRes); err != nil {
		t.Fatal(err)
	}
	if sampleRes.RefID == 0 || sampleRes.CmpID == 0 {
		t.Fatalf("expected non-zero sample lap ids: %+v", sampleRes)
	}

	// 2. List stored laps.
	w = doJSON(t, s, http.MethodGet, "/api/telemetry/laps", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list laps status %d: %s", w.Code, w.Body.String())
	}
	var listRes struct {
		Laps []telemetry.LapMeta `json:"laps"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &listRes); err != nil {
		t.Fatal(err)
	}
	if len(listRes.Laps) != 2 {
		t.Fatalf("expected 2 sample laps, got %d", len(listRes.Laps))
	}
	if listRes.Laps[0].TrackName != "Nürburgring 24h" {
		t.Errorf("expected readable track name 'Nürburgring 24h', got %q", listRes.Laps[0].TrackName)
	}

	// 3. Compare the two laps.
	w = doJSON(t, s, http.MethodGet, fmt.Sprintf("/api/telemetry/compare?ref=%d&cmp=%d", sampleRes.RefID, sampleRes.CmpID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("compare status %d: %s", w.Code, w.Body.String())
	}
	var comp telemetry.Comparison
	if err := json.Unmarshal(w.Body.Bytes(), &comp); err != nil {
		t.Fatal(err)
	}
	if !comp.HasMap || len(comp.Sections) != 15 || comp.N != 1200 {
		t.Fatalf("unexpected comparison payload: hasMap=%v sections=%d n=%d", comp.HasMap, len(comp.Sections), comp.N)
	}

	// 4. Switch reference lap to CmpID.
	w = doJSON(t, s, http.MethodPut, fmt.Sprintf("/api/telemetry/laps/%d/reference", sampleRes.CmpID), map[string]any{})
	if w.Code != http.StatusOK {
		t.Fatalf("set reference status %d: %s", w.Code, w.Body.String())
	}

	// 5. Rename a lap.
	w = doJSON(t, s, http.MethodPut, fmt.Sprintf("/api/telemetry/laps/%d", sampleRes.CmpID), map[string]string{"label": "My Nords Benchmark"})
	if w.Code != http.StatusOK {
		t.Fatalf("rename status %d: %s", w.Code, w.Body.String())
	}

	// 6. Verify reference + label updated.
	w = doJSON(t, s, http.MethodGet, "/api/telemetry/laps", nil)
	_ = json.Unmarshal(w.Body.Bytes(), &listRes)
	found := false
	for _, l := range listRes.Laps {
		if l.ID == sampleRes.CmpID {
			found = true
			if !l.IsReference || l.Label != "My Nords Benchmark" {
				t.Errorf("expected updated ref/label, got isRef=%v label=%q", l.IsReference, l.Label)
			}
		}
	}
	if !found {
		t.Fatal("renamed lap not found")
	}

	// 7. Delete a lap.
	w = doJSON(t, s, http.MethodDelete, fmt.Sprintf("/api/telemetry/laps/%d", sampleRes.RefID), map[string]any{})
	if w.Code != http.StatusOK {
		t.Fatalf("delete status %d: %s", w.Code, w.Body.String())
	}
	w = doJSON(t, s, http.MethodGet, "/api/telemetry/laps", nil)
	_ = json.Unmarshal(w.Body.Bytes(), &listRes)
	if len(listRes.Laps) != 1 {
		t.Fatalf("expected 1 lap remaining after delete, got %d", len(listRes.Laps))
	}
}
