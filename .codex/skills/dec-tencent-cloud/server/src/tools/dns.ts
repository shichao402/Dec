import { z } from "zod";
import type { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";

import { createDnspodClient } from "../clients.js";
import { rejectBlocked } from "../sensitive.js";
import { safeResult, type ToolContext } from "./common.js";

export function registerDnsTools(server: McpServer, ctx: ToolContext) {
  server.registerTool(
    "dns.describe_domains",
    {
      description: "查询 DNSPod 域名列表（DescribeDomainList）",
      inputSchema: {
        keyword: z.string().optional().describe("域名关键字过滤"),
        limit: z.number().int().min(1).max(3000).optional(),
        offset: z.number().int().min(0).optional()
      }
    },
    async ({ keyword, limit, offset }) => {
      const client = createDnspodClient(ctx.getCredentials());
      const params: Record<string, unknown> = {
        Limit: limit ?? 20,
        Offset: offset ?? 0
      };
      if (keyword) params.Keyword = keyword;
      return safeResult(await client.DescribeDomainList(params));
    }
  );

  server.registerTool(
    "dns.describe_records",
    {
      description: "查询 DNSPod 域名解析记录",
      inputSchema: {
        domain: z.string().optional(),
        domainId: z.number().int().optional(),
        subDomain: z.string().optional(),
        recordType: z.string().optional(),
        limit: z.number().int().min(1).max(3000).optional(),
        offset: z.number().int().min(0).optional()
      }
    },
    async ({ domain, domainId, subDomain, recordType, limit, offset }) => {
      if (domainId === undefined && !domain) {
        throw new Error("请提供 domain 或 domainId");
      }
      const client = createDnspodClient(ctx.getCredentials());
      const params = {
        Limit: limit ?? 20,
        Offset: offset ?? 0,
        ...(domainId !== undefined ? { DomainId: domainId } : { Domain: domain as string }),
        ...(subDomain ? { SubDomain: subDomain } : {}),
        ...(recordType ? { RecordType: recordType } : {})
      };
      return safeResult(
        await client.DescribeRecordList(
          params as Parameters<typeof client.DescribeRecordList>[0]
        )
      );
    }
  );

  server.registerTool(
    "dns.create_record",
    {
      description: "创建 DNSPod 解析记录（CreateRecord）",
      inputSchema: {
        domain: z.string(),
        recordType: z.string().describe("A / CNAME / MX / TXT 等"),
        recordLine: z.string().describe("线路，如 默认"),
        value: z.string(),
        subDomain: z.string().optional().describe("主机记录，默认 @"),
        ttl: z.number().int().min(1).max(604800).optional(),
        mx: z.number().int().min(0).max(65535).optional(),
        status: z.enum(["ENABLE", "DISABLE"]).optional()
      }
    },
    async ({ domain, recordType, recordLine, value, subDomain, ttl, mx, status }) => {
      const client = createDnspodClient(ctx.getCredentials());
      return safeResult(
        await client.CreateRecord({
          Domain: domain,
          RecordType: recordType,
          RecordLine: recordLine,
          Value: value,
          SubDomain: subDomain,
          TTL: ttl,
          MX: mx,
          Status: status
        })
      );
    }
  );

  server.registerTool(
    "dns.modify_record",
    {
      description: "修改 DNSPod 解析记录（ModifyRecord，需完整记录字段）",
      inputSchema: {
        domain: z.string(),
        recordId: z.number().int(),
        recordType: z.string().describe("A / CNAME / MX / TXT 等"),
        recordLine: z.string().describe("线路，如 默认"),
        value: z.string(),
        subDomain: z.string().optional(),
        ttl: z.number().int().min(1).max(604800).optional(),
        mx: z.number().int().min(0).max(65535).optional(),
        status: z.enum(["ENABLE", "DISABLE"]).optional()
      }
    },
    async ({
      domain,
      recordId,
      subDomain,
      recordType,
      recordLine,
      value,
      ttl,
      mx,
      status
    }) => {
      const client = createDnspodClient(ctx.getCredentials());
      return safeResult(
        await client.ModifyRecord({
          Domain: domain,
          RecordId: recordId,
          RecordType: recordType,
          RecordLine: recordLine,
          Value: value,
          SubDomain: subDomain,
          TTL: ttl,
          MX: mx,
          Status: status
        })
      );
    }
  );

  server.registerTool(
    "dns.delete_record",
    {
      description: "删除 DNSPod 解析记录（需 domain + recordId）",
      inputSchema: {
        domain: z.string().describe("域名"),
        recordId: z.number().int().describe("记录 ID，从 describe_records 获取")
      }
    },
    async ({ domain, recordId }) => {
      if (!domain?.trim() || recordId === undefined || recordId <= 0) {
        return safeResult(
          rejectBlocked("dns", "delete_record", "删除记录必须同时提供有效 domain 与 recordId")
        );
      }
      const client = createDnspodClient(ctx.getCredentials());
      return safeResult(await client.DeleteRecord({ Domain: domain, RecordId: recordId }));
    }
  );
}
