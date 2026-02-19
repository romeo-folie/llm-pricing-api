import { defineConfig } from "vitest/config"
import path from "path"

export default defineConfig({
  test: {
    environment: "jsdom",
    include: ["__tests__/**/*.test.{ts,tsx}"],
  },
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "."),
      // Stub out server-only so lib/api.ts can be imported in tests
      "server-only": path.resolve(__dirname, "__tests__/__mocks__/server-only.ts"),
    },
  },
})
