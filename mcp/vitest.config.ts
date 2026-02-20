import { defineConfig } from "vitest/config";

export default defineConfig({
  test: {
    testTimeout: 30000,  // 30s — real network calls
    hookTimeout: 30000,
    globals: true,
    environment: "node",
  },
});
