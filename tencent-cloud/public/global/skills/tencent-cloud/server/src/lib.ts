import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { homedir } from "node:os";
import { join, resolve, sep } from "node:path";
import { execFileSync } from "node:child_process";
import type COS from "cos-nodejs-sdk-v5";

import { BLOCKED_OPERATIONS } from "./sensitive.js";

export function resolveProjectRoot(): string {
  const fromEnv =
    process.env.TENCENT_CLOUD_MCP_PROJECT_ROOT?.trim() ||
    process.env.DEC_PROJECT_ROOT?.trim();
  if (fromEnv) {
    return resolve(fromEnv);
  }
  // run.mjs may start the server with cwd=server/; recover project root from
  // .../<project>/.dec/cache/tencent-cloud/skills/tencent-cloud/server
  const marker = `${sep}.dec${sep}cache${sep}`;
  const cwd = resolve(process.cwd());
  const idx = cwd.toLowerCase().lastIndexOf(marker.toLowerCase());
  if (idx > 0) {
    return cwd.slice(0, idx);
  }
  return cwd;
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
  "MYSQL_DATABASE",
  // EdgeOne Pages / Makers uses its own bearer token, not the CAM SecretId/Key
  "EDGEONE_PAGES_API_TOKEN",
  "EDGEONE_API_TOKEN",
  "PAGES_API_TOKEN",
  "EDGEONE_PAGES_REGION",
  // Injected by `dec exec` from Bitwarden / .secrets (historical pkv names)
  "TencentAPI_SecretId",
  "TencentAPI_SecretKey",
  "EdgeOnePages_ApiToken"
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

export type PagesCredentials = {
  token: string;
  baseUrl: string;
  region: "china" | "global";
};

const PAGES_ENDPOINTS = {
  china: "https://pages-api.cloud.tencent.com/v1",
  global: "https://pages-api.edgeone.ai/v1"
} as const;

/**
 * EdgeOne Pages / Makers authenticates with a console-issued bearer token on a
 * separate endpoint. The CAM SecretId/Key pair cannot sign these calls, so the
 * token is resolved independently of resolveCredentials.
 */
export function resolvePagesCredentials(
  projectRoot: string,
  region?: string
): PagesCredentials {
  const env = resolveEnv(projectRoot);
  const token = (
    env.EDGEONE_PAGES_API_TOKEN ??
    env.EDGEONE_API_TOKEN ??
    env.PAGES_API_TOKEN ??
    env.EdgeOnePages_ApiToken ??
    ""
  ).trim();
  if (!token) {
    throw new Error(
      "缺少 EdgeOne Pages API Token。请在 Pages 控制台 API Token 页创建后写入 .config/mise/conf.d/tencent-cloud.toml 的 EDGEONE_PAGES_API_TOKEN（腾讯云 SecretId/Key 无法调用 Pages 接口）。"
    );
  }
  const requested = (region ?? env.EDGEONE_PAGES_REGION ?? "china").trim().toLowerCase();
  const resolved = requested === "global" ? "global" : "china";
  return { token, baseUrl: PAGES_ENDPOINTS[resolved], region: resolved };
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
      "meta_list_namespaces",
      "meta_list_blocked_operations",
      "meta_sync_pkv_config",
      "meta_sync_dec_assets"
    ]
  },
  cvm: {
    description: "云服务器 CVM（查询、TAT 命令与文件传输）",
    tools: [
      "cvm_describe_instances",
      "cvm_describe_security_groups",
      "cvm_run_command",
      "cvm_upload_file",
      "cvm_download_file",
      "cvm_describe_invocation"
    ]
  },
  cos: {
    description: "对象存储 COS（对象与桶配置）",
    tools: [
      "cos_list_objects",
      "cos_upload_object",
      "cos_download_object",
      "cos_head_object",
      "cos_delete_object",
      "cos_get_bucket_info",
      "cos_get_bucket_cors",
      "cos_put_bucket_cors",
      "cos_get_bucket_policy",
      "cos_put_bucket_policy",
      "cos_get_bucket_domain",
      "cos_put_bucket_domain"
    ]
  },
  dns: {
    description: "DNSPod 域名解析",
    tools: [
      "dns_describe_domains",
      "dns_describe_records",
      "dns_create_record",
      "dns_modify_record",
      "dns_delete_record"
    ]
  },
  ssl: {
    description: "SSL 证书申请与部署到 COS 自定义域名",
    tools: [
      "ssl_describe_certificates",
      "ssl_describe_certificate_detail",
      "ssl_apply_certificate",
      "ssl_describe_host_cos_instances",
      "ssl_deploy_certificate_instance",
      "ssl_describe_host_deploy_record_detail"
    ]
  },
  pages: {
    description: "EdgeOne Pages / Makers 静态站（独立 API Token，非 CAM 密钥）",
    tools: [
      "pages_describe_projects",
      "pages_create_project",
      "pages_describe_cos_temp_token",
      "pages_create_deployment",
      "pages_deploy_dir",
      "pages_describe_deployments",
      "pages_describe_deployment_log",
      "pages_describe_project_envs",
      "pages_modify_project_envs",
      "pages_create_zone_custom_domain",
      "pages_describe_zone_custom_domains"
    ]
  },
  teo: {
    description: "EdgeOne 站点侧（标准 TC3 密钥）：域名归属、CNAME、免费证书",
    tools: [
      "teo_describe_zones",
      "teo_verify_ownership",
      "teo_check_cname_status",
      "teo_apply_free_certificate",
      "teo_check_free_certificate_verification",
      "teo_modify_hosts_certificate"
    ]
  },
  lighthouse: {
    description: "轻量应用服务器 Lighthouse",
    tools: [
      "lighthouse_describe_instances",
      "lighthouse_describe_firewall_rules",
      "lighthouse_run_command",
      "lighthouse_upload_file",
      "lighthouse_download_file",
      "lighthouse_describe_invocation"
    ]
  },
  cdb: {
    description: "云数据库 MySQL（CDB 管控 API + 可选 SQL）",
    tools: [
      "cdb_describe_instances",
      "cdb_describe_wan_service",
      "cdb_open_wan_service",
      "cdb_close_wan_service",
      "cdb_describe_databases",
      "cdb_describe_accounts",
      "cdb_describe_tables",
      "cdb_create_database",
      "cdb_describe_security_groups",
      "cdb_add_security_group_ingress",
      "cdb_query_sql",
      "cdb_execute_sql"
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
      cos_delete_object: "cos_delete_object 需 confirm=true",
      cos_put_bucket_domain: "cos_put_bucket_domain 会合并已有自定义域名后再写入，需 confirm=true；Type 保持 REST，不要改成 WEBSITE",
      dns_delete_record: "dns_delete_record 需同时提供 domain 与 recordId",
      ssl_apply: "ssl_apply_certificate 默认 PackageType=83（TrustAsia C1 DV Free）与 DNS_AUTO；付费证书请用控制台",
      ssl_deploy: "ssl_deploy_certificate_instance 部署 COS 时 InstanceIdList 元素必须是 Region|Bucket|Domain",
      pages_token:
        "Pages/Makers 用控制台签发的 EDGEONE_PAGES_API_TOKEN，与 TENCENTCLOUD_SECRET_* 是两套凭据；证书与 CNAME 那半套走 teo_* 工具，用的才是 CAM 密钥",
      pages_delete:
        "DeletePagesProject / DeletePagesProjectCustomDomains 未暴露为 MCP tool；删项目或摘域名请用控制台",
      pages_deploy: "pages_deploy_dir 需 confirm=true；单次上传上限 2000 个文件 / 64MiB",
      teo_certificate:
        "teo_apply_free_certificate 与 teo_modify_hosts_certificate 需 confirm=true；Mode=disable 会关掉线上域名的证书",
      cdb_sql: "cdb_execute_sql 禁止 DROP/TRUNCATE/无 WHERE 的 DELETE",
      cdb_close_wan: "cdb_close_wan_service 需 confirm=true",
      cdb_security_group_ingress:
        "cdb_add_security_group_ingress 需 confirm=true；禁止 0.0.0.0/0 与全端口；非 3306 外网端口默认同时放通 TCP 3306",
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
