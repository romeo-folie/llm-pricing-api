# PRD: Integrate OpenAI, Claude, and Gemini Sources

## 1. Overview and Problem Statement
Currently, `llm-pricing-api` lacks direct integration with the top three LLM providers: OpenAI, Claude (Anthropic), and Google Gemini. Incorporating these providers is critical for a comprehensive pricing API. 
Good news: all three are server-side rendered (SSG/SSR) and can be reliably scraped without a headless browser.

## 2. Target Users and Success Criteria
- **Target Users:** Developers and users of `llm-pricing-api` who need accurate, up-to-date pricing for the top models.
- **Success Criteria:** 
  - Pricing data for OpenAI, Claude, and Gemini is automatically scraped.
  - No headless browser is required (using standard HTTP client + HTML parser).
  - Data correctly normalizes into the existing API format.

## 3. Feature List and Priorities
- **P0:** OpenAI Scraper: Target `https://developers.openai.com/api/docs/pricing?latest-pricing=standard`. Extract standard HTML into markdown pricing tables.
- **P0:** Claude Scraper: Target `https://platform.claude.com/docs/en/about-claude/pricing#third-party-platform-pricing`. Extract HTML/Markdown via GET request.
- **P0:** Gemini Scraper: Target `https://ai.google.dev/gemini-api/docs/pricing`. Scrape initial HTML payload with a standard `User-Agent` to prevent redirect loops.
- **P1:** Data Normalization: Transform scraped markdown/HTML tables into standard database records.
- **P1:** Testing: Create unit/integration tests for the new scrapers.

## 4. Out of Scope
- Headless browser rendering (Puppeteer, Playwright).
- Complex SPA traversal for these three providers.

## 5. Non-Functional Requirements
- **Performance:** Scrapers should execute quickly due to lack of headless browser overhead.
- **Reliability:** Scrapers should handle potential rate limits or transient errors gracefully.

## 6. Open Questions
- What frequency should the scraper run at?
- Are there specific User-Agents that are preferred/blocked by Google Gemini?