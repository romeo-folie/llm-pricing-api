"use client"

import { useCallback, useEffect, useRef, useState } from "react"

/**
 * Manages a cooldown countdown with an adaptive tick rate.
 * When remaining time > 60s, ticks every 30s to avoid unnecessary re-renders.
 * When remaining time <= 60s, ticks every 1s for a smooth countdown display.
 */
export function useCooldown() {
  const [cooldownMs, setCooldownMs] = useState(0)
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  const clearTimer = () => {
    if (timerRef.current) {
      clearTimeout(timerRef.current)
      timerRef.current = null
    }
  }

  useEffect(() => {
    return () => clearTimer()
  }, [])

  const startCooldown = useCallback((ms: number) => {
    const safeMs = Number.isFinite(ms) ? Math.max(0, ms) : 0
    if (safeMs === 0) return

    clearTimer()
    const endsAt = Date.now() + safeMs
    setCooldownMs(safeMs)

    const tick = () => {
      const remaining = endsAt - Date.now()
      if (remaining <= 0) {
        setCooldownMs(0)
        timerRef.current = null
        return
      }
      setCooldownMs(remaining)
      const nextTick = remaining > 60_000 ? 30_000 : 1_000
      timerRef.current = setTimeout(tick, nextTick)
    }

    const firstTick = safeMs > 60_000 ? 30_000 : 1_000
    timerRef.current = setTimeout(tick, firstTick)
  }, [])

  return { cooldownMs, startCooldown }
}
