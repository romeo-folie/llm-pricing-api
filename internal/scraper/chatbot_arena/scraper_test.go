package chatbot_arena

import (
	"context"
	"testing"

	"github.com/rs/zerolog"
)

func TestScrape_NoOp(t *testing.T) {
	s := New(nil, nil)
	s.SetLogger(zerolog.Nop())

	err := s.Scrape(context.Background())
	if err != nil {
		t.Fatalf("Scrape() returned error: %v; want nil (no-op)", err)
	}
}

func TestNew_ReturnsNonNil(t *testing.T) {
	s := New(nil, nil)
	if s == nil {
		t.Fatal("New(nil, nil) returned nil")
	}
}
