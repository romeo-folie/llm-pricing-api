import { useState } from "react";

const C = {
  bg:       "#080C14",
  surface:  "#0F1624",
  card:     "#131E2E",
  border:   "#1A2840",
  text:     "#E2E8F0",
  muted:    "#4A6080",
  dim:      "#243044",

  traces:   "#F59E0B",
  metrics:  "#10B981",
  logs:     "#6366F1",

  sdk:      "#7C3AED",
  collector:"#E11D48",
  prom:     "#F97316",
  loki:     "#0891B2",
  tempo:    "#8B5CF6",
  grafana:  "#F97316",
  sentry:   "#E11D48",
  uptime:   "#059669",
};

// ── Signal pill ───────────────────────────────────────────────────────────────
function SignalPill({ type }) {
  const cfg = {
    traces:  { color: C.traces,  label: "TRACES",  icon: "⌥" },
    metrics: { color: C.metrics, label: "METRICS", icon: "◈" },
    logs:    { color: C.logs,    label: "LOGS",    icon: "≡" },
  }[type];
  return (
    <span style={{
      display: "inline-flex", alignItems: "center", gap: 4,
      background: cfg.color + "18",
      border: `1px solid ${cfg.color}50`,
      color: cfg.color,
      padding: "2px 8px", borderRadius: 99,
      fontSize: 9, fontWeight: 800, letterSpacing: 1,
      fontFamily: "'DM Mono', monospace",
    }}>
      <span style={{ fontSize: 10 }}>{cfg.icon}</span>
      {cfg.label}
    </span>
  );
}

// ── Arrow ─────────────────────────────────────────────────────────────────────
function Arrow({ signals = [], label = "", vertical = false, reverse = false }) {
  const colors = signals.map(s => ({ traces: C.traces, metrics: C.metrics, logs: C.logs }[s]));
  const gradient = colors.length === 1
    ? colors[0]
    : `linear-gradient(${vertical ? "180deg" : "90deg"}, ${colors.join(", ")})`;

  return (
    <div style={{
      display: "flex",
      flexDirection: vertical ? "column" : "row",
      alignItems: "center",
      justifyContent: "center",
      gap: 4,
      padding: vertical ? "2px 0" : "0 2px",
      minWidth: vertical ? "auto" : 60,
      minHeight: vertical ? 40 : "auto",
      position: "relative",
    }}>
      {/* Line */}
      <div style={{
        [vertical ? "width" : "height"]: 2,
        [vertical ? "height" : "width"]: vertical ? "100%" : "100%",
        background: gradient,
        borderRadius: 1,
        flex: 1,
        minWidth: vertical ? 2 : 40,
        minHeight: vertical ? 40 : 2,
        position: "relative",
      }}>
        {/* Arrowhead */}
        <div style={{
          position: "absolute",
          ...(vertical
            ? (reverse ? { top: 0, left: "50%", transform: "translateX(-50%) rotate(180deg)" } : { bottom: 0, left: "50%", transform: "translateX(-50%)" })
            : (reverse ? { left: 0, top: "50%", transform: "translateY(-50%) rotate(180deg)" } : { right: 0, top: "50%", transform: "translateY(-50%)" })
          ),
          width: 0, height: 0,
          borderStyle: "solid",
          ...(vertical
            ? { borderWidth: "5px 4px 0 4px", borderColor: `${colors[colors.length - 1]} transparent transparent transparent`, marginBottom: -1 }
            : { borderWidth: "4px 0 4px 6px", borderColor: `transparent transparent transparent ${colors[colors.length - 1]}`, marginRight: -1 }
          ),
        }} />
      </div>
      {label && (
        <div style={{
          fontSize: 8, color: C.muted, fontFamily: "'DM Mono', monospace",
          whiteSpace: "nowrap", letterSpacing: 0.5,
          position: "absolute",
          ...(vertical ? { left: 8, top: "50%", transform: "translateY(-50%)" } : { top: -14 }),
        }}>
          {label}
        </div>
      )}
    </div>
  );
}

