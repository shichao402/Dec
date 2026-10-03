import type { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { type ToolContext } from "./common.js";
export declare function registerMetaTools(server: McpServer, ctx: ToolContext, meta: {
    name?: string;
    version?: string;
}): void;
