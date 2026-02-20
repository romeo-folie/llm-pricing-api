/**
 * Google Analytics 4 utilities.
 *
 * All GA4 interactions flow through this module so event names and parameter
 * shapes stay consistent across the app. Components import the typed helpers
 * rather than calling `gtag()` directly.
 *
 * The gtag snippet is loaded by `components/analytics/GoogleAnalytics.tsx`;
 * this module only contains the data-layer helpers.
 */

export const GA_MEASUREMENT_ID = process.env.NEXT_PUBLIC_GA_MEASUREMENT_ID ?? ""

/* ------------------------------------------------------------------ */
/*  gtag type shim                                                    */
/* ------------------------------------------------------------------ */

type GtagCommand = "config" | "event" | "js" | "set"

declare global {
  interface Window {
    gtag: (command: GtagCommand, targetOrName: string, params?: Record<string, unknown>) => void
    dataLayer: unknown[]
  }
}

/* ------------------------------------------------------------------ */
/*  Core helpers                                                      */
/* ------------------------------------------------------------------ */

/** Send a GA4 page_view event (called on every client-side navigation). */
export function pageview(url: string) {
  if (!GA_MEASUREMENT_ID) return
  window.gtag("config", GA_MEASUREMENT_ID, { page_path: url })
}

/** Generic event dispatcher — prefer the typed wrappers below. */
function event(action: string, params: Record<string, unknown> = {}) {
  if (!GA_MEASUREMENT_ID) return
  window.gtag("event", action, params)
}

/* ------------------------------------------------------------------ */
/*  Typed custom events                                               */
/* ------------------------------------------------------------------ */

/** User compares 2+ models on /compare. */
export function trackCompareModels(modelIds: string[]) {
  event("compare_models", {
    model_count: modelIds.length,
    model_ids: modelIds.join(","),
  })
}

/** User runs a cost calculation on /calculator. */
export function trackCalculateCost(params: {
  modelIds: string[]
  inputTokens: number
  outputTokens: number
  period: string
}) {
  event("calculate_cost", {
    model_count: params.modelIds.length,
    model_ids: params.modelIds.join(","),
    input_tokens: params.inputTokens,
    output_tokens: params.outputTokens,
    period: params.period,
  })
}

/** User opens the model detail modal. */
export function trackViewModelDetail(modelId: string) {
  event("view_model_detail", { model_id: modelId })
}

/** User clicks "Add to Compare" from the model detail modal. */
export function trackAddToCompare(modelId: string) {
  event("add_to_compare", { model_id: modelId })
}

/** User clicks the share button on the compare page. */
export function trackShareComparison(modelIds: string[]) {
  event("share_comparison", {
    model_count: modelIds.length,
    model_ids: modelIds.join(","),
  })
}

/** User clicks a pricing CTA (Developer or Pro checkout). */
export function trackPricingCTA(plan: "developer" | "pro") {
  event("click_pricing_cta", { plan })
}
