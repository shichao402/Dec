import type { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import type { TencentCredentials } from "../lib.js";
export declare function textResult(payload: unknown): {
    content: {
        type: "text";
        text: string;
    }[];
};
export type ToolContext = {
    getCredentials: () => TencentCredentials;
    projectRoot: string;
};
export declare function createToolContext(): ToolContext;
export declare function safeResult(payload: unknown): {
    content: {
        type: "text";
        text: string;
    }[];
};
export type RegisterFn = (server: McpServer, ctx: ToolContext) => void;
