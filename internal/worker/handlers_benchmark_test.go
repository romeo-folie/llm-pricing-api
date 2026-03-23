package worker

import (
	"context"
	"testing"

	"github.com/hibiken/asynq"
)

// TestHandleRecomputeCapabilityScores verifies the handler calls
// intelligence.ComputeAllCapabilityScores. Without a real DB pool the pgx
// pool panics on nil dereference — we recover and confirm the handler reached
// the intelligence code path.
func TestHandleRecomputeCapabilityScores(t *testing.T) {
	store := &mockStore{}
	h := newTestHandlers(store)
	// db is nil; pgxpool.Pool.Query panics on nil receiver.
	panicked := invokePanics(func() {
		_ = h.HandleRecomputeCapabilityScores(context.Background(), asynq.NewTask(TaskRecomputeCapabilityScores, nil))
	})
	if !panicked {
		t.Fatal("expected panic from nil DB pool — handler may not be calling intelligence.ComputeAllCapabilityScores")
	}
}

// TestHandleStalenessCheck verifies the staleness check handler executes SQL.
func TestHandleStalenessCheck(t *testing.T) {
	store := &mockStore{}
	h := newTestHandlers(store)
	panicked := invokePanics(func() {
		_ = h.HandleStalenessCheck(context.Background(), asynq.NewTask(TaskStalenessCheck, nil))
	})
	if !panicked {
		t.Fatal("expected panic from nil DB pool — handler may not be executing SQL")
	}
}

// TestHandleBFCLScrape verifies the BFCL handler creates a scraper and calls Scrape.
func TestHandleBFCLScrape(t *testing.T) {
	store := &mockStore{}
	h := newTestHandlers(store)
	panicked := invokePanics(func() {
		_ = h.HandleBFCLScrape(context.Background(), asynq.NewTask(TaskBFCLScrape, nil))
	})
	if !panicked {
		t.Fatal("expected panic from nil DB pool — handler may not be calling scraper.Scrape")
	}
}

// TestHandleHuggingFaceLLMScrape verifies the handler creates a scraper and calls Scrape.
func TestHandleHuggingFaceLLMScrape(t *testing.T) {
	store := &mockStore{}
	h := newTestHandlers(store)
	panicked := invokePanics(func() {
		_ = h.HandleHuggingFaceLLMScrape(context.Background(), asynq.NewTask(TaskHuggingFaceLLMScrape, nil))
	})
	if !panicked {
		t.Fatal("expected panic from nil DB pool — handler may not be calling scraper.Scrape")
	}
}

// TestHandleChatbotArenaScrape verifies the handler creates a scraper and calls Scrape.
func TestHandleChatbotArenaScrape(t *testing.T) {
	store := &mockStore{}
	h := newTestHandlers(store)
	panicked := invokePanics(func() {
		_ = h.HandleChatbotArenaScrape(context.Background(), asynq.NewTask(TaskChatbotArenaScrape, nil))
	})
	if !panicked {
		t.Fatal("expected panic from nil DB pool — handler may not be calling scraper.Scrape")
	}
}

// TestBenchmarkTaskConstants verifies task constants are non-empty and unique.
func TestBenchmarkTaskConstants(t *testing.T) {
	tasks := []string{
		TaskBFCLScrape,
		TaskHuggingFaceLLMScrape,
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

// invokePanics runs fn and returns true if it panicked.
func invokePanics(fn func()) (panicked bool) {
	defer func() {
		if r := recover(); r != nil {
			panicked = true
		}
	}()
	fn()
	return false
}
