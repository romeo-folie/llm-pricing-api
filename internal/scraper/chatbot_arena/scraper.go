// Package chatbot_arena implements a benchmark scraper for the Chatbot Arena
// (LMSYS/lmarena-ai) leaderboard. It fetches ELO/Arena scores from the
// HuggingFace dataset mirror maintained by mathewhe, normalizes them to 0–100,
// and upserts them into the model_benchmark_scores table.
//
// The original lmarena.ai/api/leaderboard endpoint moved to arena.ai and is
// now Cloudflare-protected (returns 403). The mathewhe HuggingFace dataset
// (https://huggingface.co/datasets/mathewhe/chatbot-arena-elo) provides the
// same data via the HF Datasets Server API and is updated regularly.
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
	// datasetBaseURL is the HuggingFace Datasets Server endpoint for the
	// mathewhe/chatbot-arena-elo dataset. This dataset mirrors the live
	// Chatbot Arena leaderboard (updated regularly) and is accessible
	// without authentication. Offset and length are appended per-page.
	datasetBaseURL = "https://datasets-server.huggingface.co/rows?dataset=mathewhe%2Fchatbot-arena-elo&config=default&split=train"
	pageSize       = 100
	sourceURL      = "https://huggingface.co/datasets/mathewhe/chatbot-arena-elo"
	benchmarkName  = "Chatbot Arena"
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

// hfRow matches one row from the HuggingFace Datasets Server response.
type hfRow struct {
	Row struct {
		Model      string `json:"Model"`
		ArenaScore int    `json:"Arena Score"`
	} `json:"row"`
}

// hfResponse is the top-level envelope from the Datasets Server API.
type hfResponse struct {
	Rows []hfRow `json:"rows"`
}

// Scrape fetches the Chatbot Arena leaderboard, resolves model names, normalizes
// Arena scores to 0–100, and upserts benchmark scores.
func (s *Scraper) Scrape(ctx context.Context) error {
	benchmarkID, err := intelligence.LookupBenchmarkID(ctx, s.db, benchmarkName)
	if err != nil {
		return fmt.Errorf("chatbot_arena: %w", err)
	}

	entries, err := s.fetchAllPages(ctx)
	if err != nil {
		return fmt.Errorf("chatbot_arena: %w", err)
	}

	if len(entries) == 0 {
		return fmt.Errorf("chatbot_arena: no entries returned")
	}

	// Extract scores for normalization.
	scores := make([]float64, 0, len(entries))
	for _, e := range entries {
		if e.ArenaScore > 0 {
			scores = append(scores, float64(e.ArenaScore))
		}
	}
	minScore, maxScore := minMax(scores)

	now := time.Now().UTC()
	version := "elo-" + now.Format("2006-01")

	var matched, skipped int
	for _, e := range entries {
		if e.ArenaScore <= 0 {
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

		raw := float64(e.ArenaScore)
		norm := NormalizeELO(raw, minScore, maxScore)

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

// arenaEntry holds the normalized data from one leaderboard row.
type arenaEntry struct {
	Model      string
	ArenaScore int
}

// fetchAllPages paginates through the HuggingFace Datasets Server endpoint
// and returns all rows.
func (s *Scraper) fetchAllPages(ctx context.Context) ([]arenaEntry, error) {
	var all []arenaEntry
	offset := 0
	for {
		url := fmt.Sprintf("%s&offset=%d&length=%d", datasetBaseURL, offset, pageSize)
		rows, err := s.fetchPage(ctx, url)
		if err != nil {
			return nil, err
		}
		if len(rows) == 0 {
			break
		}
		for _, r := range rows {
			all = append(all, arenaEntry{
				Model:      r.Row.Model,
				ArenaScore: r.Row.ArenaScore,
			})
		}
		if len(rows) < pageSize {
			break // Last page.
		}
		offset += pageSize
	}
	return all, nil
}

// fetchPage retrieves one page of results from the HuggingFace Datasets Server.
func (s *Scraper) fetchPage(ctx context.Context, url string) ([]hfRow, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
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

	var hfResp hfResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 10<<20)).Decode(&hfResp); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return hfResp.Rows, nil
}

// NormalizeELO converts an Arena score to a 0–100 scale using min-max normalization.
// Exported for testing.
func NormalizeELO(elo, minELO, maxELO float64) float64 {
	if maxELO == minELO {
		return 50.0 // All models have the same score.
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
