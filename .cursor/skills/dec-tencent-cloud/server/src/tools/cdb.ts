import { z } from "zod";
import type { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";

import { createCdbClient, createVpcClient } from "../clients.js";
import { createMysqlPool, resolveCdbConnectionFromEnv } from "../helpers/mysql.js";
import { redactSecrets, resolveEnv } from "../lib.js";
import { assertSafeIngressRule, normalizeCidr, validateSqlStatement } from "../sensitive.js";
import { safeResult, textResult, type ToolContext } from "./common.js";

type WanInfo = {
  instanceId: string;
  wanStatus: number | undefined;
  wanStatusText: string;
  wanDomain: string | undefined;
  wanPort: number | undefined;
  vip: string | undefined;
  vport: number | undefined;
};

const WAN_STATUS_TEXT: Record<number, string> = {
  0: "未开通外网",
  1: "已开通外网",
  2: "已关闭外网"
};

function resolveInstanceId(instanceId: string | undefined, defaultId: string): string {
  const id = instanceId ?? defaultId;
  if (!id) throw new Error("请提供 instanceId 或在 .config/mise/conf.d/tencent-cloud.toml 配置 CDB_INSTANCE_ID");
  return id;
}

function extractWanInfo(instance: Record<string, unknown>, instanceId: string): WanInfo {
  const wanStatus = instance.WanStatus as number | undefined;
  return {
    instanceId,
    wanStatus,
    wanStatusText: wanStatus !== undefined ? (WAN_STATUS_TEXT[wanStatus] ?? `未知(${wanStatus})`) : "未知",
    wanDomain: instance.WanDomain as string | undefined,
    wanPort: instance.WanPort as number | undefined,
    vip: instance.Vip as string | undefined,
    vport: instance.Vport as number | undefined
  };
}

async function fetchWanInfo(
  client: ReturnType<typeof createCdbClient>,
  instanceId: string
): Promise<WanInfo | null> {
  const result = await client.DescribeDBInstances({ InstanceIds: [instanceId], Limit: 1, Offset: 0 });
  const items = (result as { Items?: Record<string, unknown>[] }).Items ?? [];
  const instance = items[0];
  if (!instance) return null;
  return extractWanInfo(instance, instanceId);
}

async function waitAsyncRequest(
  client: ReturnType<typeof createCdbClient>,
  asyncRequestId: string,
  timeoutMs = 120_000
): Promise<{ status: string; info?: string }> {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    const result = await client.DescribeAsyncRequestInfo({ AsyncRequestId: asyncRequestId });
    const status = result.Status ?? "UNKNOWN";
    if (status === "SUCCESS") {
      return { status, info: result.Info };
    }
    if (status === "FAILED" || status === "KILLED" || status === "REMOVED") {
      throw new Error(`异步任务失败: ${status}${result.Info ? ` — ${result.Info}` : ""}`);
    }
    await new Promise((r) => setTimeout(r, 3000));
  }
  throw new Error(`等待异步任务超时 (${timeoutMs}ms)，AsyncRequestId=${asyncRequestId}`);
}

type IngressRuleSpec = {
  port: string;
  protocol: string;
  description?: string;
};

async function addVpcIngressRules(
  vpcClient: ReturnType<typeof createVpcClient>,
  securityGroupId: string,
  cidrBlock: string,
  rules: IngressRuleSpec[]
) {
  const added: IngressRuleSpec[] = [];
  for (const rule of rules) {
    assertSafeIngressRule({ cidr: cidrBlock, port: rule.port, protocol: rule.protocol });
    await vpcClient.CreateSecurityGroupPolicies({
      SecurityGroupId: securityGroupId,
      SecurityGroupPolicySet: {
        Ingress: [
          {
            PolicyIndex: 0,
            Protocol: rule.protocol,
            Port: rule.port,
            CidrBlock: cidrBlock,
            Action: "ACCEPT",
            PolicyDescription: rule.description ?? `CDB ${rule.port} from ${cidrBlock}`
          }
        ]
      }
    });
    added.push(rule);
  }
  return added;
}

