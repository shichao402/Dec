import type { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";

import { createToolContext } from "./tools/common.js";
import { registerCdbTools } from "./tools/cdb.js";
import { registerCosTools } from "./tools/cos.js";
import { registerCvmTools } from "./tools/cvm.js";
import { registerDnsTools } from "./tools/dns.js";
import { registerLighthouseTools } from "./tools/lighthouse.js";
import { registerMetaTools } from "./tools/meta.js";

export function registerAllTools(
  server: McpServer,
  meta: { name?: string; version?: string }
) {
  const ctx = createToolContext();
  registerMetaTools(server, ctx, meta);
  registerCvmTools(server, ctx);
  registerLighthouseTools(server, ctx);
  registerCosTools(server, ctx);
  registerDnsTools(server, ctx);
  registerCdbTools(server, ctx);
}
