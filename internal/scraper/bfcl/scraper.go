// Package bfcl implements a benchmark scraper for SWE-bench Verified.
// It fetches resolved-percentage scores from the official SWE-bench
// leaderboard JSON and upserts them into the model_benchmark_scores table
// via the intelligence package.
package bfcl

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	// jsonURL is the official SWE-bench leaderboard data endpoint.
	jsonURL = "https://raw.githubusercontent.com/SWE-bench/swe-bench.github.io/master/data/leaderboards.json"
	// sourceURL is the human-readable leaderboard page used for source attribution.
	sourceURL     = "https://www.swebench.com/"
	benchmarkName = "SWE-bench Verified"
)

// Scraper fetches SWE-bench Verified leaderboard data and upserts benchmark scores.
type Scraper struct {
	db     *pgxpool.Pool
	client *http.Client
	logger zerolog.Logger
}

// Ensure Scraper implements BenchmarkScraper at compile time.
var _ scraper.BenchmarkScraper = (*Scraper)(nil)

// New returns a SWE-bench Verified scraper backed by the given DB pool.
// If client is nil, a default client with a 60s timeout and SSRF-safe transport
// is used.
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

// leaderboardFile is the top-level JSON structure from the SWE-bench data endpoint.
type leaderboardFile struct {
	Leaderboards []leaderboard `json:"leaderboards"`
}

// leaderboard represents one named leaderboard (e.g. "Verified", "Lite").
type leaderboard struct {
	Name    string             `json:"name"`
	Results []leaderboardEntry `json:"results"`
}

// leaderboardEntry represents one row in a SWE-bench leaderboard.
type leaderboardEntry struct {
	Name     string   `json:"name"`
	Resolved float64  `json:"resolved"`
	Date     string   `json:"date"`
	Tags     []string `json:"tags"`
}

// extractModelFromTags returns the first "Model: <value>" tag value, or ("", false).
func extractModelFromTags(tags []string) (string, bool) {
	for _, tag := range tags {
		if strings.HasPrefix(tag, "Model: ") {
			return strings.TrimPrefix(tag, "Model: "), true
		}
	}
	return "", false
}

// Scrape fetches the SWE-bench Verified leaderboard, resolves model names to
// canonical slugs, and upserts benchmark scores for each matched model.
func (s *Scraper) Scrape(ctx context.Context) error {
	benchmarkID, err := intelligence.LookupBenchmarkID(ctx, s.db, benchmarkName)
	if err != nil {
		return fmt.Errorf("swebench: %w", err)
	}

	entries, err := s.fetchVerifiedEntries(ctx)
	if err != nil {
		return fmt.Errorf("swebench: %w", err)
	}

	now := time.Now().UTC()
	version := fmt.Sprintf("swebench-%s", now.Format("2006-01"))

	var matched, skipped, noTag int
	for _, e := range entries {
		if e.Resolved <= 0 {
			skipped++
			continue
		}

		modelName, ok := extractModelFromTags(e.Tags)
		if !ok {
			noTag++
			continue
		}

		slug, ok := slugmap.Resolve(modelName)
		if !ok {
			s.logger.Debug().Str("model", modelName).Msg("swebench: no slug mapping — skipping")
			skipped++
			continue
		}

		modelID, err := intelligence.LookupModelIDBySlug(ctx, s.db, slug)
		if err != nil {
			s.logger.Debug().Str("slug", slug).Err(err).Msg("swebench: model not in DB — skipping")
			skipped++
			continue
		}

		// Resolved is already 0–100 percentage.
		raw := e.Resolved
		norm := e.Resolved

		evaluatedAt := now
		if e.Date != "" {
			if t, parseErr := time.Parse("2006-01-02", e.Date); parseErr == nil {
				evaluatedAt = t
			}
		}

		if err := intelligence.UpsertBenchmarkScore(ctx, s.db, intelligence.BenchmarkScore{
			ModelID:          modelID,
			BenchmarkID:      benchmarkID,
			RawScore:         &raw,
			NormalizedScore:  &norm,
			BenchmarkVersion: version,
			SourceURL:        sourceURL,
			Confidence:       "high",
			EvaluatedAt:      evaluatedAt,
		}); err != nil {
			return fmt.Errorf("swebench: upsert score for %s: %w", slug, err)
		}
		matched++
	}

	s.logger.Info().
		Int("matched", matched).
		Int("skipped", skipped).
		Int("no_model_tag", noTag).
		Int("total_entries", len(entries)).
		Msg("swebench: scrape complete")
	return nil
}

// fetchVerifiedEntries retrieves the leaderboard JSON and returns the "Verified"
// leaderboard entries.
func (s *Scraper) fetchVerifiedEntries(ctx context.Context) ([]leaderboardEntry, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, jsonURL, nil)
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

	var lf leaderboardFile
	if err := json.NewDecoder(io.LimitReader(resp.Body, 10<<20)).Decode(&lf); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}

	for _, lb := range lf.Leaderboards {
		if lb.Name == "Verified" {
			return lb.Results, nil
		}
	}

	return nil, fmt.Errorf("no 'Verified' leaderboard found in response")
}
