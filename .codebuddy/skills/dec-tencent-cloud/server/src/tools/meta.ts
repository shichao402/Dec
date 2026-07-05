import { z } from "zod";
import type { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";

import { listBlockedOperations, listNamespaces, syncDecAssets, syncPkvConfig } from "../lib.js";
import { safeResult, textResult, type ToolContext } from "./common.js";

export function registerMetaTools(server: McpServer, ctx: ToolContext, meta: { name?: string; version?: string }) {
  server.registerTool(
    "meta.list_namespaces",
    {
      description: "列出所有 namespace 及可用 tools",
      inputSchema: {}
    },
    async () => {
      return textResult({
        package: meta.name,
        version: meta.version,
        namespaces: listNamespaces()
      });
    }
  );

  server.registerTool(
    "meta.list_blocked_operations",
    {
      description: "列出已禁用的敏感操作及安全策略说明",
      inputSchema: {}
    },
    async () => textResult(listBlockedOperations())
  );

  server.registerTool(
    "meta.sync_pkv_config",
    {
      description: "从 pkv 同步腾讯云密钥到 .config/mise/conf.d/tencent-cloud.toml",
      inputSchema: {
        folder: z.string().optional().describe("pkv 文件夹名，默认 tencent-cloud")
      }
    },
    async ({ folder }) => {
      const result = syncPkvConfig(ctx.projectRoot, folder ?? "tencent-cloud");
      return textResult(result);
    }
  );

  server.registerTool(
    "meta.sync_dec_assets",
    {
      description: "在项目根目录执行 dec pull",
      inputSchema: {}
    },
    async () => {
      const output = syncDecAssets(ctx.projectRoot);
      return textResult({ projectRoot: ctx.projectRoot, output });
    }
  );
}
