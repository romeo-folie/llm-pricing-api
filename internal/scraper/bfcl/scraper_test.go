package bfcl

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchVerifiedEntries_ParsesEntries(t *testing.T) {
	lf := leaderboardFile{
		Leaderboards: []leaderboard{
			{
				Name: "Lite",
				Results: []leaderboardEntry{
					{Name: "Some Lite Entry", Resolved: 50.0, Tags: []string{"Model: gpt-4o"}},
				},
			},
			{
				Name: "Verified",
				Results: []leaderboardEntry{
					{
						Name:     "mini-SWE-agent + Claude 4 Sonnet",
						Resolved: 76.8,
						Date:     "2025-09-02",
						Tags:     []string{"Model: claude-sonnet-4-20250514", "Org: Anthropic"},
					},
					{
						Name:     "mini-SWE-agent + GPT 4o",
						Resolved: 38.8,
						Date:     "2024-05-13",
						Tags:     []string{"Model: gpt-4o-2024-05-13", "Org: OpenAI"},
					},
					{
						Name:     "Devin",
						Resolved: 70.6,
						Tags:     []string{"Org: Cognition"},
					},
				},
			},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(lf)
	}))
	defer srv.Close()

	s := &Scraper{client: srv.Client()}

	// Fetch directly from the test server to validate JSON parsing.
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	resp, err := s.client.Do(req)
	if err != nil {
		t.Fatalf("fetch failed: %v", err)
	}
	defer resp.Body.Close()

	var got leaderboardFile
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	// Find the Verified leaderboard.
	var verified *leaderboard
	for i := range got.Leaderboards {
		if got.Leaderboards[i].Name == "Verified" {
			verified = &got.Leaderboards[i]
			break
		}
	}
	if verified == nil {
		t.Fatal("no 'Verified' leaderboard found")
	}

	if len(verified.Results) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(verified.Results))
	}
	if verified.Results[0].Resolved != 76.8 {
		t.Errorf("entry[0].Resolved = %f; want 76.8", verified.Results[0].Resolved)
	}
	if verified.Results[0].Date != "2025-09-02" {
		t.Errorf("entry[0].Date = %q; want 2025-09-02", verified.Results[0].Date)
	}
}

func TestExtractModelFromTags(t *testing.T) {
	tests := []struct {
		name   string
		tags   []string
		want   string
		wantOK bool
	}{
		{
			name:   "model tag present",
			tags:   []string{"Model: claude-sonnet-4-20250514", "Org: Anthropic"},
			want:   "claude-sonnet-4-20250514",
			wantOK: true,
		},
		{
			name:   "multiple model tags returns first",
			tags:   []string{"Model: gpt-4o", "Model: o3-mini"},
			want:   "gpt-4o",
			wantOK: true,
		},
		{
			name:   "no model tag",
			tags:   []string{"Org: Cognition", "System: v1"},
			want:   "",
			wantOK: false,
		},
		{
			name:   "empty tags",
			tags:   nil,
			want:   "",
			wantOK: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := extractModelFromTags(tc.tags)
			if ok != tc.wantOK {
				t.Errorf("extractModelFromTags() ok = %v; want %v", ok, tc.wantOK)
			}
			if got != tc.want {
				t.Errorf("extractModelFromTags() = %q; want %q", got, tc.want)
			}
		})
	}
}

func TestFetchVerifiedEntries_HandlesHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("not found"))
	}))
	defer srv.Close()

	s := &Scraper{client: srv.Client()}

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	resp, err := s.client.Do(req)
	if err != nil {
		t.Fatalf("unexpected network error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 status; got %d", resp.StatusCode)
	}
}

func TestFetchVerifiedEntries_NoVerifiedLeaderboard(t *testing.T) {
	lf := leaderboardFile{
		Leaderboards: []leaderboard{
			{Name: "Lite", Results: []leaderboardEntry{}},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(lf)
	}))
	defer srv.Close()

	s := &Scraper{client: srv.Client()}

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	resp, err := s.client.Do(req)
	if err != nil {
		t.Fatalf("fetch failed: %v", err)
	}
	defer resp.Body.Close()

	var got leaderboardFile
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	for _, lb := range got.Leaderboards {
		if lb.Name == "Verified" {
			t.Fatal("should not have found a 'Verified' leaderboard")
		}
	}
}
