"use client";

import { useState } from "react";
import styles from "./widgets.module.css";

const withLLMRates = `// system prompt
You are a cost-optimization agent.

{{pricing_context}}  ← injected from /v1/context

// resolves to:
LLM_PRICES (as of 2026-03-14T09:41Z)
gpt-4o          $2.50/1M in  $10.00/1M out  conf:high
claude-sonnet-4  $3.00/1M in  $15.00/1M out  conf:high
gemini-2.0-pro  $1.25/1M in   $5.00/1M out  conf:med
llama-4-maverick $0.20/1M in   $0.60/1M out  conf:high
... 118 more models`;

const withoutLLMRates = `// system prompt — manually maintained
You are a cost-optimization agent.

Here are the current LLM prices:
- GPT-4o: $2.50 per 1M input tokens  ← is this still right?
- Claude Sonnet: $3.00 per 1M input   ← which version?
- Gemini Pro:                          ← what's the current price?
- Llama:                               ← which provider? what tier?

// Problems:
✗ Stale within days
✗ No source attribution
✗ No confidence scores
✗ Manual updates = human bottleneck
✗ Grows unbounded as models added`;

export function ContextInjection() {
  const [tab, setTab] = useState<"with" | "without">("with");

  return (
    <div className={styles.card}>
      <div className={styles.toggleRow}>
        <button
          className={`${styles.toggleBtn} ${tab === "with" ? styles.active : ""}`}
          onClick={() => setTab("with")}
        >
          With LLMRates
        </button>
        <button
          className={`${styles.toggleBtn} ${tab === "without" ? styles.active : ""}`}
          onClick={() => setTab("without")}
        >
          Without LLMRates
        </button>
      </div>

      <pre className={styles.codeArea}>
        {tab === "with" ? withLLMRates : withoutLLMRates}
      </pre>

      <div className={styles.tokenBadge}>
        {tab === "with" ? (
          <>
            <span>~2,100 tokens</span>
            <span className={styles.good}>120+ models covered</span>
          </>
        ) : (
          <>
            <span>~800 tokens</span>
            <span className={styles.bad}>4 models, already stale</span>
          </>
        )}
      </div>
    </div>
  );
}
