// Package huggingface_llm implements a benchmark scraper for the HuggingFace
// Open LLM Leaderboard v2. It extracts MMLU-Pro, GPQA Diamond, and IFEval
// scores and upserts them into the model_benchmark_scores table.
package huggingface_llm

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
	// datasetURL is the HuggingFace Datasets Server API for the Open LLM Leaderboard results.
	datasetURL = "https://datasets-server.huggingface.co/rows?dataset=open-llm-leaderboard%2Fresults&config=default&split=train&offset=0&length=100"
	sourceURL  = "https://huggingface.co/open-llm-leaderboard/open_llm_leaderboard"
)

// benchmarkDimension maps the leaderboard column names to DB benchmark names.
var benchmarkDimensions = []struct {
	jsonField     string
	benchmarkName string
}{
	{"mmlu_pro", "MMLU-Pro"},
	{"gpqa_diamond", "GPQA Diamond"},
	{"ifeval", "IFEval"},
}

// Scraper fetches HuggingFace Open LLM Leaderboard data and upserts benchmark scores.
type Scraper struct {
	db     *pgxpool.Pool
	client *http.Client
	logger zerolog.Logger
}

// Ensure Scraper implements BenchmarkScraper at compile time.
var _ scraper.BenchmarkScraper = (*Scraper)(nil)

// New returns a HuggingFace LLM leaderboard scraper backed by the given DB pool.
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

// datasetResponse is the top-level envelope from the HuggingFace datasets API.
type datasetResponse struct {
	Rows []datasetRow `json:"rows"`
}

type datasetRow struct {
	Row rowContent `json:"row"`
}

type rowContent struct {
	ModelName   string   `json:"model_name"`
	FullModel   string   `json:"fullname"`
	MMLUPro     *float64 `json:"mmlu_pro"`
	GPQADiamond *float64 `json:"gpqa_diamond"`
	IFEval      *float64 `json:"ifeval"`
	Date        string   `json:"date"`
}

// Scrape fetches the leaderboard, resolves model names to canonical slugs,
// and upserts benchmark scores for MMLU-Pro, GPQA Diamond, and IFEval.
func (s *Scraper) Scrape(ctx context.Context) error {
	// Pre-resolve benchmark IDs.
	type bmEntry struct {
		jsonField string
		name      string
		id        [16]byte // uuid.UUID
	}
	var benchmarks []bmEntry
	for _, bd := range benchmarkDimensions {
		id, err := intelligence.LookupBenchmarkID(ctx, s.db, bd.benchmarkName)
		if err != nil {
			return fmt.Errorf("huggingface_llm: %w", err)
		}
		benchmarks = append(benchmarks, bmEntry{
			jsonField: bd.jsonField,
			name:      bd.benchmarkName,
			id:        id,
		})
	}

	rows, err := s.fetchData(ctx)
	if err != nil {
		return fmt.Errorf("huggingface_llm: %w", err)
	}

	// Filter to entries from the last 6 months.
	cutoff := time.Now().AddDate(0, -6, 0)
	now := time.Now().UTC()

	var matched, skipped int
	for _, row := range rows {
		r := row.Row

		// Filter by date if available.
		if r.Date != "" {
			if t, err := time.Parse("2006-01-02", r.Date); err == nil && t.Before(cutoff) {
				continue
			}
		}

		// Try to resolve the model name.
		name := r.ModelName
		if name == "" {
			name = r.FullModel
		}
		slug, ok := slugmap.Resolve(name)
		if !ok {
			s.logger.Debug().Str("model", name).Msg("huggingface_llm: no slug mapping — skipping")
			skipped++
			continue
		}

		modelID, err := intelligence.LookupModelIDBySlug(ctx, s.db, slug)
		if err != nil {
			s.logger.Debug().Str("slug", slug).Err(err).Msg("huggingface_llm: model not in DB — skipping")
			skipped++
			continue
		}

		// Extract each benchmark score.
		scores := map[string]*float64{
			"mmlu_pro":     r.MMLUPro,
			"gpqa_diamond": r.GPQADiamond,
			"ifeval":       r.IFEval,
		}

		for _, bm := range benchmarks {
			val := scores[bm.jsonField]
			if val == nil {
				continue
			}
			// Scores may be 0–1 or 0–100 depending on the dataset version.
			// Normalize to 0–100.
			norm := *val
			if norm <= 1.0 && norm > 0 {
				norm *= 100
			}
			raw := *val
			if err := intelligence.UpsertBenchmarkScore(ctx, s.db, intelligence.BenchmarkScore{
				ModelID:          modelID,
				BenchmarkID:      bm.id,
				RawScore:         &raw,
				NormalizedScore:  &norm,
				BenchmarkVersion: "v2-" + now.Format("2006-01"),
				SourceURL:        sourceURL,
				Confidence:       "high",
				EvaluatedAt:      now,
			}); err != nil {
				return fmt.Errorf("huggingface_llm: upsert %s for %s: %w", bm.name, slug, err)
			}
		}
		matched++
	}

	s.logger.Info().Int("matched", matched).Int("skipped", skipped).Msg("huggingface_llm: scrape complete")
	return nil
}

// fetchData retrieves rows from the HuggingFace Datasets Server API.
func (s *Scraper) fetchData(ctx context.Context) ([]datasetRow, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, datasetURL, nil)
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

	var dr datasetResponse
	if err := json.NewDecoder(resp.Body).Decode(&dr); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return dr.Rows, nil
}
