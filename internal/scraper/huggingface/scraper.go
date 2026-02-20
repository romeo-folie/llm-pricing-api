package huggingface

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"llm-pricing-api/internal/scraper"
)

const (
	defaultBaseURL = "https://huggingface.co/api/models"
	defaultQuery   = "inference_provider=all&expand[]=inferenceProviderMapping&pipeline_tag=text-generation&sort=likes&direction=-1&limit=500"
)

// providerNormMap maps HuggingFace provider IDs to OpenRouter-compatible prefix conventions.
// Unknown providers pass through as-is (the map is best-effort for known aggregators).
var providerNormMap = map[string]string{
	"together-ai":  "together",
	"replicate":    "replicate",
	"fal-ai":       "fal-ai",
	"nebius":       "nebius",
	"hyperbolic":   "hyperbolic",
	"sambanova":    "sambanova",
	"fireworks-ai": "fireworks",
	"hf-inference": "hf-inference",
}

// Scraper fetches model pricing from the HuggingFace Inference Providers API daily.
type Scraper struct {
	client  *http.Client
	baseURL string
}

// New returns a Scraper using the provided HTTP client.
// If client is nil, a default client with a 60s timeout and SSRF prevention
// (RFC-1918 / loopback / link-local addresses blocked at the Transport and
// CheckRedirect layers) is used.
func New(client *http.Client) *Scraper {
	if client == nil {
		client = &http.Client{
			Timeout:   60 * time.Second,
			Transport: newSSRFSafeTransport(),
			// CheckRedirect provides a fast, early-exit block for redirect URLs
			// that point to private IPs — defense-in-depth alongside DialContext.
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return checkRedirectHost(req.Context(), req.URL.Hostname())
			},
		}
	}
	return &Scraper{client: client, baseURL: defaultBaseURL}
}

// newSSRFSafeTransport returns an http.Transport whose DialContext resolves the
// target hostname once, validates all resolved IPs against the private/loopback
// blocklist, and then connects directly to the first safe IP — eliminating the
// DNS rebinding window that exists when resolution and TCP connect are separate
// operations (SSRF prevention per CLAUDE.md).
func newSSRFSafeTransport() *http.Transport {
	base := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	return &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, fmt.Errorf("huggingface: split addr %q: %w", addr, err)
			}
			resolved, err := net.DefaultResolver.LookupHost(ctx, host)
			if err != nil {
				return nil, fmt.Errorf("huggingface: resolve %q: %w", host, err)
			}
			if err := checkIPs(resolved); err != nil {
				return nil, err
			}
			// Connect directly to the first resolved IP to prevent re-resolution
			// (and the associated rebinding window) inside the dialer.
			return base.DialContext(ctx, network, net.JoinHostPort(resolved[0], port))
		},
	}
}

// checkIPs returns an error if any address in addrs is a private, loopback, or
// link-local IP.  It is the shared inner check used by both checkRedirectHost
// and newSSRFSafeTransport's DialContext.
func checkIPs(addrs []string) error {
	for _, a := range addrs {
		ip := net.ParseIP(a)
		if ip == nil {
			continue
		}
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() {
			return fmt.Errorf("huggingface: address %s blocked (SSRF prevention)", ip)
		}
	}
	return nil
}

// checkRedirectHost blocks redirects to private IP ranges and loopback.
// Returns an error (blocking the redirect) if the host resolves to a
// private or loopback address (SSRF prevention).
func checkRedirectHost(ctx context.Context, host string) error {
	// Strip port if present.
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	addrs, err := net.DefaultResolver.LookupHost(ctx, host)
	if err != nil {
		// If we can't resolve it, block it (fail-closed).
		return fmt.Errorf("huggingface: redirect host %q unresolvable: %w", host, err)
	}
	return checkIPs(addrs)
}

