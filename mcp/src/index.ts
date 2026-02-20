#!/usr/bin/env node

import { createServer as createHttpServer } from "http";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { ApiClient } from "./api-client.js";
import { createServer } from "./server.js";

const DEFAULT_BASE_URL = "https://api.llmrates.com";

async function main(): Promise<void> {
  const args = process.argv.slice(2);
  const transportArg =
    args.find((a) => a.startsWith("--transport="))?.split("=")[1] ?? "stdio";
  const portArg =
    args.find((a) => a.startsWith("--port="))?.split("=")[1] ?? "3001";
  const port = parseInt(portArg, 10);
  if (transportArg === "http" && (isNaN(port) || port < 1 || port > 65535)) {
    console.error(`Invalid port: "${portArg}". Must be an integer between 1 and 65535.`);
    process.exit(1);
  }

  const apiKey = process.env.LLMRATES_API_KEY;
  if (!apiKey) {
    // Log to stderr so the MCP host sees the message without polluting stdout
    console.error(
      "Error: LLMRATES_API_KEY is not set. " +
        "Add it to your MCP configuration's env block."
    );
    process.exit(1);
  }

  const baseUrl = process.env.LLMRATES_API_URL ?? DEFAULT_BASE_URL;
  const client = new ApiClient(baseUrl, apiKey);
  const server = createServer(client);

  if (transportArg === "http") {
    try {
      const { StreamableHTTPServerTransport } = await import(
        "@modelcontextprotocol/sdk/server/streamableHttp.js"
      );
      // Stateless mode: no session management needed for a public API proxy.
      // StreamableHTTPServerTransport is a request handler, not a standalone listener —
      // we create a Node.js HTTP server and wire requests to transport.handleRequest.
      const transport = new StreamableHTTPServerTransport({
        sessionIdGenerator: undefined,
      });
      await server.connect(transport);
      const httpServer = createHttpServer((req, res) => {
        transport.handleRequest(req, res).catch((err) => {
          console.error("HTTP request error:", err instanceof Error ? err.message : err);
          if (!res.headersSent) res.writeHead(500).end();
        });
      });
      httpServer.listen(port, () => {
        console.error(`llmrates MCP server listening on HTTP port ${port}`);
      });
    } catch {
      console.error(
        "HTTP transport is not available in this SDK version. " +
          "Use --transport=stdio (the default)."
      );
      process.exit(1);
    }
  } else {
    const transport = new StdioServerTransport();
    await server.connect(transport);
  }
}

main().catch((err) => {
  console.error("Fatal error:", err instanceof Error ? err.message : err);
  process.exit(1);
});
