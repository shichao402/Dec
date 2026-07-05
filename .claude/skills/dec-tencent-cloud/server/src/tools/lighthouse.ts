import { z } from "zod";
import type { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";

import { createLighthouseClient, createTatClient } from "../clients.js";
import {
  buildDownloadScript,
  buildUploadScript,
  describeInvocation,
  runShellCommand
} from "../helpers/tat.js";
import { safeResult, type ToolContext } from "./common.js";

export function registerLighthouseTools(server: McpServer, ctx: ToolContext) {
  server.registerTool(
    "lighthouse.describe_instances",
    {
      description: "查询 Lighthouse 实例列表",
      inputSchema: {
        region: z.string().optional(),
        instanceIds: z.array(z.string()).optional(),
        limit: z.number().int().min(1).max(100).optional(),
        offset: z.number().int().min(0).optional()
      }
    },
    async ({ region, instanceIds, limit, offset }) => {
      const client = createLighthouseClient(ctx.getCredentials(), region);
      const params: Record<string, unknown> = {
        Limit: limit ?? 20,
        Offset: offset ?? 0
      };
      if (instanceIds?.length) params.InstanceIds = instanceIds;
      return safeResult(await client.DescribeInstances(params));
    }
  );

  server.registerTool(
    "lighthouse.describe_firewall_rules",
    {
      description: "查询 Lighthouse 实例防火墙规则",
      inputSchema: {
        instanceId: z.string().describe("Lighthouse 实例 ID"),
        region: z.string().optional(),
        limit: z.number().int().min(1).max(100).optional(),
        offset: z.number().int().min(0).optional()
      }
    },
    async ({ instanceId, region, limit, offset }) => {
      const client = createLighthouseClient(ctx.getCredentials(), region);
      return safeResult(
        await client.DescribeFirewallRules({
          InstanceId: instanceId,
          Limit: limit ?? 100,
          Offset: offset ?? 0
        })
      );
    }
  );

  server.registerTool(
    "lighthouse.run_command",
    {
      description: "通过 TAT 在 Lighthouse 上执行 Shell 命令",
      inputSchema: {
        instanceIds: z.array(z.string()).min(1),
        command: z.string(),
        region: z.string().optional(),
        timeout: z.number().int().min(1).max(86400).optional()
      }
    },
    async ({ instanceIds, command, region, timeout }) => {
      const client = createTatClient(ctx.getCredentials(), region);
      const result = await runShellCommand(client, instanceIds, command, { timeout });
      return safeResult(result);
    }
  );

  server.registerTool(
    "lighthouse.upload_file",
    {
      description: "通过 TAT 上传文件到 Lighthouse 实例",
      inputSchema: {
        instanceId: z.string(),
        remotePath: z.string(),
        content: z.string(),
        encoding: z.enum(["utf8", "base64"]).optional(),
        region: z.string().optional()
      }
    },
    async ({ instanceId, remotePath, content, encoding, region }) => {
      const b64 =
        encoding === "base64" ? content : Buffer.from(content, "utf8").toString("base64");
      const script = buildUploadScript(remotePath, b64);
      const client = createTatClient(ctx.getCredentials(), region);
      return safeResult(
        await runShellCommand(client, [instanceId], script, {
          commandName: "mcp-lh-upload",
          timeout: 300
        })
      );
    }
  );

  server.registerTool(
    "lighthouse.download_file",
    {
      description: "通过 TAT 从 Lighthouse 下载文件",
      inputSchema: {
        instanceId: z.string(),
        remotePath: z.string(),
        region: z.string().optional(),
        waitSeconds: z.number().int().min(0).max(120).optional()
      }
    },
    async ({ instanceId, remotePath, region, waitSeconds }) => {
      const script = buildDownloadScript(remotePath);
      const client = createTatClient(ctx.getCredentials(), region);
      const run = await runShellCommand(client, [instanceId], script, {
        commandName: "mcp-lh-download",
        timeout: 120
      });
      const invocationId = (run as { InvocationId?: string }).InvocationId;
      if (!invocationId) {
        return safeResult({ run, note: "请用 lighthouse.describe_invocation 查询" });
      }
      const wait = waitSeconds ?? 15;
      if (wait > 0) await new Promise((r) => setTimeout(r, wait * 1000));
      const tasks = await describeInvocation(client, invocationId);
      return safeResult({ invocationId, tasks });
    }
  );

  server.registerTool(
    "lighthouse.describe_invocation",
    {
      description: "查询 Lighthouse TAT 任务状态与输出",
      inputSchema: {
        invocationId: z.string(),
        region: z.string().optional(),
        limit: z.number().int().min(1).max(100).optional()
      }
    },
    async ({ invocationId, region, limit }) => {
      const client = createTatClient(ctx.getCredentials(), region);
      return safeResult(await describeInvocation(client, invocationId, limit ?? 20));
    }
  );
}