// hfModel is one element of the HuggingFace /api/models JSON array.
type hfModel struct {
	ID                       string                     `json:"id"`
	PipelineTag              string                     `json:"pipeline_tag"`
	InferenceProviderMapping map[string]hfProviderEntry `json:"inferenceProviderMapping"`
}

type hfProviderEntry struct {
	Provider string    `json:"provider"`
	Status   string    `json:"status"`
	Pricing  hfPricing `json:"pricing"`
}

type hfPricing struct {
	Input  float64 `json:"input"`
	Output float64 `json:"output"`
}

// Fetch retrieves all text-generation models from the HuggingFace Inference Providers
// API and normalises each live model×provider pair to a ScrapedModel.
// Models or providers with zero prices, non-live status, or wrong pipeline_tag are skipped.
// Returns an error if the HTTP request fails, the response is non-200, JSON decoding
// fails, or no models remain after filtering.
func (s *Scraper) Fetch(ctx context.Context) ([]scraper.ScrapedModel, error) {
	url := s.baseURL + "?" + defaultQuery
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("huggingface: build request: %w", err)
	}
	req.Header.Set("User-Agent", "llm-pricing-api/1.0")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("huggingface: fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		buf := make([]byte, 512)
		n, _ := resp.Body.Read(buf)
		_, _ = io.Copy(io.Discard, resp.Body)
		slog.Warn("huggingface: unexpected status", "status", resp.StatusCode, "body_excerpt", string(buf[:n]))
		return nil, fmt.Errorf("huggingface: unexpected status %d", resp.StatusCode)
	}

	var raw []hfModel
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("huggingface: decode: %w", err)
	}

	fetchedAt := time.Now().UTC()
	models := make([]scraper.ScrapedModel, 0)

	for _, m := range raw {
		// Defensive re-check: API filter should guarantee text-generation, but validate locally.
		if m.PipelineTag != "text-generation" {
			slog.Debug("huggingface: skipping model with unexpected pipeline_tag", "id", m.ID, "tag", m.PipelineTag)
			continue
		}

		for hfProviderID, entry := range m.InferenceProviderMapping {
			if entry.Status != "live" {
				slog.Debug("huggingface: skipping non-live provider", "id", m.ID, "provider", hfProviderID, "status", entry.Status)
				continue
			}

			const epsilon = 1e-12
			if entry.Pricing.Input <= epsilon || entry.Pricing.Output <= epsilon {
				slog.Debug("huggingface: skipping model with zero or missing prices", "id", m.ID, "provider", hfProviderID)
				continue
			}

			normProv := normalizedProvider(hfProviderID)
			slug := normProv + "/" + normalizeModelName(m.ID)

			models = append(models, scraper.ScrapedModel{
				Slug:               slug,
				Provider:           normProv,
				InputCostPerToken:  entry.Pricing.Input,
				OutputCostPerToken: entry.Pricing.Output,
				Modality:           "text",
				SourceName:         "huggingface_inference_providers",
				UnderlyingProvider: hfProviderID,
				FetchedAt:          fetchedAt,
			})
		}
	}

	if len(models) == 0 {
		return nil, fmt.Errorf("huggingface: no models returned after filtering")
	}

	return models, nil
}

// normalizedProvider maps a HuggingFace provider ID to an OpenRouter-compatible prefix.
// Unknown providers pass through unchanged (best-effort mapping).
func normalizedProvider(hfID string) string {
	if v, ok := providerNormMap[hfID]; ok {
		return v
	}
	return hfID
}

// normalizeModelName strips the org prefix and lowercases+normalises the model name
// so it matches the slug convention used elsewhere in the pipeline.
// "meta-llama/Meta-Llama-3-70B-Instruct" → "meta-llama-3-70b-instruct"
func normalizeModelName(hfModelID string) string {
	name := hfModelID
	if i := strings.LastIndexByte(hfModelID, '/'); i >= 0 {
		name = hfModelID[i+1:]
	}
	name = strings.ToLower(name)
	name = strings.NewReplacer("_", "-", ".", "-").Replace(name)
	return name
}