function resolveIngressRules(
  port: string,
  protocol: string,
  includeInternalPort: boolean,
  description?: string
): IngressRuleSpec[] {
  const rules: IngressRuleSpec[] = [
    { port, protocol, description: description ?? `CDB ${port} from client IP` }
  ];
  if (includeInternalPort && port !== "3306") {
    rules.push({
      port: "3306",
      protocol: "TCP",
      description: "CDB internal 3306 for WAN proxy (Tencent doc)"
    });
  }
  return rules;
}

export function registerCdbTools(server: McpServer, ctx: ToolContext) {
  server.registerTool(
    "cdb.describe_instances",
    {
      description: "查询 CDB MySQL 实例列表（DescribeDBInstances）",
      inputSchema: {
        region: z.string().optional().describe("地域，默认 CDB_REGION"),
        instanceIds: z.array(z.string()).optional(),
        limit: z.number().int().min(1).max(100).optional(),
        offset: z.number().int().min(0).optional()
      }
    },
    async ({ region, instanceIds, limit, offset }) => {
      const creds = ctx.getCredentials();
      const client = createCdbClient(creds, region);
      const params: Record<string, unknown> = {
        Limit: limit ?? 20,
        Offset: offset ?? 0
      };
      if (instanceIds?.length) params.InstanceIds = instanceIds;
      else if (creds.cdbInstanceId) params.InstanceIds = [creds.cdbInstanceId];
      return safeResult(await client.DescribeDBInstances(params));
    }
  );

  server.registerTool(
    "cdb.describe_wan_service",
    {
      description: "查询 CDB 实例外网状态（WanStatus/WanDomain/WanPort，来自 DescribeDBInstances）",
      inputSchema: {
        instanceId: z.string().optional().describe("默认 CDB_INSTANCE_ID"),
        region: z.string().optional()
      }
    },
    async ({ instanceId, region }) => {
      const creds = ctx.getCredentials();
      const id = resolveInstanceId(instanceId, creds.cdbInstanceId);
      const client = createCdbClient(creds, region);
      const wan = await fetchWanInfo(client, id);
      if (!wan) throw new Error(`未找到实例: ${id}`);
      return safeResult(wan);
    }
  );

  server.registerTool(
    "cdb.open_wan_service",
    {
      description: "开通 CDB 实例外网访问（OpenWanService），完成后返回外网域名与端口",
      inputSchema: {
        instanceId: z.string().optional().describe("默认 CDB_INSTANCE_ID"),
        region: z.string().optional(),
        wait: z.boolean().optional().describe("是否等待异步任务完成，默认 true")
      }
    },
    async ({ instanceId, region, wait }) => {
      const creds = ctx.getCredentials();
      const id = resolveInstanceId(instanceId, creds.cdbInstanceId);
      const client = createCdbClient(creds, region);

      const before = await fetchWanInfo(client, id);
      if (before?.wanStatus === 1 && before.wanDomain) {
        return safeResult({
          alreadyOpen: true,
          message: "外网已开通",
          ...before
        });
      }

      const openResult = await client.OpenWanService({ InstanceId: id });
      const asyncRequestId = openResult.AsyncRequestId;
      let asyncStatus: { status: string; info?: string } | undefined;
      if (asyncRequestId && wait !== false) {
        asyncStatus = await waitAsyncRequest(client, asyncRequestId);
      }

      const after = await fetchWanInfo(client, id);
      return safeResult({
        asyncRequestId,
        asyncStatus,
        before,
        after
      });
    }
  );

  server.registerTool(
    "cdb.close_wan_service",
    {
      description: "关闭 CDB 实例外网访问（CloseWanService，需 confirm=true）",
      inputSchema: {
        instanceId: z.string().optional().describe("默认 CDB_INSTANCE_ID"),
        region: z.string().optional(),
        confirm: z.boolean().describe("必须为 true 才执行关闭外网"),
        wait: z.boolean().optional().describe("是否等待异步任务完成，默认 true")
      }
    },
    async ({ instanceId, region, confirm, wait }) => {
      if (!confirm) {
        return textResult({
          blocked: true,
          reason: "关闭外网需显式设置 confirm=true"
        });
      }
      const creds = ctx.getCredentials();
      const id = resolveInstanceId(instanceId, creds.cdbInstanceId);
      const client = createCdbClient(creds, region);

      const before = await fetchWanInfo(client, id);
      const closeResult = await client.CloseWanService({ InstanceId: id });
      const asyncRequestId = closeResult.AsyncRequestId;
      let asyncStatus: { status: string; info?: string } | undefined;
      if (asyncRequestId && wait !== false) {
        asyncStatus = await waitAsyncRequest(client, asyncRequestId);
      }
      const after = await fetchWanInfo(client, id);
      return safeResult({
        asyncRequestId,
        asyncStatus,
        before,
        after
      });
    }
  );

  server.registerTool(
    "cdb.describe_databases",
    {
      description: "查询 CDB 实例下的数据库列表",
      inputSchema: {
        instanceId: z.string().optional().describe("默认 CDB_INSTANCE_ID"),
        region: z.string().optional(),
        limit: z.number().int().min(1).max(100).optional(),
        offset: z.number().int().min(0).optional()
      }
    },
    async ({ instanceId, region, limit, offset }) => {
      const creds = ctx.getCredentials();
      const id = instanceId ?? creds.cdbInstanceId;
      if (!id) throw new Error("请提供 instanceId 或在 .config/mise/conf.d/tencent-cloud.toml 配置 CDB_INSTANCE_ID");
      const client = createCdbClient(creds, region);
      return safeResult(
        await client.DescribeDatabases({
          InstanceId: id,
          Limit: limit ?? 100,
          Offset: offset ?? 0
        })
      );
    }
  );

  server.registerTool(
    "cdb.describe_accounts",
    {
      description: "查询 CDB 实例账号列表",
      inputSchema: {
        instanceId: z.string().optional(),
        region: z.string().optional()
      }
    },
    async ({ instanceId, region }) => {
      const creds = ctx.getCredentials();
      const id = instanceId ?? creds.cdbInstanceId;
      if (!id) throw new Error("请提供 instanceId 或在 .config/mise/conf.d/tencent-cloud.toml 配置 CDB_INSTANCE_ID");
      const client = createCdbClient(creds, region);
      return safeResult(await client.DescribeAccounts({ InstanceId: id }));
    }
  );

  server.registerTool(
    "cdb.describe_tables",
    {
      description: "查询 CDB 实例指定库下的表列表",
      inputSchema: {
        instanceId: z.string().optional(),
        database: z.string().describe("数据库名"),
        region: z.string().optional(),
        limit: z.number().int().min(1).max(100).optional(),
        offset: z.number().int().min(0).optional()
      }
    },
    async ({ instanceId, database, region, limit, offset }) => {
      const creds = ctx.getCredentials();
      const id = instanceId ?? creds.cdbInstanceId;
      if (!id) throw new Error("请提供 instanceId 或在 .config/mise/conf.d/tencent-cloud.toml 配置 CDB_INSTANCE_ID");
      const client = createCdbClient(creds, region);
      return safeResult(
        await client.DescribeTables({
          InstanceId: id,
          Database: database,
          Limit: limit ?? 100,
          Offset: offset ?? 0
        })
      );
    }
  );

  server.registerTool(
    "cdb.create_database",
    {
      description: "在 CDB 实例中创建数据库（CreateDatabase）",
      inputSchema: {
        instanceId: z.string().optional(),
        database: z.string().describe("新数据库名"),
        charset: z.string().optional().describe("字符集，默认 utf8mb4"),
        region: z.string().optional()
      }
    },
    async ({ instanceId, database, charset, region }) => {
      const creds = ctx.getCredentials();
      const id = instanceId ?? creds.cdbInstanceId;
      if (!id) throw new Error("请提供 instanceId 或在 .config/mise/conf.d/tencent-cloud.toml 配置 CDB_INSTANCE_ID");
      const client = createCdbClient(creds, region);
      return safeResult(
        await client.CreateDatabase({
          InstanceId: id,
          DBName: database,
          CharacterSetName: charset ?? "utf8mb4"
        })
      );
    }
  );

  server.registerTool(
    "cdb.describe_security_groups",
    {
      description: "查询 CDB 实例关联的安全组及入站/出站规则（DescribeDBSecurityGroups）",
      inputSchema: {
        instanceId: z.string().optional().describe("默认 CDB_INSTANCE_ID"),
        region: z.string().optional()
      }
    },
    async ({ instanceId, region }) => {
      const creds = ctx.getCredentials();
      const id = resolveInstanceId(instanceId, creds.cdbInstanceId);
      const client = createCdbClient(creds, region);
      return safeResult(await client.DescribeDBSecurityGroups({ InstanceId: id }));
    }
  );

  server.registerTool(
    "cdb.add_security_group_ingress",
    {
      description:
        "为 CDB 关联的 VPC 安全组添加入站规则（CreateSecurityGroupPolicies）。禁止 0.0.0.0/0 与全端口；外网端口默认同时放通内网 3306（WAN 代理需要），需 confirm=true",
      inputSchema: {
        instanceId: z.string().optional().describe("默认 CDB_INSTANCE_ID，用于解析关联安全组"),
        securityGroupId: z.string().optional().describe("安全组 ID，如 sg-xxx；不填则从实例关联组取第一个"),
        cidr: z.string().describe("源 CIDR，如 1.2.3.4/32；纯 IP 自动补 /32"),
        port: z.union([z.string(), z.number()]).describe("目标端口，如 22915"),
        protocol: z.enum(["TCP", "UDP", "tcp", "udp"]).optional().describe("协议，默认 TCP"),
        description: z.string().optional().describe("规则备注"),
        includeInternalPort: z
          .boolean()
          .optional()
          .describe("非 3306 端口时是否同时放通 TCP 3306（外网访问必需），默认 true"),
        region: z.string().optional(),
        confirm: z.boolean().describe("必须为 true 才执行")
      }
    },
    async ({
      instanceId,
      securityGroupId,
      cidr,
      port,
      protocol,
      description,
      includeInternalPort,
      region,
      confirm
    }) => {
      if (!confirm) {
        return textResult({
          blocked: true,
          reason: "添加入站规则需显式设置 confirm=true"
        });
      }

      const creds = ctx.getCredentials();
      const id = resolveInstanceId(instanceId, creds.cdbInstanceId);
      const cdbClient = createCdbClient(creds, region);
      const vpcClient = createVpcClient(creds, region ?? creds.cdbRegion);

      let sgId = securityGroupId?.trim();
      if (!sgId) {
        const groups = await cdbClient.DescribeDBSecurityGroups({ InstanceId: id });
        sgId = groups.Groups?.[0]?.SecurityGroupId;
        if (!sgId) {
          throw new Error(`实例 ${id} 未关联安全组，请显式提供 securityGroupId`);
        }
      }

      const cidrBlock = normalizeCidr(cidr);
      const portStr = String(port).trim();
      const proto = (protocol ?? "TCP").toUpperCase();
      const ruleSpecs = resolveIngressRules(
        portStr,
        proto,
        includeInternalPort !== false,
        description
      );

      const before = await cdbClient.DescribeDBSecurityGroups({ InstanceId: id });
      const existingInbound = before.Groups?.find((g) => g.SecurityGroupId === sgId)?.Inbound ?? [];
      const toAdd = ruleSpecs.filter(
        (rule) =>
          !existingInbound.some(
            (r) =>
              r.CidrIp === cidrBlock &&
              String(r.PortRange) === rule.port &&
              String(r.IpProtocol).toLowerCase() === rule.protocol.toLowerCase()
          )
      );

      const added = toAdd.length
        ? await addVpcIngressRules(vpcClient, sgId, cidrBlock, toAdd)
        : [];

      const after = await cdbClient.DescribeDBSecurityGroups({ InstanceId: id });
      const matched = after.Groups?.find((g) => g.SecurityGroupId === sgId)?.Inbound?.filter((r) => {
        if (r.CidrIp !== cidrBlock) return false;
        return ruleSpecs.some(
          (rule) =>
            String(r.PortRange) === rule.port &&
            String(r.IpProtocol).toLowerCase() === rule.protocol.toLowerCase()
        );
      });

      return safeResult({
        success: true,
        instanceId: id,
        securityGroupId: sgId,
        cidr: cidrBlock,
        requestedRules: ruleSpecs,
        addedRules: added,
        skippedExisting: ruleSpecs.length - toAdd.length,
        matchedRules: matched
      });
    }
  );

  server.registerTool(
    "cdb.query_sql",
    {
      description: "只读 SQL 查询（SELECT/SHOW/DESCRIBE/EXPLAIN），需配置 CDB_HOST/CDB_USER/CDB_PASSWORD",
      inputSchema: {
        sql: z.string(),
        database: z.string().optional().describe("默认 CDB_DATABASE")
      }
    },
    async ({ sql, database }) => {
      const check = validateSqlStatement(sql);
      if (!check.allowed) {
        return textResult({ blocked: true, reason: check.reason });
      }
      const upper = sql.trim().toUpperCase();
      if (
        !upper.startsWith("SELECT") &&
        !upper.startsWith("SHOW") &&
        !upper.startsWith("DESCRIBE") &&
        !upper.startsWith("DESC") &&
        !upper.startsWith("EXPLAIN")
      ) {
        return textResult({
          blocked: true,
          reason: "query_sql 仅允许 SELECT/SHOW/DESCRIBE/EXPLAIN，写操作用 cdb.execute_sql"
        });
      }
      const config = resolveCdbConnectionFromEnv(resolveEnv(ctx.projectRoot), database);
      if (!config) {
        return textResult({
          stub: true,
          reason: "未配置 SQL 连接。请在 .config/mise/conf.d/tencent-cloud.toml 设置 CDB_HOST, CDB_USER, CDB_PASSWORD（及可选 CDB_PORT, CDB_DATABASE）"
        });
      }
      const pool = await createMysqlPool(config);
      try {
        const [rows] = await pool.query(sql);
        return textResult(redactSecrets({ rowCount: Array.isArray(rows) ? rows.length : 0, rows }));
      } finally {
        await pool.end();
      }
    }
  );

  server.registerTool(
    "cdb.execute_sql",
    {
      description: "执行有限 DML/DDL（禁止 DROP/TRUNCATE/无 WHERE DELETE），需 SQL 连接配置",
      inputSchema: {
        sql: z.string(),
        database: z.string().optional()
      }
    },
    async ({ sql, database }) => {
      const check = validateSqlStatement(sql);
      if (!check.allowed) {
        return textResult({ blocked: true, reason: check.reason });
      }
      const config = resolveCdbConnectionFromEnv(resolveEnv(ctx.projectRoot), database);
      if (!config) {
        return textResult({
          stub: true,
          reason: "未配置 SQL 连接。请在 .config/mise/conf.d/tencent-cloud.toml 设置 CDB_HOST, CDB_USER, CDB_PASSWORD"
        });
      }
      const pool = await createMysqlPool(config);
      try {
        const [result] = await pool.query(sql);
        return textResult(redactSecrets({ result }));
      } finally {
        await pool.end();
      }
    }
  );
}
