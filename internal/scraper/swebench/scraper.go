// Package swebench implements a benchmark scraper for SWE-bench Verified.
// It fetches resolved-percentage scores from the official SWE-bench
// leaderboard JSON and upserts them into the model_benchmark_scores table
// via the intelligence package.
package swebench

import (
	"context"
	"crypto/sha256"
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

// resolvedEntry is a valid upstream agent-system result attached to a known
// canonical model. Multiple submissions can use the same base model.
type resolvedEntry struct {
	entry           leaderboardEntry
	sourceModelName string
	slug            string
	modelID         int
	evaluatedAt     time.Time
}

// extractModelFromTags returns a model only when the submission identifies one
// unambiguous base model. Multi-model agent systems are skipped because their
// system-level score cannot be attributed safely to any one component model.
func extractModelFromTags(tags []string) (string, bool) {
	var model string
	for _, tag := range tags {
		if !strings.HasPrefix(tag, "Model: ") {
			continue
		}
		candidate := strings.TrimSpace(strings.TrimPrefix(tag, "Model: "))
		if candidate == "" {
			continue
		}
		if model != "" && candidate != model {
			return "", false
		}
		model = candidate
	}
	return model, model != ""
}

// betterResolvedEntry defines the order-independent SWE-bench selection
// policy: best resolved percentage, then newest evaluation, then stable
// upstream identity tie-breakers.
func betterResolvedEntry(a, b resolvedEntry) bool {
	if a.entry.Resolved != b.entry.Resolved {
		return a.entry.Resolved > b.entry.Resolved
	}
	if !a.evaluatedAt.Equal(b.evaluatedAt) {
		return a.evaluatedAt.After(b.evaluatedAt)
	}
	if a.entry.Name != b.entry.Name {
		return a.entry.Name < b.entry.Name
	}
	return a.sourceModelName < b.sourceModelName
}

func selectBestEntries(entries []resolvedEntry) map[int]resolvedEntry {
	selected := make(map[int]resolvedEntry)
	for _, candidate := range entries {
		current, ok := selected[candidate.modelID]
		if !ok || betterResolvedEntry(candidate, current) {
			selected[candidate.modelID] = candidate
		}
	}
	return selected
}

func evidenceVersion(candidate resolvedEntry) string {
	payload := fmt.Sprintf("%s\x00%s\x00%s\x00%.17g",
		candidate.entry.Date, candidate.sourceModelName, candidate.entry.Name, candidate.entry.Resolved)
	sum := sha256.Sum256([]byte(payload))
	return fmt.Sprintf("swebench-%s-%x", candidate.entry.Date, sum[:8])
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
	observedAt := time.Now().UTC()

	var skipped, noTag, invalidDate int
	resolved := make([]resolvedEntry, 0, len(entries))
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

		evaluatedAt, parseErr := time.Parse("2006-01-02", e.Date)
		if parseErr != nil {
			s.logger.Debug().Str("entry", e.Name).Str("date", e.Date).Msg("swebench: invalid evaluation date — skipping")
			invalidDate++
			continue
		}

		resolved = append(resolved, resolvedEntry{
			entry:           e,
			sourceModelName: modelName,
			slug:            slug,
			modelID:         modelID,
			evaluatedAt:     evaluatedAt,
		})
	}

	selected := selectBestEntries(resolved)
	for _, candidate := range selected {
		// Resolved is already a 0–100 percentage. It is an official score for
		// an agent system, but only indirect evidence about the base model.
		raw := candidate.entry.Resolved
		norm := candidate.entry.Resolved
		sourceModelName := candidate.sourceModelName
		sourceEntryName := candidate.entry.Name

		if err := intelligence.UpsertBenchmarkScore(ctx, s.db, intelligence.BenchmarkScore{
			ModelID:          candidate.modelID,
			BenchmarkID:      benchmarkID,
			RawScore:         &raw,
			NormalizedScore:  &norm,
			BenchmarkVersion: evidenceVersion(candidate),
			SourceURL:        sourceURL,
			SourceModelName:  &sourceModelName,
			SourceEntryName:  &sourceEntryName,
			Confidence:       "low",
			EvaluatedAt:      candidate.evaluatedAt,
			LastObservedAt:   observedAt,
		}); err != nil {
			return fmt.Errorf("swebench: upsert score for %s: %w", candidate.slug, err)
		}
	}

	s.logger.Info().
		Int("matched", len(selected)).
		Int("skipped", skipped).
		Int("no_model_tag", noTag).
		Int("invalid_date", invalidDate).
		Int("duplicate_submissions", len(resolved)-len(selected)).
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
