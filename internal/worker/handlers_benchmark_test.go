package worker

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hibiken/asynq"
)

type fakeBenchmarkScraper struct {
	scrape func(context.Context) error
}

func (f fakeBenchmarkScraper) Scrape(ctx context.Context) error { return f.scrape(ctx) }

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

	if err := h.runBenchmarkScrape(context.Background(), TaskSWEBenchScrape, s); err != nil {
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

	err := h.runBenchmarkScrape(context.Background(), TaskSWEBenchScrape, s)
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

	err := h.runBenchmarkScrape(context.Background(), TaskSWEBenchScrape, s)
	if !errors.Is(err, recomputeErr) {
		t.Fatalf("error = %v; want wrapped recompute error", err)
	}
	if err == nil || !strings.Contains(err.Error(), TaskSWEBenchScrape+": recompute capabilities") {
		t.Fatalf("error = %v; want task and recompute context", err)
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
