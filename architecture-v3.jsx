import { useState } from "react";

// ── Palette ───────────────────────────────────────────────────────────────────
const C = {
  bg:        "#0A0E1A",
  surface:   "#111827",
  border:    "#1E2D40",
  borderLt:  "#2A3F55",
  text:      "#E2E8F0",
  muted:     "#64748B",
  dim:       "#334155",

  // Layer accent colours
  sources:   "#F59E0B",
  ingest:    "#7C3AED",
  storage:   "#0891B2",
  api:       "#6366F1",
  agent:     "#EC4899",
  consumers: "#10B981",
  admin:     "#F97316",
  otel:      "#E11D48",

  // misc
  white: "#FFFFFF",
};

// ── Layer config ──────────────────────────────────────────────────────────────
const LAYERS = [
  { id: "sources",   label: "01 · Data Sources",      color: C.sources,   desc: "External pricing feeds fetched on schedule" },
  { id: "ingest",    label: "02 · Ingestion Pipeline", color: C.ingest,    desc: "Scraping, diffing, reconciliation, review queue" },
  { id: "storage",   label: "03 · Storage",            color: C.storage,   desc: "Persistent data: PostgreSQL + TimescaleDB + Redis" },
  { id: "api",       label: "04 · API Layer",          color: C.api,       desc: "Go/Fiber REST API, auth, rate limiting" },
  { id: "agent",     label: "05 · Agent Interface",    color: C.agent,     desc: "MCP, SSE, /v1/ask, /v1/context, discovery" },
  { id: "consumers", label: "06 · Consumers",          color: C.consumers, desc: "Public-facing frontend, webhooks, direct API" },
  { id: "admin",     label: "07 · Admin Dashboard",    color: C.admin,     desc: "Internal ops dashboard on Vercel" },
  { id: "otel",      label: "08 · Observability",      color: C.otel,      desc: "OTel Collector → Prometheus / Loki / Tempo → Grafana" },
];

