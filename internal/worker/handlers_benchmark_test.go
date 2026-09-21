package worker

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hibiken/asynq"
	"github.com/prometheus/client_golang/prometheus"
)

type fakeBenchmarkScraper struct {
	scrape func(context.Context) error
}

func (f fakeBenchmarkScraper) Scrape(ctx context.Context) error { return f.scrape(ctx) }

// benchmarkScrapeCounter reads one llm_benchmark_scrape_runs_total child by
// gathering the default registry. Gathering (rather than a per-child read) is
// required because a CounterVec child does not exist until its first
// WithLabelValues call, so an untouched (source, status) pair must read as
// absent rather than as a zero that could hide a missing increment.
//
// prometheus/client_golang/prometheus/testutil is deliberately not used: it
// drags in github.com/kylelemons/godebug, which is not a go.mod requirement.
func benchmarkScrapeCounter(t *testing.T, source, status string) (float64, bool) {
	t.Helper()
	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather default registry: %v", err)
	}
	for _, family := range families {
		if family.GetName() != "llm_benchmark_scrape_runs_total" {
			continue
		}
		for _, metric := range family.GetMetric() {
			var gotSource, gotStatus string
			for _, label := range metric.GetLabel() {
				switch label.GetName() {
				case "source":
					gotSource = label.GetValue()
				case "status":
					gotStatus = label.GetValue()
				}
			}
			if gotSource == source && gotStatus == status {
				return metric.GetCounter().GetValue(), true
			}
		}
	}
	return 0, false
}

// benchmarkScrapeDurationSamples reads the observation count of
// llm_benchmark_scrape_duration_seconds for a source.
func benchmarkScrapeDurationSamples(t *testing.T, source string) uint64 {
	t.Helper()
	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather default registry: %v", err)
	}
	for _, family := range families {
		if family.GetName() != "llm_benchmark_scrape_duration_seconds" {
			continue
		}
		for _, metric := range family.GetMetric() {
			for _, label := range metric.GetLabel() {
				if label.GetName() == "source" && label.GetValue() == source {
					return metric.GetHistogram().GetSampleCount()
				}
			}
		}
	}
	return 0
}

func TestHandleRecomputeCapabilityScores(t *testing.T) {
	h := newTestHandlers(&mockStore{})
	called := 0
	h.recomputeCapabilities = func(context.Context) error {
		called++
		return nil
	}
	if err := h.HandleRecomputeCapabilityScores(context.Background(), asynq.NewTask(TaskRecomputeCapabilityScores, nil)); err != nil {
		t.Fatalf("HandleRecomputeCapabilityScores() error = %v", err)
	}
	if called != 1 {
		t.Fatalf("recompute called %d times; want 1", called)
	}
}

func TestHandleStalenessCheck(t *testing.T) {
	h := newTestHandlers(&mockStore{})
	called := 0
	h.recomputeCapabilities = func(context.Context) error {
		called++
		return nil
	}
	if err := h.HandleStalenessCheck(context.Background(), asynq.NewTask(TaskStalenessCheck, nil)); err != nil {
		t.Fatalf("HandleStalenessCheck() error = %v", err)
	}
	if called != 1 {
		t.Fatalf("compatibility handler called recompute %d times; want 1", called)
	}
}

func TestRunBenchmarkScrape_Success(t *testing.T) {
	h := newTestHandlers(&mockStore{})
	var calls []string
	h.recomputeCapabilities = func(context.Context) error {
		calls = append(calls, "recompute")
		return nil
	}
	s := fakeBenchmarkScraper{scrape: func(context.Context) error {
		calls = append(calls, "scrape")
		return nil
	}}

	if err := h.runBenchmarkScrape(context.Background(), TaskSWEBenchScrape, "swebench", s); err != nil {
		t.Fatalf("runBenchmarkScrape() error = %v", err)
	}
	if len(calls) != 2 || calls[0] != "scrape" || calls[1] != "recompute" {
		t.Fatalf("call order = %v; want [scrape recompute]", calls)
	}
}

func TestRunBenchmarkScrape_ScrapeFailureSkipsRecompute(t *testing.T) {
	h := newTestHandlers(&mockStore{})
	scrapeErr := errors.New("scrape failed")
	recomputeCalls := 0
	h.recomputeCapabilities = func(context.Context) error {
		recomputeCalls++
		return nil
	}
	s := fakeBenchmarkScraper{scrape: func(context.Context) error { return scrapeErr }}

	err := h.runBenchmarkScrape(context.Background(), TaskSWEBenchScrape, "swebench", s)
	if !errors.Is(err, scrapeErr) {
		t.Fatalf("error = %v; want wrapped scrape error", err)
	}
	if recomputeCalls != 0 {
		t.Fatalf("recompute called %d times after scrape failure; want 0", recomputeCalls)
	}
}

