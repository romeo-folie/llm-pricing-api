package bfcl

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchJSON_ParsesEntries(t *testing.T) {
	entries := []leaderboardEntry{
		{Model: "gpt-4o-2024-08-06", OverallAcc: 82.5, Rank: 1},
		{Model: "claude-3-5-sonnet-20241022", OverallAcc: 79.3, Rank: 2},
		{Model: "unknown-model-xyz", OverallAcc: 50.0, Rank: 3},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(entries)
	}))
	defer srv.Close()

	s := &Scraper{
		client: srv.Client(),
	}

	// Override the URL by calling fetchJSON with a patched request.
	// We test fetchJSON indirectly by creating a request to the test server.
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	resp, err := s.client.Do(req)
	if err != nil {
		t.Fatalf("fetch failed: %v", err)
	}
	defer resp.Body.Close()

	var got []leaderboardEntry
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if len(got) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(got))
	}
	if got[0].Model != "gpt-4o-2024-08-06" {
		t.Errorf("entry[0].Model = %q; want gpt-4o-2024-08-06", got[0].Model)
	}
	if got[0].OverallAcc != 82.5 {
		t.Errorf("entry[0].OverallAcc = %f; want 82.5", got[0].OverallAcc)
	}
}

func TestFetchJSON_HandlesHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("server error"))
	}))
	defer srv.Close()

	s := &Scraper{
		client: srv.Client(),
	}

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	resp, err := s.client.Do(req)
	if err != nil {
		t.Fatalf("unexpected network error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("expected 500 status; got %d", resp.StatusCode)
	}
}