// ── Node definitions ──────────────────────────────────────────────────────────
const NODES = {
  // Sources
  openrouter:   { layer: "sources",   label: "OpenRouter",      sub: "/v1/models · 6h",          color: C.sources,   icon: "◈" },
  litellm:      { layer: "sources",   label: "LiteLLM GitHub",  sub: "JSON · daily",             color: C.sources,   icon: "◈" },
  provider_docs:{ layer: "sources",   label: "Provider Docs",   sub: "OpenAI · Anthropic · Google · Mistral · Amazon", color: C.sources, icon: "◈" },

  // Ingest
  scrapers:     { layer: "ingest",    label: "Scraper Workers", sub: "Go goroutines · asynq",    color: C.ingest,    icon: "⚙" },
  diff:         { layer: "ingest",    label: "Diff Engine",     sub: "incoming vs stored",       color: C.ingest,    icon: "⚙" },
  reconciler:   { layer: "ingest",    label: "Reconciler",      sub: ">5% delta → flag · 2x match → publish", color: C.ingest, icon: "⚙", highlight: true },
  review_queue: { layer: "ingest",    label: "Review Queue",    sub: "manual · 4h SLA",          color: C.ingest,    icon: "⚙" },

  // Storage
  postgres:     { layer: "storage",   label: "PostgreSQL",      sub: "+ TimescaleDB",            color: C.storage,   icon: "🗄" },
  redis:        { layer: "storage",   label: "Redis",           sub: "cache · rate limits · queue", color: C.storage, icon: "🗄" },

  // API
  fiber:        { layer: "api",       label: "Go / Fiber",      sub: "REST /v1/ · 9 endpoints",  color: C.api,       icon: "◎", highlight: true },
  unkey:        { layer: "api",       label: "Unkey",           sub: "key mgmt · tier enforcement · rate limiting", color: C.api, icon: "◎" },
  lemon:        { layer: "api",       label: "Lemon Squeezy",   sub: "billing · MoR · webhooks", color: C.api,       icon: "◎" },

  // Agent
  mcp:          { layer: "agent",     label: "MCP Server",      sub: "@llmpricing/mcp · npm",    color: C.agent,     icon: "✦" },
  sse:          { layer: "agent",     label: "SSE Stream",      sub: "/v1/stream/changes",       color: C.agent,     icon: "✦" },
  nlquery:      { layer: "agent",     label: "/v1/ask",         sub: "natural language query",   color: C.agent,     icon: "✦" },
  context:      { layer: "agent",     label: "/v1/context",     sub: "2k token snapshot",        color: C.agent,     icon: "✦" },
  discovery:    { layer: "agent",     label: "Discovery",       sub: "openapi.json · ai-plugin.json · llms.txt", color: C.agent, icon: "✦" },

  // Consumers
  nextjs:       { layer: "consumers", label: "Next.js Frontend",sub: "Vercel · public site",     color: C.consumers, icon: "▣" },
  webhooks:     { layer: "consumers", label: "Webhooks",        sub: "Pro tier · at-least-once", color: C.consumers, icon: "▣" },
  dev_api:      { layer: "consumers", label: "Developer API",   sub: "external REST consumers",  color: C.consumers, icon: "▣" },
  ai_agents:    { layer: "consumers", label: "AI Agents",       sub: "Claude Code · Cursor · custom", color: C.consumers, icon: "▣" },

  // Admin
  admin_app:    { layer: "admin",     label: "Admin App",       sub: "Next.js · Vercel · private", color: C.admin,   icon: "⬡", highlight: true },
  admin_pg:     { layer: "admin",     label: "→ PostgreSQL",    sub: "pipeline stats · price history · review queue", color: C.admin, icon: "⬡" },
  admin_ls:     { layer: "admin",     label: "→ Lemon Squeezy", sub: "subscribers · MRR · churn", color: C.admin,    icon: "⬡" },
  admin_unkey:  { layer: "admin",     label: "→ Unkey",         sub: "key usage · tier counts",  color: C.admin,     icon: "⬡" },
  admin_otel:   { layer: "admin",     label: "→ Grafana",       sub: "metrics · traces · logs",  color: C.admin,     icon: "⬡" },

  // Observability
  otel_sdk:     { layer: "otel",      label: "OTel SDK",        sub: "Go API · workers · Next.js", color: C.otel,    icon: "◉" },
  collector:    { layer: "otel",      label: "OTel Collector",  sub: "Railway service",          color: C.otel,      icon: "◉", highlight: true },
  prometheus:   { layer: "otel",      label: "Prometheus",      sub: "metrics scrape",           color: C.otel,      icon: "◉" },
  loki:         { layer: "otel",      label: "Loki",            sub: "log aggregation",          color: C.otel,      icon: "◉" },
  tempo:        { layer: "otel",      label: "Tempo",           sub: "distributed tracing",      color: C.otel,      icon: "◉" },
  grafana:      { layer: "otel",      label: "Grafana",         sub: "dashboards · alerts",      color: C.otel,      icon: "◉", highlight: true },
};

// ── Edges (from → to) ─────────────────────────────────────────────────────────
const EDGES = [
  // sources → scrapers
  { from: "openrouter",    to: "scrapers",   label: "JSON fetch" },
  { from: "litellm",       to: "scrapers",   label: "JSON fetch" },
  { from: "provider_docs", to: "scrapers",   label: "scrape" },

  // ingest pipeline
  { from: "scrapers",      to: "diff",       label: "raw data" },
  { from: "diff",          to: "reconciler", label: "delta" },
  { from: "reconciler",    to: "review_queue",label: ">5% flag" },
  { from: "reconciler",    to: "postgres",   label: "confirmed record" },

  // storage
  { from: "scrapers",      to: "redis",      label: "job queue" },
  { from: "redis",         to: "scrapers",   label: "cron dispatch" },

  // api reads storage
  { from: "postgres",      to: "fiber",      label: "price data" },
  { from: "redis",         to: "fiber",      label: "cache · rate limit" },
  { from: "unkey",         to: "fiber",      label: "key validation" },
  { from: "lemon",         to: "fiber",      label: "subscription events" },

  // agent layer reads api
  { from: "fiber",         to: "mcp",        label: "" },
  { from: "fiber",         to: "sse",        label: "" },
  { from: "fiber",         to: "nlquery",    label: "" },
  { from: "fiber",         to: "context",    label: "" },
  { from: "fiber",         to: "discovery",  label: "" },

  // consumers
  { from: "fiber",         to: "nextjs",     label: "REST" },
  { from: "fiber",         to: "webhooks",   label: "events" },
  { from: "fiber",         to: "dev_api",    label: "REST" },
  { from: "mcp",           to: "ai_agents",  label: "tools" },
  { from: "sse",           to: "ai_agents",  label: "push" },
  { from: "context",       to: "ai_agents",  label: "snapshot" },

  // admin data sources
  { from: "postgres",      to: "admin_pg",   label: "direct read" },
  { from: "lemon",         to: "admin_ls",   label: "API" },
  { from: "unkey",         to: "admin_unkey",label: "API" },
  { from: "grafana",       to: "admin_otel", label: "embed" },
  { from: "admin_pg",      to: "admin_app",  label: "" },
  { from: "admin_ls",      to: "admin_app",  label: "" },
  { from: "admin_unkey",   to: "admin_app",  label: "" },
  { from: "admin_otel",    to: "admin_app",  label: "" },

  // otel
  { from: "fiber",         to: "otel_sdk",   label: "instrument" },
  { from: "scrapers",      to: "otel_sdk",   label: "instrument" },
  { from: "otel_sdk",      to: "collector",  label: "OTLP/gRPC" },
  { from: "collector",     to: "prometheus", label: "metrics" },
  { from: "collector",     to: "loki",       label: "logs" },
  { from: "collector",     to: "tempo",      label: "traces" },
  { from: "prometheus",    to: "grafana",    label: "" },
  { from: "loki",          to: "grafana",    label: "" },
  { from: "tempo",         to: "grafana",    label: "" },
];