// ── Box ───────────────────────────────────────────────────────────────────────
function Box({ title, sub, color, signals = [], children, wide = false, highlight = false }) {
  return (
    <div style={{
      background: highlight ? color + "18" : C.card,
      border: `1.5px solid ${highlight ? color : C.border}`,
      borderRadius: 10,
      padding: "12px 14px",
      minWidth: wide ? 200 : 140,
      maxWidth: wide ? 260 : 190,
      boxShadow: highlight ? `0 0 20px ${color}20` : "none",
      position: "relative",
    }}>
      {highlight && (
        <div style={{
          position: "absolute", top: -1, left: 12, right: 12,
          height: 2, background: color, borderRadius: 1,
        }} />
      )}
      <div style={{ fontSize: 12, fontWeight: 700, color: C.text, fontFamily: "'DM Mono', monospace", marginBottom: 2 }}>
        {title}
      </div>
      {sub && <div style={{ fontSize: 10, color: C.muted, marginBottom: signals.length ? 8 : 0, lineHeight: 1.4 }}>{sub}</div>}
      {signals.length > 0 && (
        <div style={{ display: "flex", gap: 4, flexWrap: "wrap", marginBottom: children ? 8 : 0 }}>
          {signals.map(s => <SignalPill key={s} type={s} />)}
        </div>
      )}
      {children}
    </div>
  );
}

// ── Section label ─────────────────────────────────────────────────────────────
function SectionLabel({ label, color }) {
  return (
    <div style={{
      display: "flex", alignItems: "center", gap: 8,
      marginBottom: 10,
    }}>
      <div style={{ width: 3, height: 16, background: color, borderRadius: 2 }} />
      <span style={{ fontSize: 10, fontWeight: 800, color, fontFamily: "'DM Mono', monospace", letterSpacing: 1.5 }}>
        {label}
      </span>
    </div>
  );
}

// ── Signal legend ─────────────────────────────────────────────────────────────
function Legend() {
  return (
    <div style={{
      display: "flex", gap: 20, flexWrap: "wrap",
      padding: "10px 16px",
      background: C.card, border: `1px solid ${C.border}`, borderRadius: 8,
      marginBottom: 24,
    }}>
      {[
        { type: "traces",  desc: "request spans · distributed trace context · latency attribution" },
        { type: "metrics", desc: "counters · gauges · histograms · scraped by Prometheus" },
        { type: "logs",    desc: "structured JSON log lines · forwarded to Loki" },
      ].map(({ type, desc }) => (
        <div key={type} style={{ display: "flex", alignItems: "center", gap: 8 }}>
          <SignalPill type={type} />
          <span style={{ fontSize: 10, color: C.muted, fontFamily: "'DM Mono', monospace" }}>{desc}</span>
        </div>
      ))}
    </div>
  );
}

// ── Detail panel ──────────────────────────────────────────────────────────────
const DETAILS = {
  "Go / Fiber API": {
    color: C.sdk,
    what: "The primary instrumentation surface.",
    signals: {
      traces:  ["Span per HTTP request (route, method, status, duration)", "DB query spans via otelgorm or pgx hook", "Redis command spans", "Outbound Unkey validation spans", "Outbound Lemon Squeezy call spans", "Propagates W3C trace context on all outbound calls"],
      metrics: ["http_request_duration_seconds (histogram)", "http_requests_total (counter by route/status/tier)", "db_query_duration_seconds", "redis_hit_ratio", "unkey_validation_latency_seconds", "active_sse_connections (gauge)"],
      logs:    ["Structured JSON via zerolog or slog", "Fields: trace_id, span_id, route, tier, model_id, provider, status, latency_ms", "Error logs with stack trace attached to span"],
    },
  },
  "Scraper Workers": {
    color: C.sdk,
    what: "Background goroutines managed by asynq.",
    signals: {
      traces:  ["Span per scraper job execution", "Child spans per source (OpenRouter / LiteLLM / Provider doc)", "Reconciliation span with outcome attribute (confirmed/flagged)", "Span on every DB write"],
      metrics: ["scraper_runs_total (counter by source/status)", "scraper_duration_seconds (histogram)", "reconciliation_confirmed_total", "reconciliation_flagged_total", "review_queue_depth (gauge)", "price_changes_confirmed_total (counter by provider)"],
      logs:    ["Job start/complete/fail with source, duration, records_processed", "Reconciliation outcome: model_id, old_price, new_price, delta_pct, sources_agreed", "Flag events: model_id, discrepancy_pct, sources"],
    },
  },
  "OTel Collector": {
    color: C.collector,
    what: "Central aggregation and routing hub. Deployed as a Railway service.",
    signals: {
      traces:  ["Receives OTLP/gRPC from all services", "Batches and forwards to Grafana Tempo", "Applies tail-based sampling: keep 100% of error traces, 10% of success traces"],
      metrics: ["Receives OTLP metrics from SDK", "Exposes /metrics endpoint for Prometheus scrape", "Aggregates histograms before export"],
      logs:    ["Receives OTLP logs from SDK", "Adds resource attributes: service.name, deployment.env, railway.service", "Forwards to Grafana Loki via loki exporter"],
    },
  },
  "Prometheus": {
    color: C.prom,
    what: "Metrics storage. Scrapes OTel Collector's /metrics endpoint every 15s.",
    signals: {
      metrics: ["Stores all histogram, counter, and gauge series", "Retention: 15 days on Railway", "Scraped by Grafana as a data source", "Alertmanager rules for: p99 > 500ms, error rate > 1%, review queue > 20 items, scraper failure > 2 consecutive"],
    },
  },
  "Grafana Loki": {
    color: C.loki,
    what: "Log aggregation. Receives structured logs from OTel Collector.",
    signals: {
      logs: ["Queryable by label: service, level, trace_id, provider, model_id, tier", "LogQL queries for error patterns", "Correlated with traces via trace_id field → click log line → open trace in Tempo", "Retention: 7 days on free Grafana Cloud tier"],
    },
  },
  "Grafana Tempo": {
    color: C.tempo,
    what: "Distributed trace storage. Receives spans from OTel Collector.",
    signals: {
      traces: ["Full trace visualisation: waterfall of all spans per request", "Service map auto-generated from trace data", "P50/P95/P99 latency per service and operation", "Correlated with logs via trace_id → click span → view logs in Loki"],
    },
  },
  "Grafana": {
    color: C.grafana,
    what: "Dashboard and alerting. Queries Prometheus, Loki, and Tempo.",
    signals: {
      metrics: ["Pipeline health dashboard: scraper status, reconciliation rates, queue depth", "API dashboard: request rates, latency heatmaps, error rates by endpoint and tier", "Business overlay: can embed Lemon Squeezy MRR data via JSON datasource"],
      traces:  ["Service map view from Tempo", "Latency distribution explorer"],
      logs:    ["Log explorer with correlated traces", "Error pattern detection"],
    },
  },
};

