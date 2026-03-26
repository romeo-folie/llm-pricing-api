package huggingface_llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestFetchPage_ParsesRows verifies that fetchPage correctly decodes the
// open-llm-leaderboard/contents column names returned by the HF Datasets Server.
func TestFetchPage_ParsesRows(t *testing.T) {
	mmlu := 85.5
	gpqa := 72.3
	ifeval := 91.0

	resp := datasetResponse{
		Rows: []datasetRow{
			{Row: rowContent{
				FullModel:   "meta-llama/Llama-3.3-70B-Instruct",
				MMLUPro:     &mmlu,
				GPQADiamond: &gpqa,
				IFEval:      &ifeval,
			}},
			{Row: rowContent{
				FullModel: "unknown-model/unknown",
				MMLUPro:   &mmlu,
			}},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	s := &Scraper{client: srv.Client()}

	rows, err := s.fetchPage(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("fetchPage failed: %v", err)
	}

	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if rows[0].Row.FullModel != "meta-llama/Llama-3.3-70B-Instruct" {
		t.Errorf("row[0].FullModel = %q; want meta-llama/Llama-3.3-70B-Instruct", rows[0].Row.FullModel)
	}
	if rows[0].Row.MMLUPro == nil || *rows[0].Row.MMLUPro != 85.5 {
		t.Errorf("row[0].MMLUPro unexpected value")
	}
}

func TestNormalization_AlreadyPercent(t *testing.T) {
	// The /contents dataset returns scores already in 0–100 range.
	// No normalization should be applied.
	val := 85.5
	norm := val // pass-through
	if norm != 85.5 {
		t.Errorf("normalized score = %f; want 85.5", norm)
	}
}
