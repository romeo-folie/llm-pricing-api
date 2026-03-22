"use client";

import { useEffect, useRef } from "react";
import { providerStyle } from "@/lib/provider-colors";
import { SiteBadge } from "@/components/ui/SiteBadge";
import styles from "./PricingTicker.module.css";

interface TickerModel {
  provider: string;
  name: string;
  input: number;
  output: number;
}

// Illustrative sample prices ($/M tokens). These are static placeholders used
// only in the hero ticker animation. The actual live pricing data is served by
// the /v1/models and /v1/history API endpoints. Update periodically to stay
// roughly current, but exact accuracy is not required — users click through to
// the live data.
const MODELS: TickerModel[] = [
  { provider: "OpenAI",    name: "gpt-4o",               input: 2.50,  output: 10.00 },
  { provider: "Anthropic", name: "claude-3-5-sonnet",    input: 3.00,  output: 15.00 },
  { provider: "Google",    name: "gemini-1.5-pro",        input: 1.25,  output: 5.00  },
  { provider: "OpenAI",    name: "gpt-4o-mini",           input: 0.15,  output: 0.60  },
  { provider: "Anthropic", name: "claude-3-haiku",        input: 0.25,  output: 1.25  },
  { provider: "Google",    name: "gemini-1.5-flash",      input: 0.075, output: 0.30  },
  { provider: "Mistral",   name: "mistral-large",         input: 2.00,  output: 6.00  },
  { provider: "Cohere",    name: "command-r+",            input: 2.50,  output: 10.00 },
  { provider: "DeepSeek",  name: "deepseek-r1",           input: 0.55,  output: 2.19  },
  { provider: "Meta",      name: "llama-3.3-70b",         input: 0.20,  output: 0.60  },
];

function fmt(n: number): string {
  return `$${n < 0.1 ? n.toFixed(3) : n.toFixed(2)}`;
}

export function PricingTicker() {
  const trackRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const id = setInterval(() => {
      const track = trackRef.current;
      if (!track) return;
      const prices = track.querySelectorAll("[data-price]");
      if (!prices.length) return;
      const el = prices[Math.floor(Math.random() * Math.min(prices.length, MODELS.length))] as HTMLElement;
      el.classList.remove(styles.flash);
      void el.offsetWidth;
      el.classList.add(styles.flash);
    }, 2200);
    return () => clearInterval(id);
  }, []);

  const items = [...MODELS, ...MODELS];

  return (
    <div className={styles.wrapper}>
      <div className={styles.track} ref={trackRef}>
        {items.map((m, i) => (
          <div className={styles.item} key={`${m.name}-${i}`}>
            <SiteBadge
              label={m.provider}
              {...providerStyle(m.provider)}
              className="mr-2.5"
            />
            <span className={styles.modelName}>{m.name}</span>
            <span className={styles.price} data-price>
              in {fmt(m.input)}/M · out {fmt(m.output)}/M
            </span>
          </div>
        ))}
      </div>
    </div>
  );
}