// ── Node component ────────────────────────────────────────────────────────────
function Node({ id, data, selected, onClick }) {
  const isHighlight = data.highlight;
  return (
    <div
      onClick={() => onClick(id)}
      style={{
        background: selected ? data.color + "22" : isHighlight ? data.color + "15" : C.surface,
        border: `1.5px solid ${selected ? data.color : isHighlight ? data.color + "80" : C.border}`,
        borderRadius: 10,
        padding: "10px 14px",
        cursor: "pointer",
        minWidth: 160,
        maxWidth: 200,
        transition: "all 0.15s",
        boxShadow: selected ? `0 0 0 2px ${data.color}40` : isHighlight ? `0 0 12px ${data.color}20` : "none",
        position: "relative",
      }}
    >
      {isHighlight && (
        <div style={{
          position: "absolute", top: -1, left: -1, right: -1,
          height: 2, borderRadius: "10px 10px 0 0",
          background: data.color,
        }} />
      )}
      <div style={{ display: "flex", alignItems: "center", gap: 6, marginBottom: 4 }}>
        <span style={{ fontSize: 12, color: data.color }}>{data.icon}</span>
        <span style={{ fontSize: 12, fontWeight: 700, color: C.text, fontFamily: "'DM Mono', monospace", letterSpacing: "-0.2px" }}>
          {data.label}
        </span>
      </div>
      <div style={{ fontSize: 10, color: C.muted, lineHeight: 1.4, fontFamily: "'DM Mono', monospace" }}>
        {data.sub}
      </div>
    </div>
  );
}

// ── Layer row ─────────────────────────────────────────────────────────────────
function LayerRow({ layer, nodes, selectedNode, onNodeClick, activeLayer, onLayerClick }) {
  const isActive = activeLayer === layer.id || activeLayer === null;
  const layerNodes = Object.entries(nodes).filter(([, n]) => n.layer === layer.id);

  return (
    <div
      style={{
        opacity: isActive ? 1 : 0.35,
        transition: "opacity 0.2s",
        marginBottom: 6,
      }}
    >
      <div style={{
        display: "flex",
        gap: 0,
        background: C.surface,
        border: `1px solid ${C.border}`,
        borderRadius: 12,
        overflow: "hidden",
      }}>
        {/* Layer label */}
        <div
          onClick={() => onLayerClick(layer.id)}
          style={{
            width: 140,
            minWidth: 140,
            background: layer.color + "18",
            borderRight: `1px solid ${layer.color}40`,
            padding: "14px 12px",
            cursor: "pointer",
            display: "flex",
            flexDirection: "column",
            justifyContent: "center",
            gap: 4,
          }}
        >
          <div style={{ fontSize: 10, fontWeight: 800, color: layer.color, fontFamily: "'DM Mono', monospace", letterSpacing: 1 }}>
            {layer.label}
          </div>
          <div style={{ fontSize: 9, color: C.muted, lineHeight: 1.4 }}>{layer.desc}</div>
        </div>

        {/* Nodes */}
        <div style={{
          flex: 1,
          padding: "12px 16px",
          display: "flex",
          flexWrap: "wrap",
          gap: 10,
          alignItems: "center",
        }}>
          {layerNodes.map(([id, data]) => (
            <Node
              key={id}
              id={id}
              data={data}
              selected={selectedNode === id}
              onClick={onNodeClick}
            />
          ))}
        </div>
      </div>
    </div>
  );
}

