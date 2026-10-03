import { z } from "zod";
import type { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";

import { createTeoClient } from "../clients.js";
import { rejectBlocked } from "../sensitive.js";
import { safeResult, textResult, type ToolContext } from "./common.js";

/**
 * EdgeOne site-side APIs. Unlike pages_*, these are standard TC3-signed calls
 * and reuse TENCENTCLOUD_SECRET_ID/KEY, so no Pages API Token is needed.
 */
export function registerTeoTools(server: McpServer, ctx: ToolContext) {
  server.registerTool(
    "teo_describe_zones",
    {
      description: "查询 EdgeOne 站点列表（DescribeZones）。绑定域名与证书都要先拿到 ZoneId",
      inputSchema: {
        zoneName: z.string().optional().describe("按站点名过滤，如 firoyang.com"),
        limit: z.number().int().min(1).max(100).optional(),
        offset: z.number().int().min(0).optional(),
        region: z.string().optional()
      }
    },
    async ({ zoneName, limit, offset, region }) => {
      const client = createTeoClient(ctx.getCredentials(), region);
      const params: Record<string, unknown> = {
        Limit: limit ?? 20,
        Offset: offset ?? 0
      };
      if (zoneName) {
        params.Filters = [{ Name: "zone-name", Values: [zoneName], Fuzzy: false }];
      }
      return safeResult(await client.DescribeZones(params));
    }
  );

  server.registerTool(
    "teo_verify_ownership",
    {
      description: "校验域名归属权（VerifyOwnership）。返回 fail 时按提示补 TXT 记录或文件",
      inputSchema: {
        domain: z.string(),
        region: z.string().optional()
      }
    },
    async ({ domain, region }) => {
      const client = createTeoClient(ctx.getCredentials(), region);
      return safeResult(await client.VerifyOwnership({ Domain: domain.trim().toLowerCase() }));
    }
  );

  server.registerTool(
    "teo_check_cname_status",
    {
      description:
        "检查域名的 CNAME 是否已指向 EdgeOne 分配的地址（CheckCnameStatus）。active 才算接入成功",
      inputSchema: {
        zoneId: z.string(),
        recordNames: z.array(z.string()).min(1),
        region: z.string().optional()
      }
    },
    async ({ zoneId, recordNames, region }) => {
      const client = createTeoClient(ctx.getCredentials(), region);
      return safeResult(
        await client.CheckCnameStatus({
          ZoneId: zoneId,
          RecordNames: recordNames.map((name) => name.trim().toLowerCase())
        })
      );
    }
  );

  server.registerTool(
    "teo_apply_free_certificate",
    {
      description:
        "为 EdgeOne 域名申请免费证书（ApplyFreeCertificate）。默认 dns_challenge（DNS 委派）。需 confirm=true",
      inputSchema: {
        zoneId: z.string(),
        domain: z.string(),
        confirm: z.boolean().describe("必须为 true 才提交申请"),
        verificationMethod: z.enum(["dns_challenge", "http_challenge"]).optional(),
        region: z.string().optional()
      }
    },
    async ({ zoneId, domain, confirm, verificationMethod, region }) => {
      if (!confirm) {
        return textResult(
          rejectBlocked("teo", "apply_free_certificate", "申请证书需显式设置 confirm=true")
        );
      }
      const client = createTeoClient(ctx.getCredentials(), region);
      return safeResult(
        await client.ApplyFreeCertificate({
          ZoneId: zoneId,
          Domain: domain.trim().toLowerCase(),
          VerificationMethod: verificationMethod ?? "dns_challenge"
        })
      );
    }
  );

  server.registerTool(
    "teo_check_free_certificate_verification",
    {
      description:
        "查询免费证书申请结果（CheckFreeCertificateVerification）。报 FreeCertificateVerificationIsEmpty 说明还没触发申请",
      inputSchema: {
        zoneId: z.string(),
        domain: z.string(),
        region: z.string().optional()
      }
    },
    async ({ zoneId, domain, region }) => {
      const client = createTeoClient(ctx.getCredentials(), region);
      return safeResult(
        await client.CheckFreeCertificateVerification({
          ZoneId: zoneId,
          Domain: domain.trim().toLowerCase()
        })
      );
    }
  );

  server.registerTool(
    "teo_modify_hosts_certificate",
    {
      description:
        "配置域名证书（ModifyHostsCertificate）。eofreecert 启用免费证书，disable 关闭。需 confirm=true",
      inputSchema: {
        zoneId: z.string(),
        hosts: z.array(z.string()).min(1),
        mode: z.enum(["eofreecert", "disable"]).optional(),
        confirm: z.boolean().describe("必须为 true 才写入"),
        region: z.string().optional()
      }
    },
    async ({ zoneId, hosts, mode, confirm, region }) => {
      if (!confirm) {
        return textResult(
          rejectBlocked("teo", "modify_hosts_certificate", "改域名证书需显式设置 confirm=true")
        );
      }
      const client = createTeoClient(ctx.getCredentials(), region);
      return safeResult(
        await client.ModifyHostsCertificate({
          ZoneId: zoneId,
          Hosts: hosts.map((host) => host.trim().toLowerCase()),
          Mode: mode ?? "eofreecert"
        })
      );
    }
  );
}
