import type { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import type { TencentCredentials } from "../lib.js";
import { redactSecrets, resolveCredentials, resolveProjectRoot } from "../lib.js";

export function textResult(payload: unknown) {
  return {
    content: [{ type: "text" as const, text: JSON.stringify(payload, null, 2) }]
  };
}

export type ToolContext = {
  getCredentials: () => TencentCredentials;
  projectRoot: string;
};

export function createToolContext(): ToolContext {
  const projectRoot = resolveProjectRoot();
  return {
    projectRoot,
    getCredentials: () => resolveCredentials(projectRoot)
  };
}

export function safeResult(payload: unknown) {
  return textResult(redactSecrets(payload));
}

export type RegisterFn = (server: McpServer, ctx: ToolContext) => void;
