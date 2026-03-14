"use client";

import { useState } from "react";
import styles from "./widgets.module.css";

const EXAMPLES = [
  {
    label: "Cheapest 128k context model?",
    response: JSON.stringify(
      {
        query: "Cheapest 128k context model?",
        results: [
          {
            model: "gemini-2.5-flash",
            context_window: 1048576,
            input_price: "$0.15/1M",
            output_price: "$0.60/1M",
            confidence: "high",
            sources: ["openrouter", "litellm"],
          },
          {
            model: "gpt-4.1-nano",
            context_window: 1048576,
            input_price: "$0.10/1M",
            output_price: "$0.40/1M",
            confidence: "high",
            sources: ["openrouter", "huggingface"],
          },
        ],
      },
      null,
      2
    ),
  },
  {
    label: "Compare GPT-4o vs Claude Sonnet",
    response: JSON.stringify(
      {
        query: "Compare GPT-4o vs Claude Sonnet",
        comparison: {
          "gpt-4o": {
            input: "$2.50/1M",
            output: "$10.00/1M",
            context: 128000,
            trend: "stable",
          },
          "claude-sonnet-4": {
            input: "$3.00/1M",
            output: "$15.00/1M",
            context: 200000,
            trend: "dropped 6% this month",
          },
        },
        verdict:
          "GPT-4o is 17% cheaper on input, 33% cheaper on output. Claude Sonnet offers 56% more context.",
      },
      null,
      2
    ),
  },
  {
    label: "Models under $1/M input?",
    response: JSON.stringify(
      {
        query: "Models under $1/M input?",
        results: [
          { model: "gpt-4.1-nano", input: "$0.10/1M" },
          { model: "gemini-2.5-flash", input: "$0.15/1M" },
          { model: "llama-4-maverick", input: "$0.20/1M" },
          { model: "deepseek-r1", input: "$0.55/1M" },
        ],
        total_matching: 34,
        showing: 4,
      },
      null,
      2
    ),
  },
];

async function fetchAsk(query: string): Promise<string> {
  try {
    const res = await fetch("/api/ask", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ query }),
    });
    const data = await res.json();
    // If the proxy isn't wired up yet, it returns { simulated: true }
    if (data.simulated) throw new Error("simulated");
    return JSON.stringify(data, null, 2);
  } catch {
    // Fall back to a generic stub if the proxy isn't configured
    return JSON.stringify(
      {
        query,
        results: [
          {
            model: "gemini-2.5-flash",
            input: "$0.15/1M",
            output: "$0.60/1M",
            confidence: "high",
          },
        ],
        note: "Live responses available via API key",
      },
      null,
      2
    );
  }
}

export function AskEndpoint() {
  const [output, setOutput] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [input, setInput] = useState("");

  function runPreset(response: string) {
    setLoading(true);
    setOutput(null);
    setTimeout(() => {
      setOutput(response);
      setLoading(false);
    }, 400);
  }

  async function runLive(query: string) {
    setLoading(true);
    setOutput(null);
    const result = await fetchAsk(query);
    setOutput(result);
    setLoading(false);
  }

  function handleExample(idx: number) {
    runPreset(EXAMPLES[idx].response);
  }

  function handleCustom() {
    const q = input.trim();
    if (!q) return;
    setInput("");
    runLive(q);
  }

  return (
    <div className={styles.card}>
      <div className={styles.askTopbar}>
        <span className={styles.method}>POST</span>
        <span className={styles.endpoint}>/v1/ask</span>
      </div>

      <div className={styles.askChips}>
        {EXAMPLES.map((ex, i) => (
          <button
            key={i}
            className={styles.chip}
            onClick={() => handleExample(i)}
          >
            {ex.label}
          </button>
        ))}
      </div>

      <pre className={styles.codeArea}>
        {loading
          ? "// loading..."
          : output
            ? output
            : "// response will appear here"}
      </pre>

      <div className={styles.askInputRow}>
        <input
          type="text"
          value={input}
          onChange={(e) => setInput(e.target.value)}
          onKeyDown={(e) => e.key === "Enter" && handleCustom()}
          placeholder="Ask a pricing question..."
        />
        <button onClick={handleCustom}>Ask</button>
      </div>
    </div>
  );
}