func TestRunBenchmarkScrape_RecomputeFailureFailsTask(t *testing.T) {
	h := newTestHandlers(&mockStore{})
	recomputeErr := errors.New("recompute failed")
	h.recomputeCapabilities = func(context.Context) error { return recomputeErr }
	s := fakeBenchmarkScraper{scrape: func(context.Context) error { return nil }}

	err := h.runBenchmarkScrape(context.Background(), TaskSWEBenchScrape, "swebench", s)
	if !errors.Is(err, recomputeErr) {
		t.Fatalf("error = %v; want wrapped recompute error", err)
	}
	if err == nil || !strings.Contains(err.Error(), TaskSWEBenchScrape+": recompute capabilities") {
		t.Fatalf("error = %v; want task and recompute context", err)
	}
}

// TestRunBenchmarkScrape_RecordsSuccessMetrics verifies a successful benchmark
// run increments the success counter for its source and observes one duration
// sample — the signal LLMBenchmarkScrapeFailureConsecutive needs to exist at all.
func TestRunBenchmarkScrape_RecordsSuccessMetrics(t *testing.T) {
	const source = "swebench"
	h := newTestHandlers(&mockStore{})
	h.recomputeCapabilities = func(context.Context) error { return nil }
	s := fakeBenchmarkScraper{scrape: func(context.Context) error { return nil }}

	beforeRuns, _ := benchmarkScrapeCounter(t, source, "success")
	beforeSamples := benchmarkScrapeDurationSamples(t, source)

	if err := h.runBenchmarkScrape(context.Background(), TaskSWEBenchScrape, source, s); err != nil {
		t.Fatalf("runBenchmarkScrape() error = %v", err)
	}

	afterRuns, ok := benchmarkScrapeCounter(t, source, "success")
	if !ok {
		t.Fatalf("llm_benchmark_scrape_runs_total has no sample for %s/success", source)
	}
	if afterRuns != beforeRuns+1 {
		t.Errorf("success runs delta = %v; want 1", afterRuns-beforeRuns)
	}
	if got := benchmarkScrapeDurationSamples(t, source); got != beforeSamples+1 {
		t.Errorf("duration samples delta = %d; want 1", got-beforeSamples)
	}
}

// TestRunBenchmarkScrape_RecordsFailureMetrics verifies both a scrape failure
// and a recompute failure count as errors for the source; the alert must fire
// on any failed run, not only on fetch failures.
func TestRunBenchmarkScrape_RecordsFailureMetrics(t *testing.T) {
	tests := []struct {
		name      string
		source    string
		scrape    func(context.Context) error
		recompute func(context.Context) error
	}{
		{
			name:      "scrape failure",
			source:    "livecodebench",
			scrape:    func(context.Context) error { return errors.New("leaderboard unavailable") },
			recompute: func(context.Context) error { return nil },
		},
		{
			name:      "recompute failure",
			source:    "swebench",
			scrape:    func(context.Context) error { return nil },
			recompute: func(context.Context) error { return errors.New("recompute failed") },
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newTestHandlers(&mockStore{})
			h.recomputeCapabilities = tc.recompute
			s := fakeBenchmarkScraper{scrape: tc.scrape}

			before, _ := benchmarkScrapeCounter(t, tc.source, "error")
			if err := h.runBenchmarkScrape(context.Background(), TaskSWEBenchScrape, tc.source, s); err == nil {
				t.Fatal("runBenchmarkScrape() error = nil; want failure")
			}
			after, ok := benchmarkScrapeCounter(t, tc.source, "error")
			if !ok {
				t.Fatalf("llm_benchmark_scrape_runs_total has no sample for %s/error", tc.source)
			}
			if after != before+1 {
				t.Errorf("error runs delta = %v; want 1", after-before)
			}
		})
	}
}

// TestHandleChatbotArenaScrape verifies the handler creates a scraper and calls Scrape.
// The Chatbot Arena scraper is a no-op stub (upstream API returned 403), so this
// should succeed without error rather than panic.
func TestHandleChatbotArenaScrape(t *testing.T) {
	store := &mockStore{}
	h := newTestHandlers(store)
	err := h.HandleChatbotArenaScrape(context.Background(), asynq.NewTask(TaskChatbotArenaScrape, nil))
	if err != nil {
		t.Fatalf("HandleChatbotArenaScrape() returned error: %v; want nil (no-op stub)", err)
	}
}

// TestBenchmarkTaskConstants verifies task constants are non-empty and unique.
func TestBenchmarkTaskConstants(t *testing.T) {
	tasks := []string{
		TaskSWEBenchScrape,
		TaskLiveCodeBenchScrape,
		TaskChatbotArenaScrape,
		TaskRecomputeCapabilityScores,
		TaskStalenessCheck,
	}
	seen := map[string]bool{}
	for _, task := range tasks {
		if task == "" {
			t.Error("task constant is empty")
		}
		if seen[task] {
			t.Errorf("duplicate task constant: %q", task)
		}
		seen[task] = true
	}
}
