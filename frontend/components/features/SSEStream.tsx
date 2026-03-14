"use client";

import { useState, useEffect, useRef, useCallback } from "react";
import styles from "./widgets.module.css";

interface StreamEvent {
  id: number;
  model: string;
  oldPrice: string;
  newPrice: string;
  direction: "up" | "down";
  source: string;
  confidence: string;
  age: number;
}

const MODELS = [
  { name: "gpt-4o", prices: ["$2.50", "$2.40", "$2.60"] },
  { name: "claude-sonnet-4", prices: ["$3.00", "$2.80", "$3.20"] },
  { name: "gemini-2.5-flash", prices: ["$0.15", "$0.12", "$0.18"] },
  { name: "llama-4-maverick", prices: ["$0.20", "$0.18", "$0.22"] },
  { name: "deepseek-r1", prices: ["$0.55", "$0.50", "$0.60"] },
  { name: "mistral-large", prices: ["$2.00", "$1.80", "$2.10"] },
  { name: "gpt-4.1-nano", prices: ["$0.10", "$0.08", "$0.12"] },
  { name: "command-r-plus", prices: ["$2.50", "$2.30", "$2.70"] },
];

const SOURCES = ["openrouter", "litellm", "huggingface"];
const CONFS = ["high", "high", "medium", "high"];

function generateEvent(id: number): StreamEvent {
  const m = MODELS[Math.floor(Math.random() * MODELS.length)];
  const pi = Math.floor(Math.random() * m.prices.length);
  const ni = (pi + 1) % m.prices.length;
  const oldVal = parseFloat(m.prices[pi].replace("$", ""));
  const newVal = parseFloat(m.prices[ni].replace("$", ""));

  return {
    id,
    model: m.name,
    oldPrice: m.prices[pi],
    newPrice: m.prices[ni],
    direction: newVal < oldVal ? "down" : "up",
    source: SOURCES[Math.floor(Math.random() * SOURCES.length)],
    confidence: CONFS[Math.floor(Math.random() * CONFS.length)],
    age: Math.floor(Math.random() * 55 + 5),
  };
}

export function SSEStream() {
  const [connected, setConnected] = useState(true);
  const [events, setEvents] = useState<StreamEvent[]>([]);
  const eventId = useRef(1000);
  const timerRef = useRef<NodeJS.Timeout | null>(null);
  const lastDisconnectId = useRef<number | null>(null);

  const addEvent = useCallback(() => {
    eventId.current += 1;
    const ev = generateEvent(eventId.current);
    setEvents((prev) => [ev, ...prev].slice(0, 20));
  }, []);

  const startStream = useCallback(() => {
    addEvent();
    timerRef.current = setInterval(
      () => addEvent(),
      2200 + Math.random() * 1800
    );
  }, [addEvent]);

  const stopStream = useCallback(() => {
    if (timerRef.current) {
      clearInterval(timerRef.current);
      timerRef.current = null;
    }
  }, []);

  useEffect(() => {
    // seed 3 initial events (startStream adds a 4th immediately after)
    const seed: StreamEvent[] = [];
    for (let i = 0; i < 3; i++) {
      eventId.current += 1;
      seed.push(generateEvent(eventId.current));
    }
    setEvents(seed);
    startStream();
    return () => stopStream();
  }, [startStream, stopStream]);

  function toggleConnection() {
    if (connected) {
      stopStream();
      lastDisconnectId.current = eventId.current;
      setConnected(false);
    } else {
      setConnected(true);
      // simulate catching up on missed events
      const missed = Math.floor(Math.random() * 3) + 1;
      for (let i = 0; i < missed; i++) addEvent();
      startStream();
      lastDisconnectId.current = null;
    }
  }

  function clearLog() {
    setEvents([]);
  }

  return (
    <div className={styles.card}>
      <div className={styles.streamHeader}>
        <span className={styles.endpoint}>GET /v1/stream/changes</span>
        <span className={styles.status}>
          <span
            className={`${styles.statusDot} ${!connected ? styles.off : ""}`}
          />
          {connected ? "connected" : "disconnected"}
        </span>
      </div>

      <div className={styles.streamLog}>
        {events.map((ev) => (
          <div key={ev.id} className={styles.ev}>
            <span className={styles.meta}>id:{ev.id}</span>{" "}
            <span className={styles.model}>{ev.model}</span>{" "}
            <span className={ev.direction === "down" ? styles.down : styles.up}>
              {ev.direction === "down" ? "↓" : "↑"} {ev.oldPrice} →{" "}
              {ev.newPrice}/1M in
            </span>{" "}
            <span className={styles.meta}>
              {ev.source} · conf:{ev.confidence} · {ev.age}s ago
            </span>
          </div>
        ))}
      </div>

      {!connected && lastDisconnectId.current && (
        <div className={styles.reconnectNote}>
          Reconnecting from last-event-id: {lastDisconnectId.current}
        </div>
      )}

      <div className={styles.streamControls}>
        <button onClick={toggleConnection}>
          {connected ? "Disconnect" : "Reconnect"}
        </button>
        <button onClick={clearLog}>Clear</button>
      </div>
    </div>
  );
}
