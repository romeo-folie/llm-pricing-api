// Package chatbot_arena implements a benchmark scraper for the Chatbot Arena
// (LMSYS) leaderboard. It fetches ELO scores, normalizes them to 0–100, and
// upserts them into the model_benchmark_scores table.
package chatbot_arena

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"llm-pricing-api/internal/intelligence"
	"llm-pricing-api/internal/scraper"
	"llm-pricing-api/internal/scraper/slugmap"
)

const (
	// primaryURL is the public JSON export of Arena Hard results.
	primaryURL = "https://raw.githubusercontent.com/lm-sys/FastChat/main/fastchat/serve/leaderboard/data/leaderboard_table_20240701.json"
	sourceURL  = "https://lmarena.ai"
	benchmarkName = "Chatbot Arena"
)

// Scraper fetches Chatbot Arena leaderboard data and upserts benchmark scores.
type Scraper struct {
	db     *pgxpool.Pool
	client *http.Client
	logger zerolog.Logger
}

// Ensure Scraper implements BenchmarkScraper at compile time.
var _ scraper.BenchmarkScraper = (*Scraper)(nil)

// New returns a Chatbot Arena scraper backed by the given DB pool.
func New(db *pgxpool.Pool, client *http.Client) *Scraper {
	if client == nil {
		client = &http.Client{
			Timeout:   60 * time.Second,
			Transport: scraper.NewSSRFSafeTransport(),
			CheckRedirect: func(req *http.Request, _ []*http.Request) error {
				return scraper.CheckRedirectHost(req.Context(), req.URL.Hostname())
			},
		}
	}
	return &Scraper{db: db, client: client, logger: zerolog.Nop()}
}

// SetLogger configures the structured logger.
func (s *Scraper) SetLogger(l zerolog.Logger) { s.logger = l }

// arenaEntry represents one model in the Chatbot Arena leaderboard JSON.
// The JSON format varies across sources; we support the common fields.
type arenaEntry struct {
	Model string  `json:"model"`
	ELO   float64 `json:"elo"`
	Rating float64 `json:"rating"`
}

// Scrape fetches the Chatbot Arena leaderboard, resolves model names, normalizes
// ELO scores to 0–100, and upserts benchmark scores.
func (s *Scraper) Scrape(ctx context.Context) error {
	benchmarkID, err := intelligence.LookupBenchmarkID(ctx, s.db, benchmarkName)
	if err != nil {
		return fmt.Errorf("chatbot_arena: %w", err)
	}

	entries, err := s.fetchJSON(ctx)
	if err != nil {
		return fmt.Errorf("chatbot_arena: %w", err)
	}

	if len(entries) == 0 {
		return fmt.Errorf("chatbot_arena: no entries returned")
	}

	// Extract ELO scores for normalization.
	elos := make([]float64, 0, len(entries))
	for i := range entries {
		elo := entries[i].ELO
		if elo == 0 {
			elo = entries[i].Rating
		}
		entries[i].ELO = elo
		if elo > 0 {
			elos = append(elos, elo)
		}
	}

	minELO, maxELO := minMax(elos)

	now := time.Now().UTC()
	version := "elo-" + now.Format("2006-01")

	var matched, skipped int
	for _, e := range entries {
		if e.ELO <= 0 {
			continue
		}

		slug, ok := slugmap.Resolve(e.Model)
		if !ok {
			s.logger.Debug().Str("model", e.Model).Msg("chatbot_arena: no slug mapping — skipping")
			skipped++
			continue
		}

		modelID, err := intelligence.LookupModelIDBySlug(ctx, s.db, slug)
		if err != nil {
			s.logger.Debug().Str("slug", slug).Err(err).Msg("chatbot_arena: model not in DB — skipping")
			skipped++
			continue
		}

		raw := e.ELO
		norm := NormalizeELO(e.ELO, minELO, maxELO)

		if err := intelligence.UpsertBenchmarkScore(ctx, s.db, intelligence.BenchmarkScore{
			ModelID:          modelID,
			BenchmarkID:      benchmarkID,
			RawScore:         &raw,
			NormalizedScore:  &norm,
			BenchmarkVersion: version,
			SourceURL:        sourceURL,
			Confidence:       "high",
			EvaluatedAt:      now,
		}); err != nil {
			return fmt.Errorf("chatbot_arena: upsert score for %s: %w", slug, err)
		}
		matched++
	}

	s.logger.Info().Int("matched", matched).Int("skipped", skipped).Msg("chatbot_arena: scrape complete")
	return nil
}

// NormalizeELO converts an ELO score to a 0–100 scale using min-max normalization.
// Exported for testing.
func NormalizeELO(elo, minELO, maxELO float64) float64 {
	if maxELO == minELO {
		return 50.0 // All models have the same ELO.
	}
	norm := (elo - minELO) / (maxELO - minELO) * 100
	return math.Round(norm*100) / 100 // Round to 2 decimal places.
}

// minMax returns the minimum and maximum values from a float64 slice.
func minMax(vals []float64) (min, max float64) {
	if len(vals) == 0 {
		return 0, 0
	}
	min = vals[0]
	max = vals[0]
	for _, v := range vals[1:] {
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
	}
	return min, max
}

// fetchJSON retrieves the leaderboard JSON.
func (s *Scraper) fetchJSON(ctx context.Context) ([]arenaEntry, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, primaryURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", "llm-pricing-api/1.0")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var entries []arenaEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return entries, nil
}
