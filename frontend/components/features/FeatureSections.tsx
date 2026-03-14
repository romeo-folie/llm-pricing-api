"use client";

import { ContextInjection } from "./ContextInjection";
import { SSEStream } from "./SSEStream";
import { AskEndpoint } from "./AskEndpoint";
import styles from "./features.module.css";

const sections = [
  {
    endpoint: "/v1/context",
    label: "Agent-optimized",
    title: "Context injection for system prompts",
    description:
      "Your agent's system prompt stays lean. One endpoint returns a ~2k token pricing snapshot — structured, current, and small enough to inject without blowing your context window.",
    hint: "Toggle to compare with vs. without",
    component: <ContextInjection />,
    reverse: false,
  },
  {
    endpoint: "/v1/stream/changes",
    label: "Real-time",
    title: "Live price change stream",
    description:
      "SSE endpoint with replay semantics. Every price delta surfaces within 60 seconds — with model, old/new price, source, and confidence metadata. Disconnect and reconnect without missing events.",
    hint: "Try disconnecting and reconnecting",
    component: <SSEStream />,
    reverse: true,
  },
  {
    endpoint: "POST /v1/ask",
    label: "Natural language",
    title: "Ask pricing questions in plain English",
    description:
      "No query syntax to learn. Post a natural language question, get back structured JSON with model recommendations, pricing breakdowns, and source attribution. Built for agent tool-use flows.",
    hint: "Click an example or type your own",
    component: <AskEndpoint />,
    reverse: false,
  },
];

export function FeatureSections() {
  return (
    <section className={styles.wrapper}>
      <div className={styles.sectionHeader}>
        <span className={styles.tag}>[ FEATURE HIGHLIGHTS ]</span>
        <h2 id="feature-highlights-heading" className={styles.heading}>
          Feature highlights for agents and developers.
        </h2>
        <p className={styles.subheading}>
          Everything is available through one unified API surface.
        </p>
      </div>

      {sections.map((s, i) => (
        <div
          key={i}
          className={`${styles.split} ${s.reverse ? styles.reverse : ""}`}
        >
          <div className={styles.copy}>
            <span className={styles.pill}>{s.endpoint}</span>
            <span className={styles.label}>{s.label}</span>
            <h3 className={styles.title}>{s.title}</h3>
            <p className={styles.desc}>{s.description}</p>
            <div className={styles.hint}>
              <span className={styles.hintDot} />
              {s.hint}
            </div>
          </div>
          <div className={styles.visual}>{s.component}</div>
        </div>
      ))}
    </section>
  );
}
