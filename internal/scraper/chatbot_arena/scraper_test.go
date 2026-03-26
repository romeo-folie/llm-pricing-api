package chatbot_arena

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNormalizeELO(t *testing.T) {
	tests := []struct {
		name   string
		elo    float64
		min    float64
		max    float64
		expect float64
	}{
		{"max score", 1400, 1000, 1400, 100.0},
		{"min score", 1000, 1000, 1400, 0.0},
		{"mid score", 1200, 1000, 1400, 50.0},
		{"equal min max", 1200, 1200, 1200, 50.0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := NormalizeELO(tc.elo, tc.min, tc.max)
			if math.Abs(got-tc.expect) > 0.01 {
				t.Errorf("NormalizeELO(%f, %f, %f) = %f; want %f", tc.elo, tc.min, tc.max, got, tc.expect)
			}
		})
	}
}

// TestFetchPage_ParsesHFResponse verifies that fetchPage correctly parses the
// HuggingFace Datasets Server envelope format.
func TestFetchPage_ParsesHFResponse(t *testing.T) {
	// The HF Datasets Server wraps rows in {"rows": [{"row": {...}}, ...]}
	hfPayload := hfResponse{
		Rows: []hfRow{
			{Row: struct {
				Model      string `json:"Model"`
				ArenaScore int    `json:"Arena Score"`
			}{Model: "Gemini-2.5-Pro", ArenaScore: 1474}},
			{Row: struct {
				Model      string `json:"Model"`
				ArenaScore int    `json:"Arena Score"`
			}{Model: "Claude Opus 4 (20250514)", ArenaScore: 1368}},
			{Row: struct {
				Model      string `json:"Model"`
				ArenaScore int    `json:"Arena Score"`
			}{Model: "GPT-4o-2024-05-13", ArenaScore: 1300}},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(hfPayload)
	}))
	defer srv.Close()

	s := &Scraper{client: srv.Client()}

	rows, err := s.fetchPage(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("fetchPage failed: %v", err)
	}

	if len(rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(rows))
	}
	if rows[0].Row.Model != "Gemini-2.5-Pro" {
		t.Errorf("rows[0].Row.Model = %q; want Gemini-2.5-Pro", rows[0].Row.Model)
	}
	if rows[0].Row.ArenaScore != 1474 {
		t.Errorf("rows[0].Row.ArenaScore = %d; want 1474", rows[0].Row.ArenaScore)
	}
}

func TestMinMax(t *testing.T) {
	min, max := minMax([]float64{1200, 1000, 1400, 1100})
	if min != 1000 {
		t.Errorf("min = %f; want 1000", min)
	}
	if max != 1400 {
		t.Errorf("max = %f; want 1400", max)
	}
}

func TestMinMax_Empty(t *testing.T) {
	min, max := minMax(nil)
	if min != 0 || max != 0 {
		t.Errorf("minMax(nil) = (%f, %f); want (0, 0)", min, max)
	}
}

func TestNormalizeELO_RoundTrip(t *testing.T) {
	// 3/4 of the range → 75.0
	got := NormalizeELO(1300, 1000, 1400)
	if math.Abs(got-75.0) > 0.01 {
		t.Errorf("NormalizeELO(1300, 1000, 1400) = %f; want 75.0", got)
	}
}
