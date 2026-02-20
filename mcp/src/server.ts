import { Server } from "@modelcontextprotocol/sdk/server/index.js";
import {
  CallToolRequestSchema,
  ListToolsRequestSchema,
  type CallToolResult,
} from "@modelcontextprotocol/sdk/types.js";
import { Transport } from "@modelcontextprotocol/sdk/shared/transport.js";
import { ApiClient } from "./api-client.js";
import * as getCheapestModel from "./tools/get-cheapest-model.js";
import * as compareModels from "./tools/compare-models.js";
import * as getPriceHistory from "./tools/get-price-history.js";
import * as getRecentChanges from "./tools/get-recent-changes.js";
import * as getContextSnapshot from "./tools/get-context-snapshot.js";
import * as subscribeToChanges from "./tools/subscribe-to-changes.js";

/** Shape shared by every tool module exported from ./tools/ */
interface ToolModule {
  definition: {
    name: string;
    description: string;
    inputSchema: {
      type: "object";
      properties: Record<string, unknown>;
      required?: string[];
    };
  };
  handler: (
    args: Record<string, unknown>,
    client: ApiClient
  ) => Promise<CallToolResult>;
}

const TOOLS: ToolModule[] = [
  getCheapestModel,
  compareModels,
  getPriceHistory,
  getRecentChanges,
  getContextSnapshot,
  subscribeToChanges,
];

/**
 * Wraps the low-level MCP Server so we can register tools whose input schemas
 * are plain JSON Schema objects (not Zod schemas).  The high-level McpServer
 * requires Zod, so we drop one level down.
 */
export class LlmRatesServer {
  private readonly server: Server;
  private readonly toolMap: Map<string, ToolModule>;

  constructor(client: ApiClient) {
    this.server = new Server(
      { name: "llmrates", version: "0.1.0" },
      { capabilities: { tools: {} } }
    );

    // Build a lookup map for O(1) dispatch
    this.toolMap = new Map(TOOLS.map((t) => [t.definition.name, t]));

    // Register list-tools handler
    this.server.setRequestHandler(ListToolsRequestSchema, () => ({
      tools: TOOLS.map((t) => ({
        name: t.definition.name,
        description: t.definition.description,
        inputSchema: t.definition.inputSchema,
      })),
    }));

    // Register call-tool handler
    this.server.setRequestHandler(CallToolRequestSchema, async (request) => {
      const toolName = request.params.name;
      const tool = this.toolMap.get(toolName);

      if (!tool) {
        return {
          content: [
            {
              type: "text" as const,
              text: `Unknown tool: ${toolName}`,
            },
          ],
          isError: true,
        };
      }

      const args = (request.params.arguments ?? {}) as Record<string, unknown>;
      return tool.handler(args, client);
    });
  }

  connect(transport: Transport): Promise<void> {
    return this.server.connect(transport);
  }

  close(): Promise<void> {
    return this.server.close();
  }
}

/** Factory kept for backwards compatibility with index.ts */
export function createServer(client: ApiClient): LlmRatesServer {
  return new LlmRatesServer(client);
}
