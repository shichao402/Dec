import { z } from "zod";
import type { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";

import { createSslClient } from "../clients.js";
import { rejectBlocked } from "../sensitive.js";
import { safeResult, textResult, type ToolContext } from "./common.js";

export function registerSslTools(server: McpServer, ctx: ToolContext) {
  server.registerTool(
    "ssl_describe_certificates",
    {
      description: "查询 SSL 证书列表（DescribeCertificates）",
      inputSchema: {
        searchKey: z.string().optional().describe("模糊匹配证书 ID、备注或域名"),
        certificateStatus: z.string().optional().describe("状态码，逗号分隔，如 1 表示已签发"),
        limit: z.number().int().min(1).max(1000).optional(),
        offset: z.number().int().min(0).optional(),
        region: z.string().optional()
      }
    },
    async ({ searchKey, certificateStatus, limit, offset, region }) => {
      const client = createSslClient(ctx.getCredentials(), region);
      const params: Record<string, unknown> = {
        Limit: limit ?? 20,
        Offset: offset ?? 0
      };
      if (searchKey) params.SearchKey = searchKey;
      if (certificateStatus) {
        params.CertificateStatus = certificateStatus
          .split(",")
          .map((item) => Number(item.trim()))
          .filter((item) => !Number.isNaN(item));
      }
      return safeResult(await client.DescribeCertificates(params));
    }
  );

  server.registerTool(
    "ssl_describe_certificate_detail",
    {
      description: "查询单张 SSL 证书详情（DescribeCertificateDetail）",
      inputSchema: {
        certificateId: z.string(),
        region: z.string().optional()
      }
    },
    async ({ certificateId, region }) => {
      const client = createSslClient(ctx.getCredentials(), region);
      return safeResult(await client.DescribeCertificateDetail({ CertificateId: certificateId }));
    }
  );

  server.registerTool(
    "ssl_apply_certificate",
    {
      description:
        "申请免费 DV 证书（ApplyCertificate）。默认 PackageType=83（TrustAsia C1 DV Free）、DvAuthMethod=DNS_AUTO。需 confirm=true",
      inputSchema: {
        domainName: z.string().describe("证书绑定域名，如 raw.firoyang.com"),
        confirm: z.boolean().describe("必须为 true 才提交申请"),
        dvAuthMethod: z.enum(["DNS_AUTO", "DNS", "FILE"]).optional(),
        packageType: z.string().optional().describe("默认 83；不要用 2"),
        region: z.string().optional()
      }
    },
    async ({ domainName, confirm, dvAuthMethod, packageType, region }) => {
      if (!confirm) {
        return textResult(
          rejectBlocked("ssl", "apply_certificate", "申请证书需显式设置 confirm=true")
        );
      }
      const client = createSslClient(ctx.getCredentials(), region);
      return safeResult(
        await client.ApplyCertificate({
          DvAuthMethod: dvAuthMethod ?? "DNS_AUTO",
          DomainName: domainName,
          PackageType: packageType ?? "83"
        })
      );
    }
  );

  server.registerTool(
    "ssl_describe_host_cos_instances",
    {
      description:
        "列出证书可部署的 COS 自定义域名实例（DescribeHostCosInstanceList）。不要传 Region 参数，会报 UnknownParameter",
      inputSchema: {
        certificateId: z.string(),
        region: z.string().optional()
      }
    },
    async ({ certificateId, region }) => {
      const client = createSslClient(ctx.getCredentials(), region);
      return safeResult(
        await client.DescribeHostCosInstanceList({
          CertificateId: certificateId,
          ResourceType: "cos"
        })
      );
    }
  );

  server.registerTool(
    "ssl_deploy_certificate_instance",
    {
      description:
        "把证书部署到云资源（DeployCertificateInstance）。COS 的 InstanceIdList 必须是 Region|Bucket|Domain。需 confirm=true",
      inputSchema: {
        certificateId: z.string(),
        instanceIdList: z.array(z.string()).min(1),
        resourceType: z.string().optional().describe("默认 cos"),
        confirm: z.boolean().describe("必须为 true 才部署"),
        region: z.string().optional().describe("COS/CLB 等类型必传，如 ap-guangzhou")
      }
    },
    async ({ certificateId, instanceIdList, resourceType, confirm, region }) => {
      if (!confirm) {
        return textResult(
          rejectBlocked("ssl", "deploy_certificate_instance", "部署证书需显式设置 confirm=true")
        );
      }
      const kind = resourceType ?? "cos";
      if (kind === "cos") {
        for (const item of instanceIdList) {
          if (item.split("|").length !== 3) {
            throw new Error(
              `COS 部署 InstanceIdList 必须是 Region|Bucket|Domain，收到 ${item}`
            );
          }
        }
      }
      const client = createSslClient(ctx.getCredentials(), region);
      return safeResult(
        await client.DeployCertificateInstance({
          CertificateId: certificateId,
          InstanceIdList: instanceIdList,
          ResourceType: kind
        })
      );
    }
  );

  server.registerTool(
    "ssl_describe_host_deploy_record_detail",
    {
      description: "查询证书部署记录详情（DescribeHostDeployRecordDetail）",
      inputSchema: {
        deployRecordId: z.string(),
        limit: z.number().int().min(1).max(200).optional(),
        offset: z.number().int().min(0).optional(),
        region: z.string().optional()
      }
    },
    async ({ deployRecordId, limit, offset, region }) => {
      const client = createSslClient(ctx.getCredentials(), region);
      return safeResult(
        await client.DescribeHostDeployRecordDetail({
          DeployRecordId: deployRecordId,
          Limit: limit ?? 20,
          Offset: offset ?? 0
        })
      );
    }
  );
}
