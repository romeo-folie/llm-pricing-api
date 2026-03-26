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
	// datasetBaseURL is the HuggingFace Datasets Server API for the Open LLM Leaderboard v2
	// contents dataset. The original open-llm-leaderboard/results endpoint returns HTTP 500
	// due to a schema change on the HuggingFace side; open-llm-leaderboard/contents is the
	// canonical replacement and returns the same benchmark columns.
	datasetBaseURL = "https://datasets-server.huggingface.co/rows?dataset=open-llm-leaderboard%2Fcontents&config=default&split=train"
	pageSize       = 100
	// maxPages caps the number of pages fetched per run to avoid runaway scrapes.
	// The dataset has ~4500 rows so 45 pages at 100 rows each covers it fully.
	maxPages  = 50
	sourceURL = "https://huggingface.co/spaces/open-llm-leaderboard/open_llm_leaderboard"
)

// benchmarkDimension maps the leaderboard column names to DB benchmark names.
// The /contents dataset uses "MMLU-PRO", "GPQA", "IFEval" as column names.
var benchmarkDimensions = []struct {
	jsonField     string
	benchmarkName string
}{
	{"MMLU-PRO", "MMLU-Pro"},
	{"GPQA", "GPQA Diamond"},
	{"IFEval", "IFEval"},
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
	// open-llm-leaderboard/contents column names.
	FullModel   string   `json:"fullname"`
	ModelMarkup string   `json:"Model"` // HTML anchor tag — not used directly
	MMLUPro     *float64 `json:"MMLU-PRO"`
	GPQADiamond *float64 `json:"GPQA"`
	IFEval      *float64 `json:"IFEval"`
	SubmittedAt string   `json:"Submission Date"`
	UploadedAt  string   `json:"Upload To Hub Date"`
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

	now := time.Now().UTC()

	var matched, skipped int
	for _, row := range rows {
		r := row.Row

		// Use the fullname field as the model identifier. It is the HuggingFace
		// repo path (e.g. "meta-llama/Llama-3.3-70B-Instruct") which is the most
		// stable identifier across leaderboard versions.
		name := r.FullModel
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
			"MMLU-PRO": r.MMLUPro,
			"GPQA":     r.GPQADiamond,
			"IFEval":   r.IFEval,
		}

		for _, bm := range benchmarks {
			val := scores[bm.jsonField] // jsonField is the JSON column name e.g. "MMLU-PRO"
			if val == nil {
				continue
			}
			// The HuggingFace Open LLM Leaderboard v2 returns scores in 0–100 range.
			// Do not apply any heuristic normalization — the <= 1.0 * 100 pattern is
			// ambiguous (a model scoring exactly 1.0% would be inflated to 100%) and
			// fragile across API versions. Trust the data as-is.
			norm := *val
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
	var all []datasetRow
	for page := 0; page < maxPages; page++ {
		offset := page * pageSize
		url := fmt.Sprintf("%s&offset=%d&length=%d", datasetBaseURL, offset, pageSize)
		rows, err := s.fetchPage(ctx, url)
		if err != nil {
			return nil, fmt.Errorf("page %d: %w", page, err)
		}
		all = append(all, rows...)
		if len(rows) < pageSize {
			// Last page — no more results.
			break
		}
	}
	return all, nil
}

func (s *Scraper) fetchPage(ctx context.Context, url string) ([]datasetRow, error) {
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

	var dr datasetResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 10<<20)).Decode(&dr); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return dr.Rows, nil
}
