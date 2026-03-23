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

func TestFetchJSON_ParsesEntries(t *testing.T) {
	entries := []arenaEntry{
		{Model: "gpt-4o", ELO: 1350},
		{Model: "claude-3-5-sonnet", ELO: 1300},
		{Model: "gemini-2.0-flash", ELO: 1250},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(entries)
	}))
	defer srv.Close()

	s := &Scraper{client: srv.Client()}

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	resp, err := s.client.Do(req)
	if err != nil {
		t.Fatalf("fetch failed: %v", err)
	}
	defer resp.Body.Close()

	var got []arenaEntry
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if len(got) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(got))
	}
	if got[0].ELO != 1350 {
		t.Errorf("entry[0].ELO = %f; want 1350", got[0].ELO)
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