// ── Connection detail panel ───────────────────────────────────────────────────
function ConnectionPanel({ nodeId, nodeData, edges }) {
  if (!nodeId) return null;

  const incoming = edges.filter(e => e.to === nodeId);
  const outgoing = edges.filter(e => e.from === nodeId);

  const Arrow = ({ edge, dir }) => (
    <div style={{
      display: "flex", alignItems: "center", gap: 8,
      padding: "5px 0",
      borderBottom: `1px solid ${C.border}`,
    }}>
      <span style={{ fontSize: 10, color: dir === "in" ? C.storage : C.api, fontFamily: "'DM Mono', monospace", width: 14 }}>
        {dir === "in" ? "←" : "→"}
      </span>
      <span style={{ fontSize: 11, color: C.text, fontFamily: "'DM Mono', monospace", flex: 1 }}>
        {dir === "in" ? edge.from : edge.to}
      </span>
      {edge.label && (
        <span style={{ fontSize: 10, color: C.muted, fontFamily: "'DM Mono', monospace" }}>{edge.label}</span>
      )}
    </div>
  );

  return (
    <div style={{
      background: C.surface,
      border: `1px solid ${nodeData.color}50`,
      borderRadius: 10,
      padding: 16,
      minWidth: 220,
    }}>
      <div style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: 12 }}>
        <span style={{ fontSize: 14, color: nodeData.color }}>{nodeData.icon}</span>
        <div>
          <div style={{ fontSize: 12, fontWeight: 700, color: C.text, fontFamily: "'DM Mono', monospace" }}>{nodeData.label}</div>
          <div style={{ fontSize: 10, color: C.muted }}>{nodeData.sub}</div>
        </div>
      </div>

      {incoming.length > 0 && (
        <div style={{ marginBottom: 10 }}>
          <div style={{ fontSize: 9, fontWeight: 700, color: C.muted, letterSpacing: 1, marginBottom: 4, fontFamily: "'DM Mono', monospace" }}>RECEIVES FROM</div>
          {incoming.map((e, i) => <Arrow key={i} edge={e} dir="in" />)}
        </div>
      )}
      {outgoing.length > 0 && (
        <div>
          <div style={{ fontSize: 9, fontWeight: 700, color: C.muted, letterSpacing: 1, marginBottom: 4, fontFamily: "'DM Mono', monospace" }}>SENDS TO</div>
          {outgoing.map((e, i) => <Arrow key={i} edge={e} dir="out" />)}
        </div>
      )}
    </div>
  );
}

