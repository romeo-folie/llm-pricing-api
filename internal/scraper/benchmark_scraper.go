package scraper

import "context"

// BenchmarkScraper is implemented by scrapers that fetch benchmark scores
// (as opposed to pricing data). Each implementation fetches scores from an
// external leaderboard, maps model names to canonical DB slugs, and upserts
// rows into model_benchmark_scores via intelligence.UpsertBenchmarkScore.
type BenchmarkScraper interface {
	Scrape(ctx context.Context) error
}
