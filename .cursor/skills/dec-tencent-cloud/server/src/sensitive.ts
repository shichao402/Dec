/** 敏感操作清单 — 不暴露为 tool，调用时返回明确拒绝 */

export const BLOCKED_OPERATIONS = {
  cvm: [
    "TerminateInstances — 销毁 CVM 实例",
    "ResetInstancesPassword — 重置实例密码",
    "ModifySecurityGroupPolicies — 修改安全组规则（含开放 0.0.0.0/0）",
    "DeleteSecurityGroup — 删除安全组",
    "AssociateSecurityGroups — 批量绑定危险安全组"
  ],
  lighthouse: [
    "TerminateInstances — 销毁 Lighthouse 实例",
    "ResetInstancesPassword — 重置实例密码",
    "CreateFirewallRules / ModifyFirewallRules — 开放 0.0.0.0/0 全端口",
    "DeleteFirewallRules — 批量删除防火墙规则（请用控制台）"
  ],
  cos: [
    "deleteBucket — 删除存储桶",
    "deleteMultipleObject — 批量删除对象",
    "deleteBucketPolicy / deleteBucketCors — 删除桶级配置（请用 put 覆盖）"
  ],
  cdb: [
    "DeleteDatabase — 删除数据库",
    "DropDatabase / DropTables — 通过 API 删除",
    "SQL: DROP DATABASE, DROP TABLE, TRUNCATE, DELETE 无 WHERE"
  ],
  dns: ["DeleteRecordBatch — 批量删除解析记录"]
} as const;

export function rejectBlocked(namespace: string, operation: string, reason?: string) {
  return {
    blocked: true,
    namespace,
    operation,
    message: reason ?? `敏感操作已禁用: ${namespace}.${operation}`,
    hint: "详见 meta.list_blocked_operations"
  };
}

/** 检测防火墙/安全组规则是否开放 0.0.0.0/0 危险端口 */
export function isDangerousNetworkRule(rule: {
  CidrBlock?: string;
  Cidr?: string;
  Protocol?: string;
  Port?: string;
  Action?: string;
}): boolean {
  const cidr = (rule.CidrBlock ?? rule.Cidr ?? "").trim();
  const action = (rule.Action ?? "ACCEPT").toUpperCase();
  const port = (rule.Port ?? "").trim().toUpperCase();
  const protocol = (rule.Protocol ?? "").trim().toUpperCase();

  if (action !== "ACCEPT") {
    return false;
  }
  if (cidr !== "0.0.0.0/0" && cidr !== "::/0") {
    return false;
  }
  // 全协议或全端口视为危险
  if (protocol === "ALL" || port === "ALL" || port === "") {
    return true;
  }
  // 常见管理端口大范围开放
  if (port.includes("22") || port.includes("3389") || port.includes("3306")) {
    return true;
  }
  return false;
}

export function assertSafeFirewallRules(
  rules: Array<{ CidrBlock?: string; Cidr?: string; Protocol?: string; Port?: string; Action?: string }>
): void {
  for (const rule of rules) {
    if (isDangerousNetworkRule(rule)) {
      throw new Error(
        "拒绝：检测到对 0.0.0.0/0 开放危险端口/协议的安全组或防火墙规则。请在控制台手动操作。"
      );
    }
  }
}

/** 校验单条入站规则：禁止全开放 CIDR、全端口/全协议 */
export function assertSafeIngressRule(rule: {
  cidr: string;
  port: string | number;
  protocol?: string;
}): void {
  const cidr = rule.cidr.trim();
  if (!cidr || cidr === "0.0.0.0/0" || cidr === "::/0") {
    throw new Error("拒绝：入站源 CIDR 不能为空或 0.0.0.0/0 / ::/0，请使用最小权限 IP/32");
  }
  const port = String(rule.port).trim();
  if (!port || port.toUpperCase() === "ALL") {
    throw new Error("拒绝：必须指定具体端口，不能为 ALL");
  }
  const protocol = (rule.protocol ?? "TCP").trim().toUpperCase();
  if (protocol === "ALL") {
    throw new Error("拒绝：必须指定具体协议（TCP/UDP），不能为 ALL");
  }
  assertSafeFirewallRules([
    { CidrBlock: cidr, Protocol: protocol, Port: port, Action: "ACCEPT" }
  ]);
}

export function normalizeCidr(cidr: string): string {
  const trimmed = cidr.trim();
  if (trimmed.includes("/")) return trimmed;
  return `${trimmed}/32`;
}

const DESTRUCTIVE_SQL =
  /\b(DROP\s+(DATABASE|TABLE|INDEX|VIEW|SCHEMA|USER)|TRUNCATE\s+TABLE|DELETE\s+FROM\s+\w+\s*;)\b/i;

const DELETE_WITHOUT_WHERE = /\bDELETE\s+FROM\b/i;

export function validateSqlStatement(sql: string): { allowed: boolean; reason?: string } {
  const trimmed = sql.trim();
  if (!trimmed) {
    return { allowed: false, reason: "SQL 不能为空" };
  }
  if (DESTRUCTIVE_SQL.test(trimmed)) {
    return {
      allowed: false,
      reason: "禁止执行 DROP / TRUNCATE / 无 WHERE 的 DELETE 等破坏性 SQL"
    };
  }
  if (DELETE_WITHOUT_WHERE.test(trimmed) && !/\bWHERE\b/i.test(trimmed)) {
    return { allowed: false, reason: "DELETE 必须包含 WHERE 条件" };
  }
  // 仅允许多语句中的单条（简单防护）
  const statements = trimmed.split(";").filter((s) => s.trim());
  if (statements.length > 1) {
    return { allowed: false, reason: "一次仅允许执行一条 SQL 语句" };
  }
  return { allowed: true };
}