// ── Admin dashboard panel ─────────────────────────────────────────────────────
function AdminPanel() {
  const sections = [
    {
      title: "Pipeline Health",
      color: C.ingest,
      items: [
        "Last successful scraper run per source",
        "Next scheduled run countdown",
        "Consecutive failure count + last error",
        "Review queue depth + age of oldest item",
        "Reconciliation outcomes: confirmed vs flagged (last 24h)",
      ],
      source: "PostgreSQL · Redis job metadata",
    },
    {
      title: "Price Change Feed",
      color: C.sources,
      items: [
        "Live log of confirmed price changes",
        "Source, old value, new value, delta %",
        "Model + provider attribution",
        "Change velocity trend (7d / 30d)",
      ],
      source: "PostgreSQL price_history table",
    },
    {
      title: "API Metrics",
      color: C.api,
      items: [
        "Request volume per tier (Free/Dev/Pro)",
        "Error rate per endpoint",
        "p99 latency per endpoint",
        "Top 10 consumers by daily usage",
        "SSE active connection count",
        "Keys approaching daily rate limit ceiling",
      ],
      source: "Grafana (Prometheus + OTel) · Unkey API",
    },
    {
      title: "Business Metrics",
      color: C.admin,
      items: [
        "Active subscribers: Free / Developer / Pro",
        "MRR + trend",
        "Recent signups (last 7d)",
        "Churn events (last 30d)",
        "Failed payments",
        "Upgrade / downgrade events",
      ],
      source: "Lemon Squeezy API",
    },
    {
      title: "Key Management",
      color: C.consumers,
      items: [
        "Total active keys per tier",
        "Verification counts vs plan limits",
        "Recently revoked keys",
        "Keys with anomalous usage spikes",
      ],
      source: "Unkey API",
    },
    {
      title: "Infrastructure",
      color: C.otel,
      items: [
        "Railway service health per container",
        "Database connection pool utilisation",
        "Redis memory usage",
        "Grafana embed: system dashboards",
      ],
      source: "Grafana (OTel Collector) · Railway API",
    },
  ];

  return (
    <div>
      <div style={{ fontSize: 11, fontWeight: 700, color: C.admin, fontFamily: "'DM Mono', monospace", letterSpacing: 1, marginBottom: 12 }}>
        ADMIN DASHBOARD · VERCEL · PRIVATE
      </div>
      <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 10 }}>
        {sections.map(s => (
          <div key={s.title} style={{
            background: C.bg,
            border: `1px solid ${s.color}40`,
            borderRadius: 8,
            padding: 12,
          }}>
            <div style={{ display: "flex", alignItems: "center", gap: 6, marginBottom: 8 }}>
              <div style={{ width: 6, height: 6, borderRadius: "50%", background: s.color }} />
              <span style={{ fontSize: 11, fontWeight: 700, color: s.color, fontFamily: "'DM Mono', monospace" }}>{s.title}</span>
            </div>
            {s.items.map(item => (
              <div key={item} style={{ fontSize: 10, color: C.muted, lineHeight: 1.6, paddingLeft: 12, position: "relative" }}>
                <span style={{ position: "absolute", left: 3, color: C.dim }}>·</span>
                {item}
              </div>
            ))}
            <div style={{ marginTop: 8, paddingTop: 6, borderTop: `1px solid ${C.border}`, fontSize: 9, color: C.dim, fontFamily: "'DM Mono', monospace" }}>
              source: {s.source}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

// ── Main ──────────────────────────────────────────────────────────────────────
export default function ArchDiagram() {
  const [selectedNode, setSelectedNode] = useState(null);
  const [activeLayer, setActiveLayer] = useState(null);
  const [tab, setTab] = useState("arch"); // arch | admin

  const handleNodeClick = (id) => setSelectedNode(prev => prev === id ? null : id);
  const handleLayerClick = (id) => setActiveLayer(prev => prev === id ? null : id);

  return (
    <div style={{
      background: C.bg,
      minHeight: "100vh",
      fontFamily: "'DM Sans', sans-serif",
      color: C.text,
      padding: 24,
    }}>
      <style>{`
        @import url('https://fonts.googleapis.com/css2?family=DM+Sans:wght@400;500;600;700&family=DM+Mono:wght@400;500;600&display=swap');
        * { box-sizing: border-box; }
        ::-webkit-scrollbar { width: 4px; height: 4px; }
        ::-webkit-scrollbar-track { background: transparent; }
        ::-webkit-scrollbar-thumb { background: #1E2D40; border-radius: 2px; }
      `}</style>

      {/* Header */}
      <div style={{ marginBottom: 20, display: "flex", alignItems: "flex-start", justifyContent: "space-between", flexWrap: "wrap", gap: 12 }}>
        <div>
          <div style={{ fontSize: 10, fontWeight: 700, color: C.muted, fontFamily: "'DM Mono', monospace", letterSpacing: 1.5, marginBottom: 4 }}>
            LLM TOKEN PRICING PLATFORM
          </div>
          <h1 style={{ fontSize: 22, fontWeight: 700, color: C.text, letterSpacing: "-0.5px", margin: 0 }}>
            System Architecture
          </h1>
          <div style={{ fontSize: 11, color: C.muted, marginTop: 3 }}>v3 · includes admin dashboard + OTel observability</div>
        </div>

        {/* Tabs */}
        <div style={{ display: "flex", gap: 6 }}>
          {[["arch", "Architecture"], ["admin", "Admin Dashboard Plan"]].map(([id, label]) => (
            <button
              key={id}
              onClick={() => setTab(id)}
              style={{
                background: tab === id ? C.api + "30" : "transparent",
                border: `1px solid ${tab === id ? C.api : C.border}`,
                color: tab === id ? C.text : C.muted,
                padding: "6px 14px", borderRadius: 7,
                fontSize: 11, fontWeight: 600, cursor: "pointer",
                fontFamily: "'DM Mono', monospace",
              }}
            >{label}</button>
          ))}
        </div>
      </div>

      {tab === "arch" ? (
        <div style={{ display: "flex", gap: 16, alignItems: "flex-start" }}>
          {/* Main diagram */}
          <div style={{ flex: 1, minWidth: 0 }}>
            {/* Layer filter hint */}
            <div style={{ fontSize: 10, color: C.dim, fontFamily: "'DM Mono', monospace", marginBottom: 10 }}>
              click layer label to isolate · click node to inspect connections
              {activeLayer && (
                <span
                  onClick={() => setActiveLayer(null)}
                  style={{ marginLeft: 12, color: C.api, cursor: "pointer" }}
                >
                  [clear filter]
                </span>
              )}
            </div>

            {LAYERS.map(layer => (
              <LayerRow
                key={layer.id}
                layer={layer}
                nodes={NODES}
                selectedNode={selectedNode}
                onNodeClick={handleNodeClick}
                activeLayer={activeLayer}
                onLayerClick={handleLayerClick}
              />
            ))}

            {/* Legend */}
            <div style={{ display: "flex", gap: 16, flexWrap: "wrap", marginTop: 14, padding: "10px 14px", background: C.surface, border: `1px solid ${C.border}`, borderRadius: 8 }}>
              {LAYERS.map(l => (
                <div key={l.id} style={{ display: "flex", alignItems: "center", gap: 5 }}>
                  <div style={{ width: 8, height: 8, borderRadius: 2, background: l.color }} />
                  <span style={{ fontSize: 9, color: C.muted, fontFamily: "'DM Mono', monospace" }}>{l.label.split(" · ")[1]}</span>
                </div>
              ))}
              <div style={{ display: "flex", alignItems: "center", gap: 5, marginLeft: 8, paddingLeft: 8, borderLeft: `1px solid ${C.border}` }}>
                <div style={{ width: 16, height: 2, background: "linear-gradient(90deg, #6366F1, transparent)" }} />
                <span style={{ fontSize: 9, color: C.muted, fontFamily: "'DM Mono', monospace" }}>top border = key node</span>
              </div>
            </div>
          </div>

          {/* Side panel */}
          <div style={{ width: 240, flexShrink: 0 }}>
            {selectedNode ? (
              <ConnectionPanel
                nodeId={selectedNode}
                nodeData={NODES[selectedNode]}
                edges={EDGES}
              />
            ) : (
              <div style={{
                background: C.surface,
                border: `1px solid ${C.border}`,
                borderRadius: 10,
                padding: 16,
              }}>
                <div style={{ fontSize: 10, color: C.muted, fontFamily: "'DM Mono', monospace", lineHeight: 1.7 }}>
                  <div style={{ fontWeight: 700, color: C.text, marginBottom: 8 }}>Quick stats</div>
                  <div style={{ marginBottom: 6 }}>
                    <span style={{ color: C.sources }}>◈</span> 3 data sources
                  </div>
                  <div style={{ marginBottom: 6 }}>
                    <span style={{ color: C.ingest }}>⚙</span> 4 ingest nodes
                  </div>
                  <div style={{ marginBottom: 6 }}>
                    <span style={{ color: C.storage }}>🗄</span> 2 storage nodes
                  </div>
                  <div style={{ marginBottom: 6 }}>
                    <span style={{ color: C.api }}>◎</span> 3 API nodes
                  </div>
                  <div style={{ marginBottom: 6 }}>
                    <span style={{ color: C.agent }}>✦</span> 5 agent interface nodes
                  </div>
                  <div style={{ marginBottom: 6 }}>
                    <span style={{ color: C.consumers }}>▣</span> 4 consumer nodes
                  </div>
                  <div style={{ marginBottom: 6 }}>
                    <span style={{ color: C.admin }}>⬡</span> 5 admin data sources
                  </div>
                  <div style={{ marginBottom: 12 }}>
                    <span style={{ color: C.otel }}>◉</span> 6 observability nodes
                  </div>
                  <div style={{ borderTop: `1px solid ${C.border}`, paddingTop: 10, fontSize: 9, color: C.dim }}>
                    {EDGES.length} total connections mapped
                  </div>
                </div>
              </div>
            )}
          </div>
        </div>
      ) : (
        <AdminPanel />
      )}
    </div>
  );
}
