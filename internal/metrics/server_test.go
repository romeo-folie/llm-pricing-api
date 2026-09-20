package metrics

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestNewServerDisabled verifies that an empty port yields no server, which is
// how callers implement the documented "METRICS_PORT empty disables the
// endpoint" behaviour.
func TestNewServerDisabled(t *testing.T) {
	if srv := NewServer(""); srv != nil {
		t.Fatalf("NewServer(%q) = %v, want nil", "", srv)
	}
}

// TestNewServerEnabled verifies the server binds the requested port, carries a
// handler, and applies bounded socket timeouts.
func TestNewServerEnabled(t *testing.T) {
	srv := NewServer("9099")
	if srv == nil {
		t.Fatal(`NewServer("9099") = nil, want a server`)
	}
	t.Cleanup(func() { _ = srv.Close() })

	if srv.Addr != ":9099" {
		t.Errorf("Addr = %q, want %q", srv.Addr, ":9099")
	}
	if srv.Handler == nil {
		t.Error("Handler = nil, want the metrics mux")
	}
	if srv.ReadTimeout <= 0 || srv.WriteTimeout <= 0 {
		t.Errorf("timeouts must be positive, got read=%v write=%v", srv.ReadTimeout, srv.WriteTimeout)
	}
}

// TestMuxServesPipelineMetrics is the regression test for the gap this change
// closes. The worker increments its pipeline counters into the process-local
// default registry; before this endpoint existed nothing read them, so the
// data-pipeline dashboard stayed empty and the scraper-failure alert could not
// fire.
//
// A CounterVec only appears in the exposition once it has an observed child, so
// seed one sample per family. The assertions match full sample lines rather
// than bare family names, which also pins the label names the dashboards and
// alert rules depend on.
func TestMuxServesPipelineMetrics(t *testing.T) {
	ScraperRunsTotal.WithLabelValues("openrouter", "success").Inc()
	ReconcilerEventsTotal.WithLabelValues("price_published").Inc()
	WebhookDeliveriesTotal.WithLabelValues("success").Inc()

	rec := httptest.NewRecorder()
	NewMux().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	body := rec.Body.String()
	for _, sample := range []string{
		`llm_scraper_runs_total{source="openrouter",status="success"}`,
		`llm_reconciler_events_total{event_type="price_published"}`,
		`llm_webhook_deliveries_total{status="success"}`,
	} {
		if !strings.Contains(body, sample) {
			t.Errorf("/metrics response is missing sample %q", sample)
		}
	}
}

// TestMuxRejectsUnknownPath guards the handler surface: the metrics server
// exists only to serve /metrics and must not become a general router.
func TestMuxRejectsUnknownPath(t *testing.T) {
	rec := httptest.NewRecorder()
	NewMux().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusNotFound {
		t.Errorf("status for / = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

// TestServerServesOverHTTP exercises the full path a scraper takes — real
// listener, real HTTP request, real exposition — rather than the mux in
// isolation. It binds port 0 so it never collides with a running service.
func TestServerServesOverHTTP(t *testing.T) {
	ScraperRunsTotal.WithLabelValues("openrouter", "success").Inc()
	ReconcilerEventsTotal.WithLabelValues("price_published").Inc()
	WebhookDeliveriesTotal.WithLabelValues("success").Inc()

	srv := NewServer("0")
	if srv == nil {
		t.Fatal(`NewServer("0") = nil, want a server`)
	}

	// Bind an explicit loopback address rather than srv.Addr (":0"): a listener
	// on the unspecified address reports "[::]:port", which is not dialable on
	// every platform.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })

	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://" + ln.Addr().String() + "/metrics")
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	for _, sample := range []string{
		`llm_scraper_runs_total{source="openrouter",status="success"}`,
		`llm_reconciler_events_total{event_type="price_published"}`,
		`llm_webhook_deliveries_total{status="success"}`,
	} {
		if !strings.Contains(string(body), sample) {
			t.Errorf("served /metrics is missing sample %q", sample)
		}
	}
}