function DetailPanel({ selected }) {
  if (!selected || !DETAILS[selected]) return (
    <div style={{
      background: C.card, border: `1px solid ${C.border}`, borderRadius: 10,
      padding: 16, height: "100%",
    }}>
      <div style={{ fontSize: 10, color: C.muted, fontFamily: "'DM Mono', monospace" }}>
        click any node to see<br />what signals it emits<br />or receives
      </div>
    </div>
  );

  const d = DETAILS[selected];
  return (
    <div style={{
      background: C.card, border: `1.5px solid ${d.color}50`, borderRadius: 10, padding: 16,
      overflowY: "auto", maxHeight: 600,
    }}>
      <div style={{ fontSize: 13, fontWeight: 700, color: C.text, fontFamily: "'DM Mono', monospace", marginBottom: 4 }}>{selected}</div>
      <div style={{ fontSize: 10, color: C.muted, marginBottom: 14, lineHeight: 1.5 }}>{d.what}</div>

      {Object.entries(d.signals).map(([type, items]) => (
        <div key={type} style={{ marginBottom: 14 }}>
          <div style={{ marginBottom: 6 }}><SignalPill type={type} /></div>
          {items.map(item => (
            <div key={item} style={{
              fontSize: 10, color: C.muted, lineHeight: 1.6,
              paddingLeft: 12, position: "relative", marginBottom: 2,
              fontFamily: "'DM Mono', monospace",
            }}>
              <span style={{ position: "absolute", left: 3, color: C.dim }}>·</span>
              {item}
            </div>
          ))}
        </div>
      ))}
    </div>
  );
}

// ── Clickable box wrapper ─────────────────────────────────────────────────────
function ClickBox({ title, sub, color, signals, wide, highlight, onClick, selected, children }) {
  return (
    <div
      onClick={() => onClick(title)}
      style={{ cursor: "pointer", transition: "transform 0.1s", transform: selected ? "scale(1.02)" : "scale(1)" }}
    >
      <Box title={title} sub={sub} color={color} signals={signals} wide={wide}
        highlight={highlight || selected} children={children} />
    </div>
  );
}

