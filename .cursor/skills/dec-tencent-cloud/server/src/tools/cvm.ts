import { z } from "zod";
import type { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";

import { createCvmClient, createTatClient, createVpcClient } from "../clients.js";
import {
  buildDownloadScript,
  buildUploadScript,
  describeInvocation,
  runShellCommand
} from "../helpers/tat.js";
import { safeResult, type ToolContext } from "./common.js";

export function registerCvmTools(server: McpServer, ctx: ToolContext) {
  server.registerTool(
    "cvm.describe_instances",
    {
      description: "查询 CVM 实例列表（DescribeInstances）",
      inputSchema: {
        region: z.string().optional().describe("地域，默认 TENCENTCLOUD_REGION"),
        instanceIds: z.array(z.string()).optional().describe("实例 ID 列表"),
        limit: z.number().int().min(1).max(100).optional().describe("返回数量，默认 20"),
        offset: z.number().int().min(0).optional().describe("偏移量，默认 0")
      }
    },
    async ({ region, instanceIds, limit, offset }) => {
      const client = createCvmClient(ctx.getCredentials(), region);
      const params: Record<string, unknown> = {
        Limit: limit ?? 20,
        Offset: offset ?? 0
      };
      if (instanceIds?.length) params.InstanceIds = instanceIds;
      return safeResult(await client.DescribeInstances(params));
    }
  );

  server.registerTool(
    "cvm.describe_security_groups",
    {
      description: "查询安全组列表及规则（VPC DescribeSecurityGroups）",
      inputSchema: {
        region: z.string().optional(),
        securityGroupIds: z.array(z.string()).optional().describe("安全组 ID 列表"),
        limit: z.number().int().min(1).max(100).optional(),
        offset: z.number().int().min(0).optional()
      }
    },
    async ({ region, securityGroupIds, limit, offset }) => {
      const client = createVpcClient(ctx.getCredentials(), region);
      const params: Record<string, unknown> = {
        Limit: limit ?? 20,
        Offset: offset ?? 0
      };
      if (securityGroupIds?.length) params.SecurityGroupIds = securityGroupIds;
      return safeResult(await client.DescribeSecurityGroups(params));
    }
  );

  server.registerTool(
    "cvm.run_command",
    {
      description: "通过 TAT 在云助手 Agent 上执行 Shell 命令（RunCommand）",
      inputSchema: {
        instanceIds: z.array(z.string()).min(1).describe("CVM 实例 ID 列表"),
        command: z.string().describe("Shell 命令（明文，服务端 Base64 编码后发送）"),
        region: z.string().optional(),
        timeout: z.number().int().min(1).max(86400).optional().describe("超时秒数，默认 120")
      }
    },
    async ({ instanceIds, command, region, timeout }) => {
      const client = createTatClient(ctx.getCredentials(), region);
      const result = await runShellCommand(client, instanceIds, command, { timeout });
      return safeResult(result);
    }
  );

  server.registerTool(
    "cvm.upload_file",
    {
      description: "通过 TAT 上传文本/二进制文件到实例（base64 写入）",
      inputSchema: {
        instanceId: z.string().describe("CVM 实例 ID"),
        remotePath: z.string().describe("实例内目标路径，如 /home/ubuntu/app/config.json"),
        content: z.string().describe("文件内容（文本）"),
        encoding: z.enum(["utf8", "base64"]).optional().describe("content 编码，默认 utf8"),
        region: z.string().optional()
      }
    },
    async ({ instanceId, remotePath, content, encoding, region }) => {
      const b64 =
        encoding === "base64" ? content : Buffer.from(content, "utf8").toString("base64");
      const script = buildUploadScript(remotePath, b64);
      const client = createTatClient(ctx.getCredentials(), region);
      const result = await runShellCommand(client, [instanceId], script, {
        commandName: "mcp-upload-file",
        timeout: 300
      });
      return safeResult(result);
    }
  );

  server.registerTool(
    "cvm.download_file",
    {
      description: "通过 TAT 从实例下载文件（base64 返回 invocation 任务输出）",
      inputSchema: {
        instanceId: z.string(),
        remotePath: z.string(),
        region: z.string().optional(),
        waitSeconds: z.number().int().min(0).max(120).optional().describe("等待任务完成秒数，默认 15")
      }
    },
    async ({ instanceId, remotePath, region, waitSeconds }) => {
      const script = buildDownloadScript(remotePath);
      const client = createTatClient(ctx.getCredentials(), region);
      const run = await runShellCommand(client, [instanceId], script, {
        commandName: "mcp-download-file",
        timeout: 120
      });
      const invocationId = (run as { InvocationId?: string }).InvocationId;
      if (!invocationId) {
        return safeResult({ run, note: "未返回 InvocationId，请用 cvm.describe_invocation 查询" });
      }
      const wait = waitSeconds ?? 15;
      if (wait > 0) {
        await new Promise((r) => setTimeout(r, wait * 1000));
      }
      const tasks = await describeInvocation(client, invocationId);
      return safeResult({ invocationId, tasks });
    }
  );

  server.registerTool(
    "cvm.describe_invocation",
    {
      description: "查询 TAT 命令执行任务状态与输出（DescribeInvocationTasks）",
      inputSchema: {
        invocationId: z.string().describe("RunCommand 返回的 InvocationId"),
        region: z.string().optional(),
        limit: z.number().int().min(1).max(100).optional()
      }
    },
    async ({ invocationId, region, limit }) => {
      const client = createTatClient(ctx.getCredentials(), region);
      const result = await describeInvocation(client, invocationId, limit ?? 20);
      return safeResult(result);
    }
  );
}
