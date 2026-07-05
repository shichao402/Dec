import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { homedir } from "node:os";
import { join, resolve } from "node:path";
import { execFileSync } from "node:child_process";
import type COS from "cos-nodejs-sdk-v5";

import { BLOCKED_OPERATIONS } from "./sensitive.js";

export function resolveProjectRoot(): string {
  const fromEnv = process.env.TENCENT_CLOUD_MCP_PROJECT_ROOT?.trim();
  if (fromEnv) {
    return resolve(fromEnv);
  }
  return process.cwd();
}

export const MISE_CONF_D_REL = ".config/mise/conf.d/tencent-cloud.toml";
export const MISE_LOCAL_REL = "mise.local.toml";

export function resolveMiseSecretsPath(projectRoot: string): string {
  const confd = join(projectRoot, MISE_CONF_D_REL);
  if (existsSync(confd)) {
    return confd;
  }
  return join(projectRoot, MISE_LOCAL_REL);
}

export function defaultMiseSecretsWritePath(projectRoot: string): string {
  return join(projectRoot, MISE_CONF_D_REL);
}

export function loadEnvFromMiseLocal(projectRoot: string): Record<string, string> {
  const path = resolveMiseSecretsPath(projectRoot);
  if (!existsSync(path)) {
    return {};
  }
  const raw = readFileSync(path, "utf8");
  const env: Record<string, string> = {};
  let inEnv = false;
  for (const line of raw.split("\n")) {
    const trimmed = line.trim();
    if (trimmed === "[env]") {
      inEnv = true;
      continue;
    }
    if (trimmed.startsWith("[") && trimmed.endsWith("]")) {
      inEnv = false;
      continue;
    }
    if (!inEnv || !trimmed || trimmed.startsWith("#")) {
      continue;
    }
    const match = trimmed.match(/^([A-Z0-9_]+)\s*=\s*"(.*)"\s*$/);
    if (match) {
      env[match[1]] = match[2].replace(/\\"/g, '"');
    }
  }
  return env;
}

export type TencentCredentials = {
  secretId: string;
  secretKey: string;
  region: string;
  cosBucket: string;
  cosRegion: string;
  cdbRegion: string;
  cdbInstanceId: string;
};

export type CdbConnectionConfig = {
  host: string;
  port: number;
  user: string;
  password: string;
  database: string;
};

const ENV_KEYS = [
  "TENCENTCLOUD_SECRET_ID",
  "TENCENTCLOUD_SECRET_KEY",
  "TENCENTCLOUD_REGION",
  "COS_SECRET_ID",
  "COS_SECRET_KEY",
  "COS_BUCKET",
  "COS_REGION",
  "CDB_REGION",
  "CDB_INSTANCE_ID",
  "CDB_HOST",
  "CDB_PORT",
  "CDB_USER",
  "CDB_PASSWORD",
  "CDB_DATABASE",
  "MYSQL_HOST",
  "MYSQL_PORT",
  "MYSQL_USER",
  "MYSQL_PASSWORD",
  "MYSQL_DATABASE"
] as const;

export function resolveCredentials(projectRoot: string): TencentCredentials {
  const fromFile = loadEnvFromMiseLocal(projectRoot);
  const env = { ...fromFile, ...pickProcessEnv() };

  const secretId =
    env.TENCENTCLOUD_SECRET_ID ?? env.COS_SECRET_ID ?? env.TencentAPI_SecretId ?? "";
  const secretKey =
    env.TENCENTCLOUD_SECRET_KEY ?? env.COS_SECRET_KEY ?? env.TencentAPI_SecretKey ?? "";

  if (!secretId || !secretKey) {
    throw new Error(
      "缺少腾讯云密钥。请配置 .config/mise/conf.d/tencent-cloud.toml 或运行 meta.sync_pkv_config 同步 pkv 密钥。"
    );
  }

  const region = env.TENCENTCLOUD_REGION ?? env.COS_REGION ?? "ap-chengdu";
  return {
    secretId,
    secretKey,
    region,
    cosBucket: env.COS_BUCKET ?? env.TENCENTCOS_BUCKET ?? "",
    cosRegion: env.COS_REGION ?? region,
    cdbRegion: env.CDB_REGION ?? region,
    cdbInstanceId: env.CDB_INSTANCE_ID ?? ""
  };
}

export function resolveEnv(projectRoot: string): Record<string, string> {
  return { ...loadEnvFromMiseLocal(projectRoot), ...pickProcessEnv() };
}

function pickProcessEnv(): Record<string, string> {
  const env: Record<string, string> = {};
  for (const key of ENV_KEYS) {
    const value = process.env[key];
    if (value) {
      env[key] = value;
    }
  }
  return env;
}

function escapeTomlString(value: string): string {
  return value.replace(/\\/g, "\\\\").replace(/"/g, '\\"');
}

function buildMiseLocalToml(env: Record<string, string>): string {
  const keys = [
    "TENCENTCLOUD_SECRET_ID",
    "TENCENTCLOUD_SECRET_KEY",
    "TENCENTCLOUD_REGION",
    "COS_SECRET_ID",
    "COS_SECRET_KEY",
    "COS_BUCKET",
    "COS_REGION",
    "CDB_REGION",
    "CDB_INSTANCE_ID",
    "CDB_HOST",
    "CDB_PORT",
    "CDB_USER",
    "CDB_PASSWORD",
    "CDB_DATABASE"
  ];
  const lines = ["[env]"];
  for (const key of keys) {
    const value = env[key];
    if (value) {
      lines.push(`${key}="${escapeTomlString(value)}"`);
    }
  }
  lines.push("");
  return `${lines.join("\n")}`;
}

function readJsonEnv(path: string): Record<string, string> {
  if (!existsSync(path)) {
    return {};
  }
  const parsed = JSON.parse(readFileSync(path, "utf8")) as Record<string, unknown>;
  const env: Record<string, string> = {};
  for (const [key, value] of Object.entries(parsed)) {
    if (typeof value === "string" && value.length > 0) {
      env[key] = value;
    }
  }
  return env;
}

function normalizeTencentEnv(raw: Record<string, string>): Record<string, string> {
  const secretId =
    raw.TENCENTCLOUD_SECRET_ID ?? raw.COS_SECRET_ID ?? raw.TencentAPI_SecretId ?? "";
  const secretKey =
    raw.TENCENTCLOUD_SECRET_KEY ?? raw.COS_SECRET_KEY ?? raw.TencentAPI_SecretKey ?? "";

  return {
    TENCENTCLOUD_SECRET_ID: secretId,
    TENCENTCLOUD_SECRET_KEY: secretKey,
    TENCENTCLOUD_REGION: raw.TENCENTCLOUD_REGION ?? raw.COS_REGION ?? "ap-chengdu",
    COS_SECRET_ID: raw.COS_SECRET_ID ?? secretId,
    COS_SECRET_KEY: raw.COS_SECRET_KEY ?? secretKey,
    COS_BUCKET: raw.COS_BUCKET ?? raw.TENCENTCOS_BUCKET ?? "",
    COS_REGION: raw.COS_REGION ?? raw.TENCENTCLOUD_REGION ?? "ap-chengdu",
    CDB_REGION: raw.CDB_REGION ?? raw.TENCENTCLOUD_REGION ?? "ap-chengdu",
    CDB_INSTANCE_ID: raw.CDB_INSTANCE_ID ?? "",
    CDB_HOST: raw.CDB_HOST ?? "",
    CDB_PORT: raw.CDB_PORT ?? "",
    CDB_USER: raw.CDB_USER ?? "",
    CDB_PASSWORD: raw.CDB_PASSWORD ?? "",
    CDB_DATABASE: raw.CDB_DATABASE ?? ""
  };
}

export function syncPkvConfig(
  projectRoot: string,
  folder = "tencent-cloud"
): { source: string; path: string; keys: string[] } {
  let source = "fallback";
  let rawEnv: Record<string, string> = {};

  try {
    const output = execFileSync("pkv", ["get", folder, "env"], {
      encoding: "utf8",
      timeout: 8000,
      env: { ...process.env, PKV_NO_TUI: "1" },
      stdio: ["ignore", "pipe", "pipe"]
    }).trim();
    if (output) {
      rawEnv = JSON.parse(output) as Record<string, string>;
      source = `pkv:${folder}`;
    }
  } catch {
    // Bitwarden 未解锁或 pkv 不可用时走 fallback
  }

  if (Object.keys(rawEnv).length === 0) {
    const pkvFallback = join(homedir(), ".pkv", "env", "HelloKnightTasker.json");
    rawEnv = readJsonEnv(pkvFallback);
    if (Object.keys(rawEnv).length > 0) {
      source = pkvFallback;
    }
  }

  const env = normalizeTencentEnv(rawEnv);
  if (!env.TENCENTCLOUD_SECRET_ID || !env.TENCENTCLOUD_SECRET_KEY) {
    throw new Error(
      "无法获取腾讯云密钥，请解锁 Bitwarden 或检查 pkv / .config/mise/conf.d/tencent-cloud.toml"
    );
  }

  const path = defaultMiseSecretsWritePath(projectRoot);
  mkdirSync(join(projectRoot, ".config/mise/conf.d"), { recursive: true });
  writeFileSync(path, buildMiseLocalToml(env), "utf8");
  return {
    source,
    path,
    keys: Object.keys(env).filter((key) => Boolean(env[key]))
  };
}

export function syncDecAssets(projectRoot: string): string {
  const output = execFileSync("dec", ["pull"], {
    cwd: projectRoot,
    encoding: "utf8",
    env: process.env,
    stdio: ["ignore", "pipe", "pipe"]
  });
  return output.trim();
}

export const TOOL_NAMESPACES = {
  meta: {
    description: "元信息、密钥同步、敏感操作说明",
    tools: [
      "meta.list_namespaces",
      "meta.list_blocked_operations",
      "meta.sync_pkv_config",
      "meta.sync_dec_assets"
    ]
  },
  cvm: {
    description: "云服务器 CVM（查询、TAT 命令与文件传输）",
    tools: [
      "cvm.describe_instances",
      "cvm.describe_security_groups",
      "cvm.run_command",
      "cvm.upload_file",
      "cvm.download_file",
      "cvm.describe_invocation"
    ]
  },
  cos: {
    description: "对象存储 COS（对象与桶配置）",
    tools: [
      "cos.list_objects",
      "cos.upload_object",
      "cos.download_object",
      "cos.head_object",
      "cos.delete_object",
      "cos.get_bucket_info",
      "cos.get_bucket_cors",
      "cos.put_bucket_cors",
      "cos.get_bucket_policy",
      "cos.put_bucket_policy"
    ]
  },
  dns: {
    description: "DNSPod 域名解析",
    tools: [
      "dns.describe_domains",
      "dns.describe_records",
      "dns.create_record",
      "dns.modify_record",
      "dns.delete_record"
    ]
  },
  lighthouse: {
    description: "轻量应用服务器 Lighthouse",
    tools: [
      "lighthouse.describe_instances",
      "lighthouse.describe_firewall_rules",
      "lighthouse.run_command",
      "lighthouse.upload_file",
      "lighthouse.download_file",
      "lighthouse.describe_invocation"
    ]
  },
  cdb: {
    description: "云数据库 MySQL（CDB 管控 API + 可选 SQL）",
    tools: [
      "cdb.describe_instances",
      "cdb.describe_wan_service",
      "cdb.open_wan_service",
      "cdb.close_wan_service",
      "cdb.describe_databases",
      "cdb.describe_accounts",
      "cdb.describe_tables",
      "cdb.create_database",
      "cdb.describe_security_groups",
      "cdb.add_security_group_ingress",
      "cdb.query_sql",
      "cdb.execute_sql"
    ]
  }
} as const;

export function listNamespaces() {
  return Object.entries(TOOL_NAMESPACES).map(([name, entry]) => ({
    namespace: name,
    description: entry.description,
    tools: [...entry.tools]
  }));
}

export function listBlockedOperations() {
  return {
    policy: "以下敏感操作不暴露为 MCP tool；若通过组合命令尝试等效操作，将在服务端拒绝。",
    blocked: BLOCKED_OPERATIONS,
    notes: {
      cos_delete_object: "cos.delete_object 需 confirm=true",
      dns_delete_record: "dns.delete_record 需同时提供 domain 与 recordId",
      cdb_sql: "cdb.execute_sql 禁止 DROP/TRUNCATE/无 WHERE 的 DELETE",
      cdb_close_wan: "cdb.close_wan_service 需 confirm=true",
      cdb_security_group_ingress:
        "cdb.add_security_group_ingress 需 confirm=true；禁止 0.0.0.0/0 与全端口；非 3306 外网端口默认同时放通 TCP 3306",
      cdb_account_password:
        "ModifyAccountPassword 未暴露为 MCP tool；修改 DB 密码请用控制台或 CDB API（CDB_PASSWORD 为 MySQL 账号密码，非 TENCENTCLOUD_SECRET_*）",
      file_transfer: "CVM/Lighthouse 文件传输通过 TAT RunCommand + base64，需实例已安装并运行云助手 Agent"
    }
  };
}

export function redactSecrets<T>(value: T): T {
  return JSON.parse(
    JSON.stringify(value, (key, v) => {
      if (typeof v === "string" && /secret|password|token|key/i.test(key) && v.length > 8) {
        return `${v.slice(0, 4)}****${v.slice(-4)}`;
      }
      return v;
    })
  ) as T;
}

export function cosBucketOrThrow(creds: TencentCredentials, bucket?: string): string {
  const name = bucket ?? creds.cosBucket;
  if (!name) {
    throw new Error("缺少 COS_BUCKET，请在 .config/mise/conf.d/tencent-cloud.toml 中配置");
  }
  return name;
}

export function promisifyCos<T>(
  cos: COS,
  method: string,
  params: Record<string, unknown>
): Promise<T> {
  return new Promise((resolve, reject) => {
    const fn = (cos as unknown as Record<string, (p: Record<string, unknown>, cb: (err: Error | null, data: T) => void) => void>)[method];
    if (typeof fn !== "function") {
      reject(new Error(`COS method ${method} not found`));
      return;
    }
    fn.call(cos, params, (err, data) => {
      if (err) reject(err);
      else resolve(data);
    });
  });
}
