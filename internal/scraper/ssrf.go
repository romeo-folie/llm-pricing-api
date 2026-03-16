package scraper

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"
)

// NewSSRFSafeTransport returns an http.Transport whose DialContext resolves the
// target hostname once, validates all resolved IPs against the private/loopback
// blocklist, and then connects directly to the first safe IP — eliminating the
// DNS rebinding window that exists when resolution and TCP connect are separate
// operations (SSRF prevention per CLAUDE.md §External Data & Scraper Safety).
//
// Use this transport for the default *http.Client in every scraper that fetches
// from external URLs. Pass a custom client in tests (e.g. httptest.Server) so
// that test traffic to loopback is not blocked by this transport.
func NewSSRFSafeTransport() *http.Transport {
	base := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	return &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, fmt.Errorf("scraper: split addr %q: %w", addr, err)
			}
			resolved, err := net.DefaultResolver.LookupHost(ctx, host)
			if err != nil {
				return nil, fmt.Errorf("scraper: resolve %q: %w", host, err)
			}
			if err := CheckIPs(resolved); err != nil {
				return nil, err
			}
			// Connect directly to the first resolved IP to prevent re-resolution
			// (and the associated DNS rebinding window) inside the dialer.
			return base.DialContext(ctx, network, net.JoinHostPort(resolved[0], port))
		},
		// ResponseHeaderTimeout caps the time waiting for the server to send
		// response headers after the request body is sent. Without this, a
		// server that accepts the connection but stalls before sending headers
		// (or stalls mid-body on a large streaming response) can hang a worker
		// slot indefinitely even when http.Client.Timeout is set — because
		// http.Client.Timeout only fires if the entire round-trip hasn't
		// completed within the deadline, and some runtimes reset that clock
		// per read. 45s leaves headroom under a 60s client timeout.
		ResponseHeaderTimeout: 45 * time.Second,
		// TLSHandshakeTimeout bounds the TLS negotiation phase separately.
		TLSHandshakeTimeout: 15 * time.Second,
		// IdleConnTimeout recycles pooled connections that have been idle
		// longer than 90s to avoid stale-connection errors on long-running
		// workers that make infrequent scrape calls.
		IdleConnTimeout: 90 * time.Second,
	}
}

// CheckIPs returns an error if any address in addrs is a private, loopback,
// link-local, multicast, or unspecified IP.  It is the inner check shared by
// both CheckRedirectHost and the DialContext inside NewSSRFSafeTransport.
// Addresses that net.ParseIP cannot parse are treated as blocked (fail-closed).
func CheckIPs(addrs []string) error {
	for _, a := range addrs {
		ip := net.ParseIP(a)
		if ip == nil {
			// Fail-closed: an unparseable address is not provably safe.
			return fmt.Errorf("scraper: unparseable address %q blocked (SSRF prevention)", a)
		}
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
			ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
			return fmt.Errorf("scraper: address %s blocked (SSRF prevention)", ip)
		}
	}
	return nil
}

// CheckRedirectHost blocks redirects to private IP ranges and loopback.
// Returns an error (blocking the redirect) if the host resolves to a
// private or loopback address. Pass this as the CheckRedirect hook on the
// *http.Client alongside NewSSRFSafeTransport for defense-in-depth coverage.
func CheckRedirectHost(ctx context.Context, host string) error {
	// Strip port if present.
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	addrs, err := net.DefaultResolver.LookupHost(ctx, host)
	if err != nil {
		// If we can't resolve it, block it (fail-closed).
		return fmt.Errorf("scraper: redirect host %q unresolvable: %w", host, err)
	}
	return CheckIPs(addrs)
}
