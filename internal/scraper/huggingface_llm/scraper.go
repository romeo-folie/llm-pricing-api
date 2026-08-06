// TODO(refactor): rename package huggingface_llm → livecodebench to match the implementation.
// Package huggingface_llm implements a benchmark scraper for LiveCodeBench.
// It fetches per-question pass@1 scores, aggregates them to per-model averages,
// and upserts them into the model_benchmark_scores table.
package huggingface_llm

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
	// dataURL is the LiveCodeBench performance data endpoint.
	dataURL   = "https://raw.githubusercontent.com/LiveCodeBench/livecodebench.github.io/main/src/mocks/performances_generation.json"
	sourceURL = "https://livecodebench.github.io/leaderboard.html"

	benchmarkName = "LiveCodeBench"
)

// Scraper fetches LiveCodeBench data and upserts benchmark scores.
type Scraper struct {
	db     *pgxpool.Pool
	client *http.Client
	logger zerolog.Logger
}

// Ensure Scraper implements BenchmarkScraper at compile time.
var _ scraper.BenchmarkScraper = (*Scraper)(nil)

// New returns a LiveCodeBench scraper backed by the given DB pool.
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

// lcbData is the top-level JSON structure from the LiveCodeBench data endpoint.
type lcbData struct {
	Models       []lcbModel       `json:"models"`
	Performances []lcbPerformance `json:"performances"`
}

// lcbModel represents one model entry in the LiveCodeBench models array.
type lcbModel struct {
	ModelName  string `json:"model_name"`
	ModelRepr  string `json:"model_repr"`
	ModelStyle string `json:"model_style"`
}

// lcbPerformance represents one per-question performance entry.
type lcbPerformance struct {
	Model string  `json:"model"`
	Pass1 float64 `json:"pass@1"`
}

// modelAgg holds aggregation state for computing per-model average pass@1.
type modelAgg struct {
	sum   float64
	count int
}

type resolvedModel struct {
	modelID   int
	slug      string
	modelName string
	repr      string
	average   float64
	count     int
}

// betterResolvedModel avoids score cherry-picking when multiple upstream
// aliases resolve to one canonical model. Prefer the entry backed by the most
// questions, then stable upstream identity ordering.
func betterResolvedModel(a, b resolvedModel) bool {
	if a.count != b.count {
		return a.count > b.count
	}
	if a.repr != b.repr {
		return a.repr < b.repr
	}
	return a.modelName < b.modelName
}

func selectBestResolvedModels(candidates []resolvedModel) map[int]resolvedModel {
	selected := make(map[int]resolvedModel)
	for _, candidate := range candidates {
		current, exists := selected[candidate.modelID]
		if !exists || betterResolvedModel(candidate, current) {
			selected[candidate.modelID] = candidate
		}
	}
	return selected
}

func modelNamesByRepresentation(models []lcbModel) map[string]string {
	reprToName := make(map[string]string, len(models))
	for _, model := range models {
		current, exists := reprToName[model.ModelRepr]
		if !exists || model.ModelName < current {
			reprToName[model.ModelRepr] = model.ModelName
		}
	}
	return reprToName
}

func evidenceVersion(candidate resolvedModel) string {
	payload := fmt.Sprintf("%s\x00%s\x00%.17g\x00%d",
		candidate.modelName, candidate.repr, candidate.average, candidate.count)
	sum := sha256.Sum256([]byte(payload))
	return fmt.Sprintf("lcb-%x", sum[:8])
}

// Scrape fetches the LiveCodeBench data, aggregates per-model average pass@1,
// resolves model names to canonical slugs, and upserts benchmark scores.
func (s *Scraper) Scrape(ctx context.Context) error {
	benchmarkID, err := intelligence.LookupBenchmarkID(ctx, s.db, benchmarkName)
	if err != nil {
		return fmt.Errorf("livecodebench: %w", err)
	}

	data, err := s.fetchData(ctx)
	if err != nil {
		return fmt.Errorf("livecodebench: %w", err)
	}

	// Build a map from model_repr → model_name for slug resolution.
	reprToName := modelNamesByRepresentation(data.Models)

	// Aggregate performances per model (keyed by model_repr).
	aggs := make(map[string]*modelAgg, len(data.Models))
	for i := range data.Performances {
		p := &data.Performances[i]
		a, ok := aggs[p.Model]
		if !ok {
			a = &modelAgg{}
			aggs[p.Model] = a
		}
		a.sum += p.Pass1
		a.count++
	}

	now := time.Now().UTC()
	var skipped int
	candidates := make([]resolvedModel, 0, len(aggs))
	for repr, agg := range aggs {
		if agg.count == 0 {
			continue
		}
		avgPass1 := agg.sum / float64(agg.count)

		// Try resolving via model_name first, then model_repr.
		modelName := reprToName[repr]
		slug, ok := slugmap.Resolve(modelName)
		if !ok {
			slug, ok = slugmap.Resolve(repr)
		}
		if !ok {
			s.logger.Debug().Str("model_repr", repr).Str("model_name", modelName).Msg("livecodebench: no slug mapping — skipping")
			skipped++
			continue
		}

		modelID, err := intelligence.LookupModelIDBySlug(ctx, s.db, slug)
		if err != nil {
			s.logger.Debug().Str("slug", slug).Err(err).Msg("livecodebench: model not in DB — skipping")
			skipped++
			continue
		}

		candidate := resolvedModel{
			modelID:   modelID,
			slug:      slug,
			modelName: modelName,
			repr:      repr,
			average:   avgPass1,
			count:     agg.count,
		}
		candidates = append(candidates, candidate)
	}
	selected := selectBestResolvedModels(candidates)

	for _, candidate := range selected {
		// pass@1 is already 0–100.
		raw := candidate.average
		norm := candidate.average
		sourceModelName := candidate.modelName
		sourceEntryName := candidate.repr
		if err := intelligence.UpsertBenchmarkScore(ctx, s.db, intelligence.BenchmarkScore{
			ModelID:          candidate.modelID,
			BenchmarkID:      benchmarkID,
			RawScore:         &raw,
			NormalizedScore:  &norm,
			BenchmarkVersion: evidenceVersion(candidate),
			SourceURL:        sourceURL,
			SourceModelName:  &sourceModelName,
			SourceEntryName:  &sourceEntryName,
			Confidence:       "high",
			EvaluatedAt:      now,
			LastObservedAt:   now,
		}); err != nil {
			return fmt.Errorf("livecodebench: upsert score for %s: %w", candidate.slug, err)
		}
	}

	s.logger.Info().
		Int("matched", len(selected)).
		Int("skipped", skipped).
		Int("duplicate_aliases", len(aggs)-skipped-len(selected)).
		Int("total_models", len(data.Models)).
		Int("total_performances", len(data.Performances)).
		Msg("livecodebench: scrape complete")
	return nil
}

// fetchData retrieves the LiveCodeBench JSON data.
func (s *Scraper) fetchData(ctx context.Context) (*lcbData, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, dataURL, nil)
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

	var data lcbData
	if err := json.NewDecoder(io.LimitReader(resp.Body, 50<<20)).Decode(&data); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return &data, nil
}
