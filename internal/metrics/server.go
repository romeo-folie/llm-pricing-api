package metrics

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Socket timeouts for the internal metrics listener. They are deliberately
// short: the endpoint serves a small text payload to a scraper and nothing else.
const (
	serverReadTimeout  = 5 * time.Second
	serverWriteTimeout = 10 * time.Second
)

// NewMux returns the handler for the internal metrics endpoint.
//
// It serves only /metrics. This is a telemetry surface, not a router, so no
// other path is reachable on the metrics port.
func NewMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	return mux
}

// NewServer returns an HTTP server exposing the default Prometheus registry at
// /metrics on the supplied port, or nil when port is empty so callers can treat
// a disabled endpoint as "nothing to start or shut down".
//
// Both binaries use this. Each needs its own listener because Prometheus
// scrapes per process: the API records HTTP metrics, and the worker records the
// pipeline metrics (scraper runs, reconciler events, webhook deliveries) that
// exist nowhere else.
//
// An empty port disables the listener without disabling instrumentation — the
// counters still increment, nothing scrapes them.
func NewServer(port string) *http.Server {
	if port == "" {
		return nil
	}
	return &http.Server{
		Addr:         ":" + port,
		Handler:      NewMux(),
		ReadTimeout:  serverReadTimeout,
		WriteTimeout: serverWriteTimeout,
	}
}