// ── Main ──────────────────────────────────────────────────────────────────────
export default function OtelDiagram() {
  const [selected, setSelected] = useState(null);
  const toggle = (name) => setSelected(prev => prev === name ? null : name);

  return (
    <div style={{ background: C.bg, minHeight: "100vh", padding: 24, fontFamily: "'DM Sans', sans-serif", color: C.text }}>
      <style>{`
        @import url('https://fonts.googleapis.com/css2?family=DM+Sans:wght@400;500;600;700&family=DM+Mono:wght@400;500;600&display=swap');
        * { box-sizing: border-box; }
      `}</style>

      <div style={{ marginBottom: 20 }}>
        <div style={{ fontSize: 10, fontWeight: 700, color: C.muted, fontFamily: "'DM Mono', monospace", letterSpacing: 1.5, marginBottom: 4 }}>
          LLM TOKEN PRICING PLATFORM
        </div>
        <h1 style={{ fontSize: 22, fontWeight: 700, margin: 0, letterSpacing: "-0.5px" }}>
          Observability Signal Flow
        </h1>
        <div style={{ fontSize: 11, color: C.muted, marginTop: 3 }}>OpenTelemetry · Prometheus · Loki · Tempo · Grafana</div>
      </div>

      <Legend />

      <div style={{ display: "flex", gap: 20, alignItems: "flex-start" }}>

        {/* ── Main flow diagram ── */}
        <div style={{ flex: 1, minWidth: 0 }}>

          {/* Row 1: Instrumented services */}
          <SectionLabel label="INSTRUMENTED SERVICES" color={C.sdk} />
          <div style={{ display: "flex", gap: 12, marginBottom: 0, flexWrap: "wrap" }}>
            <ClickBox
              title="Go / Fiber API"
              sub="otelfiber middleware · otelgorm · otelredis"
              color={C.sdk}
              signals={["traces", "metrics", "logs"]}
              highlight
              onClick={toggle}
              selected={selected === "Go / Fiber API"}
            />
            <ClickBox
              title="Scraper Workers"
              sub="asynq jobs · manual span creation"
              color={C.sdk}
              signals={["traces", "metrics", "logs"]}
              highlight
              onClick={toggle}
              selected={selected === "Scraper Workers"}
            />
            <Box title="Next.js Frontend" sub="Vercel · Sentry only (no OTel)" color={C.muted} signals={[]} />
          </div>

          {/* Arrow down to SDK config */}
          <div style={{ display: "flex", gap: 12, paddingLeft: 0, margin: "6px 0" }}>
            <div style={{ width: 140, display: "flex", justifyContent: "center" }}>
              <Arrow signals={["traces", "metrics", "logs"]} vertical label="OTLP/gRPC" />
            </div>
            <div style={{ width: 140, display: "flex", justifyContent: "center" }}>
              <Arrow signals={["traces", "metrics", "logs"]} vertical />
            </div>
          </div>

          {/* Row 2: OTel Collector */}
          <SectionLabel label="COLLECTION + ROUTING" color={C.collector} />
          <div style={{ marginBottom: 0 }}>
            <ClickBox
              title="OTel Collector"
              sub="Railway service · OTLP receiver · batching · sampling · routing"
              color={C.collector}
              signals={["traces", "metrics", "logs"]}
              wide
              highlight
              onClick={toggle}
              selected={selected === "OTel Collector"}
            >
              <div style={{ marginTop: 10, display: "flex", gap: 6, flexWrap: "wrap" }}>
                {["OTLP receiver", "batch processor", "tail sampler", "resource detector", "loki exporter", "otlp exporter", "/metrics endpoint"].map(t => (
                  <span key={t} style={{
                    fontSize: 9, color: C.muted, background: C.dim,
                    padding: "2px 6px", borderRadius: 4,
                    fontFamily: "'DM Mono', monospace",
                  }}>{t}</span>
                ))}
              </div>
            </ClickBox>
          </div>

          {/* Three arrows out of collector */}
          <div style={{ display: "flex", gap: 0, margin: "6px 0", alignItems: "flex-start" }}>
            {/* Traces branch */}
            <div style={{ flex: 1, display: "flex", flexDirection: "column", alignItems: "center" }}>
              <Arrow signals={["traces"]} vertical label="spans" />
            </div>
            {/* Metrics branch */}
            <div style={{ flex: 1, display: "flex", flexDirection: "column", alignItems: "center" }}>
              <Arrow signals={["metrics"]} vertical label="scrape /metrics" />
            </div>
            {/* Logs branch */}
            <div style={{ flex: 1, display: "flex", flexDirection: "column", alignItems: "center" }}>
              <Arrow signals={["logs"]} vertical label="push via loki exporter" />
            </div>
          </div>

          {/* Row 3: Backends */}
          <SectionLabel label="SIGNAL BACKENDS" color={C.muted} />
          <div style={{ display: "flex", gap: 12, marginBottom: 0 }}>
            <ClickBox
              title="Grafana Tempo"
              sub="trace storage · Railway"
              color={C.tempo}
              signals={["traces"]}
              onClick={toggle}
              selected={selected === "Grafana Tempo"}
            />
            <ClickBox
              title="Prometheus"
              sub="metrics store · Railway · 15d retention"
              color={C.prom}
              signals={["metrics"]}
              onClick={toggle}
              selected={selected === "Prometheus"}
            />
            <ClickBox
              title="Grafana Loki"
              sub="log aggregation · Railway"
              color={C.loki}
              signals={["logs"]}
              onClick={toggle}
              selected={selected === "Grafana Loki"}
            />
          </div>

          {/* Three arrows converging to Grafana */}
          <div style={{ display: "flex", gap: 0, margin: "6px 0" }}>
            <div style={{ flex: 1, display: "flex", justifyContent: "center" }}>
              <Arrow signals={["traces"]} vertical />
            </div>
            <div style={{ flex: 1, display: "flex", justifyContent: "center" }}>
              <Arrow signals={["metrics"]} vertical />
            </div>
            <div style={{ flex: 1, display: "flex", justifyContent: "center" }}>
              <Arrow signals={["logs"]} vertical />
            </div>
          </div>

          {/* Row 4: Grafana */}
          <SectionLabel label="VISUALISATION + ALERTING" color={C.grafana} />
          <ClickBox
            title="Grafana"
            sub="dashboards · alerts · correlations · Grafana Cloud or self-hosted on Railway"
            color={C.grafana}
            signals={["traces", "metrics", "logs"]}
            wide
            highlight
            onClick={toggle}
            selected={selected === "Grafana"}
          >
            <div style={{ marginTop: 10, display: "grid", gridTemplateColumns: "1fr 1fr", gap: 4 }}>
              {[
                "Pipeline health dashboard",
                "API latency heatmap",
                "Reconciliation rates",
                "Review queue depth",
                "SSE connection gauge",
                "Error rate by endpoint",
                "Service map (from Tempo)",
                "Log explorer (correlated)",
              ].map(d => (
                <div key={d} style={{ fontSize: 9, color: C.muted, fontFamily: "'DM Mono', monospace', padding: '2px 0" }}>
                  · {d}
                </div>
              ))}
            </div>
          </ClickBox>

          {/* Arrow to Admin */}
          <div style={{ display: "flex", margin: "6px 0 0 0" }}>
            <div style={{ width: 140, display: "flex", justifyContent: "center" }}>
              <Arrow signals={["metrics", "logs"]} vertical label="embed panels" />
            </div>
          </div>

          {/* Row 5: Admin + Sentry */}
          <SectionLabel label="CONSUMPTION" color={C.consumers} />
          <div style={{ display: "flex", gap: 12, flexWrap: "wrap" }}>
            <Box
              title="Admin Dashboard"
              sub="Grafana panels embedded via iframe · private Vercel deploy"
              color={C.admin}
              signals={["metrics", "logs"]}
            />
            <Box
              title="Sentry"
              sub="error tracking · Go API + Next.js · separate from OTel pipeline"
              color={C.sentry}
              signals={[]}
            >
              <div style={{ marginTop: 6, fontSize: 9, color: C.muted, lineHeight: 1.5 }}>
                captures panics · unhandled errors<br />
                source maps · performance traces<br />
                5k errors/mo free tier
              </div>
            </Box>
            <Box
              title="Alertmanager"
              sub="fires on Prometheus rules"
              color={C.prom}
            >
              <div style={{ marginTop: 6 }}>
                {["p99 > 500ms", "error rate > 1%", "scraper failure > 2x", "review queue > 20", "SSE connections > 80%"].map(r => (
                  <div key={r} style={{ fontSize: 9, color: C.muted, fontFamily: "'DM Mono', monospace", lineHeight: 1.7 }}>→ {r}</div>
                ))}
              </div>
            </Box>
          </div>

          {/* Sampling note */}
          <div style={{
            marginTop: 20, padding: "10px 14px",
            background: C.card, border: `1px solid ${C.border}`, borderRadius: 8,
            fontSize: 10, color: C.muted, fontFamily: "'DM Mono', monospace", lineHeight: 1.8,
          }}>
            <span style={{ color: C.traces, fontWeight: 700 }}>SAMPLING STRATEGY  </span>
            Tail-based sampling in Collector — keep 100% of error traces (status_code = ERROR), 10% of success traces.
            Adjustable once you understand your traffic patterns.
            Metrics and logs are not sampled — all signals forwarded.
          </div>
        </div>

        {/* ── Detail panel ── */}
        <div style={{ width: 260, flexShrink: 0 }}>
          <div style={{ fontSize: 10, color: C.muted, fontFamily: "'DM Mono', monospace", marginBottom: 8 }}>
            signal detail
          </div>
          <DetailPanel selected={selected} />
        </div>
      </div>
    </div>
  );
}
