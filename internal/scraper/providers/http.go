package providers

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
)

// maxBodyBytes is the maximum number of bytes read from a provider pricing page.
// Real pages are typically <1 MiB; 10 MiB provides a generous safety margin.
const maxBodyBytes = 10 << 20 // 10 MiB

// fetchHTML performs a GET request for the given URL and returns the response
// body as an in-memory reader bounded to maxBodyBytes.
// The underlying HTTP connection is drained and closed before returning, so
// callers do not need to close the returned ReadCloser (but doing so is harmless).
//
// On non-200: logs a warning with a body excerpt and returns an error.
func fetchHTML(ctx context.Context, client *http.Client, url, source string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("%s: build request: %w", source, err)
	}
	req.Header.Set("User-Agent", "llm-pricing-api/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: fetch: %w", source, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		buf := make([]byte, 512)
		n, _ := resp.Body.Read(buf)
		_, _ = io.Copy(io.Discard, resp.Body)
		slog.Warn("provider scraper: unexpected status",
			"source", source,
			"status", resp.StatusCode,
			"body_excerpt", string(buf[:n]),
		)
		return nil, fmt.Errorf("%s: unexpected status %d", source, resp.StatusCode)
	}

	// Read body into memory with a hard cap. This closes the TCP connection early
	// and prevents OOM on unexpectedly large or malicious responses.
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return nil, fmt.Errorf("%s: read body: %w", source, err)
	}

	return io.NopCloser(bytes.NewReader(data)), nil
}
