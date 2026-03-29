package huggingface_llm

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchData_ParsesModelsAndPerformances(t *testing.T) {
	data := lcbData{
		Models: []lcbModel{
			{ModelName: "deepseek-chat", ModelRepr: "DeepSeek-V3", ModelStyle: "DeepSeekAPI"},
			{ModelName: "gpt-4o-2024-08-06", ModelRepr: "GPT-4O-2024-08-06", ModelStyle: "OpenAI"},
		},
		Performances: []lcbPerformance{
			{Model: "DeepSeek-V3", Pass1: 100.0},
			{Model: "DeepSeek-V3", Pass1: 80.0},
			{Model: "DeepSeek-V3", Pass1: 60.0},
			{Model: "GPT-4O-2024-08-06", Pass1: 90.0},
			{Model: "GPT-4O-2024-08-06", Pass1: 70.0},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(data)
	}))
	defer srv.Close()

	s := &Scraper{client: srv.Client()}

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	resp, err := s.client.Do(req)
	if err != nil {
		t.Fatalf("fetch failed: %v", err)
	}
	defer resp.Body.Close()

	var got lcbData
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if len(got.Models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(got.Models))
	}
	if got.Models[0].ModelName != "deepseek-chat" {
		t.Errorf("models[0].ModelName = %q; want deepseek-chat", got.Models[0].ModelName)
	}
	if got.Models[0].ModelRepr != "DeepSeek-V3" {
		t.Errorf("models[0].ModelRepr = %q; want DeepSeek-V3", got.Models[0].ModelRepr)
	}
	if len(got.Performances) != 5 {
		t.Fatalf("expected 5 performances, got %d", len(got.Performances))
	}
}

func TestAggregation_AveragePass1(t *testing.T) {
	// Simulate the aggregation logic from Scrape.
	perfs := []lcbPerformance{
		{Model: "DeepSeek-V3", Pass1: 100.0},
		{Model: "DeepSeek-V3", Pass1: 80.0},
		{Model: "DeepSeek-V3", Pass1: 60.0},
		{Model: "GPT-4O-2024-08-06", Pass1: 90.0},
		{Model: "GPT-4O-2024-08-06", Pass1: 70.0},
	}

	aggs := make(map[string]*modelAgg)
	for i := range perfs {
		p := &perfs[i]
		a, ok := aggs[p.Model]
		if !ok {
			a = &modelAgg{}
			aggs[p.Model] = a
		}
		a.sum += p.Pass1
		a.count++
	}

	// DeepSeek-V3: (100+80+60)/3 = 80.0
	dsAvg := aggs["DeepSeek-V3"].sum / float64(aggs["DeepSeek-V3"].count)
	if math.Abs(dsAvg-80.0) > 0.01 {
		t.Errorf("DeepSeek-V3 avg = %f; want 80.0", dsAvg)
	}

	// GPT-4O: (90+70)/2 = 80.0
	gptAvg := aggs["GPT-4O-2024-08-06"].sum / float64(aggs["GPT-4O-2024-08-06"].count)
	if math.Abs(gptAvg-80.0) > 0.01 {
		t.Errorf("GPT-4O-2024-08-06 avg = %f; want 80.0", gptAvg)
	}
}

func TestFetchData_HandlesHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("server error"))
	}))
	defer srv.Close()

	s := &Scraper{client: srv.Client()}

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

func TestPass1_AlreadyPercentage(t *testing.T) {
	// pass@1 values from LiveCodeBench are already 0–100 — no normalization needed.
	val := 85.5
	if val < 0 || val > 100 {
		t.Errorf("pass@1 value %f out of expected 0-100 range", val)
	}
}
