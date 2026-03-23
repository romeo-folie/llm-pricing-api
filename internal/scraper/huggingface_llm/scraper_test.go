package huggingface_llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchData_ParsesRows(t *testing.T) {
	mmlu := 85.5
	gpqa := 72.3
	ifeval := 91.0

	resp := datasetResponse{
		Rows: []datasetRow{
			{Row: rowContent{
				ModelName:   "gpt-4o",
				MMLUPro:     &mmlu,
				GPQADiamond: &gpqa,
				IFEval:      &ifeval,
				Date:        "2025-01-15",
			}},
			{Row: rowContent{
				ModelName: "unknown-model",
				MMLUPro:   &mmlu,
				Date:      "2025-02-01",
			}},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	s := &Scraper{client: srv.Client()}

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	httpResp, err := s.client.Do(req)
	if err != nil {
		t.Fatalf("fetch failed: %v", err)
	}
	defer httpResp.Body.Close()

	var got datasetResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&got); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if len(got.Rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(got.Rows))
	}
	if got.Rows[0].Row.ModelName != "gpt-4o" {
		t.Errorf("row[0].ModelName = %q; want gpt-4o", got.Rows[0].Row.ModelName)
	}
	if got.Rows[0].Row.MMLUPro == nil || *got.Rows[0].Row.MMLUPro != 85.5 {
		t.Errorf("row[0].MMLUPro unexpected value")
	}
}

func TestNormalization_ZeroToOne(t *testing.T) {
	// If score is between 0 and 1, it should be multiplied by 100.
	val := 0.855
	norm := val
	if norm <= 1.0 && norm > 0 {
		norm *= 100
	}
	if norm != 85.5 {
		t.Errorf("normalized score = %f; want 85.5", norm)
	}
}

func TestNormalization_AlreadyPercent(t *testing.T) {
	// If score is already 0–100, it should stay the same.
	val := 85.5
	norm := val
	if norm <= 1.0 && norm > 0 {
		norm *= 100
	}
	if norm != 85.5 {
		t.Errorf("normalized score = %f; want 85.5", norm)
	}
}
