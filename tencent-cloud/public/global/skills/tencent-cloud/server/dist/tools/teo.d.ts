import type { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { type ToolContext } from "./common.js";
/**
 * EdgeOne site-side APIs. Unlike pages_*, these are standard TC3-signed calls
 * and reuse TENCENTCLOUD_SECRET_ID/KEY, so no Pages API Token is needed.
 */
export declare function registerTeoTools(server: McpServer, ctx: ToolContext): void;
