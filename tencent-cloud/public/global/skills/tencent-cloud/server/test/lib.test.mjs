import assert from "node:assert/strict";
import { mkdtempSync, mkdirSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

import {
  listBlockedOperations,
  listNamespaces,
  loadEnvFromMiseLocal,
  resolveCredentials,
  resolvePagesCredentials
} from "../dist/lib.js";
import {
  assertSafeFirewallRules,
  assertSafeIngressRule,
  isDangerousNetworkRule,
  normalizeCidr,
  validateSqlStatement
} from "../dist/sensitive.js";

test("loadEnvFromMiseLocal parses conf.d [env] section", () => {
  const dir = mkdtempSync(join(tmpdir(), "tcm-"));
  const confd = join(dir, ".config/mise/conf.d");
  mkdirSync(confd, { recursive: true });
  writeFileSync(
    join(confd, "tencent-cloud.toml"),
    `[env]\nTENCENTCLOUD_SECRET_ID="sid"\nTENCENTCLOUD_SECRET_KEY="skey"\n`
  );
  const env = loadEnvFromMiseLocal(dir);
  assert.equal(env.TENCENTCLOUD_SECRET_ID, "sid");
  assert.equal(env.TENCENTCLOUD_SECRET_KEY, "skey");
});

test("loadEnvFromMiseLocal falls back to mise.local.toml", () => {
  const dir = mkdtempSync(join(tmpdir(), "tcm-"));
  writeFileSync(
    join(dir, "mise.local.toml"),
    `[env]\nTENCENTCLOUD_SECRET_ID="sid"\nTENCENTCLOUD_SECRET_KEY="skey"\n`
  );
  const env = loadEnvFromMiseLocal(dir);
  assert.equal(env.TENCENTCLOUD_SECRET_ID, "sid");
  assert.equal(env.TENCENTCLOUD_SECRET_KEY, "skey");
});

test("resolveCredentials accepts TencentAPI_* from process env", () => {
  const prevId = process.env.TencentAPI_SecretId;
  const prevKey = process.env.TencentAPI_SecretKey;
  process.env.TencentAPI_SecretId = "from-dec-exec";
  process.env.TencentAPI_SecretKey = "from-dec-exec-key";
  try {
    const dir = mkdtempSync(join(tmpdir(), "tcm-"));
    const creds = resolveCredentials(dir);
    assert.equal(creds.secretId, "from-dec-exec");
    assert.equal(creds.secretKey, "from-dec-exec-key");
  } finally {
    if (prevId === undefined) delete process.env.TencentAPI_SecretId;
    else process.env.TencentAPI_SecretId = prevId;
    if (prevKey === undefined) delete process.env.TencentAPI_SecretKey;
    else process.env.TencentAPI_SecretKey = prevKey;
  }
});

test("resolvePagesCredentials needs its own token, not the CAM key", () => {
  const dir = mkdtempSync(join(tmpdir(), "tcm-"));
  const confd = join(dir, ".config/mise/conf.d");
  mkdirSync(confd, { recursive: true });
  writeFileSync(
    join(confd, "tencent-cloud.toml"),
    `[env]\nTENCENTCLOUD_SECRET_ID="sid"\nTENCENTCLOUD_SECRET_KEY="skey"\n`
  );
  assert.throws(() => resolvePagesCredentials(dir), /API Token/);

  writeFileSync(
    join(confd, "tencent-cloud.toml"),
    `[env]\nTENCENTCLOUD_SECRET_ID="sid"\nEDGEONE_PAGES_API_TOKEN="pages-token"\n`
  );
  const china = resolvePagesCredentials(dir);
  assert.equal(china.token, "pages-token");
  assert.equal(china.region, "china");
  assert.equal(china.baseUrl, "https://pages-api.cloud.tencent.com/v1");
  assert.equal(resolvePagesCredentials(dir, "global").baseUrl, "https://pages-api.edgeone.ai/v1");
});

test("listNamespaces includes v0.3 pages and teo tools", () => {
  const names = listNamespaces().flatMap((n) => n.tools);
  assert.ok(names.includes("pages_create_project"));
  assert.ok(names.includes("pages_deploy_dir"));
  assert.ok(names.includes("pages_create_zone_custom_domain"));
  assert.ok(names.includes("teo_describe_zones"));
  assert.ok(names.includes("teo_apply_free_certificate"));
  assert.ok(names.includes("teo_modify_hosts_certificate"));
});

test("listNamespaces includes v0.2 tools", () => {
  const names = listNamespaces().flatMap((n) => n.tools);
  assert.ok(names.includes("cvm_describe_instances"));
  assert.ok(names.includes("cvm_run_command"));
  assert.ok(names.includes("cos_download_object"));
  assert.ok(names.includes("cos_get_bucket_domain"));
  assert.ok(names.includes("cos_put_bucket_domain"));
  assert.ok(names.includes("dns_create_record"));
  assert.ok(names.includes("ssl_apply_certificate"));
  assert.ok(names.includes("ssl_deploy_certificate_instance"));
  assert.ok(names.includes("cdb_describe_instances"));
  assert.ok(names.includes("cdb_query_sql"));
  assert.ok(names.includes("meta_list_blocked_operations"));
});

test("listBlockedOperations documents sensitive ops", () => {
  const blocked = listBlockedOperations();
  assert.ok(blocked.blocked.cvm.length > 0);
  assert.ok(blocked.blocked.cos.some((s) => s.includes("deleteBucket")));
  assert.ok(blocked.blocked.pages.some((s) => s.includes("DeletePagesProject")));
  assert.ok(blocked.notes.pages_token.includes("EDGEONE_PAGES_API_TOKEN"));
});

test("isDangerousNetworkRule detects 0.0.0.0/0 ALL", () => {
  assert.equal(isDangerousNetworkRule({ CidrBlock: "0.0.0.0/0", Protocol: "ALL", Action: "ACCEPT" }), true);
  assert.equal(isDangerousNetworkRule({ CidrBlock: "10.0.0.0/8", Protocol: "TCP", Port: "80", Action: "ACCEPT" }), false);
});

test("assertSafeFirewallRules throws on dangerous rule", () => {
  assert.throws(() =>
    assertSafeFirewallRules([{ CidrBlock: "0.0.0.0/0", Protocol: "TCP", Port: "22", Action: "ACCEPT" }])
  );
});

test("assertSafeIngressRule rejects 0.0.0.0/0 and ALL port", () => {
  assert.throws(() => assertSafeIngressRule({ cidr: "0.0.0.0/0", port: "22915" }));
  assert.throws(() => assertSafeIngressRule({ cidr: "1.2.3.4/32", port: "ALL" }));
  assert.doesNotThrow(() => assertSafeIngressRule({ cidr: "1.2.3.4/32", port: "22915" }));
});

test("normalizeCidr appends /32 for bare IP", () => {
  assert.equal(normalizeCidr("1.2.3.4"), "1.2.3.4/32");
  assert.equal(normalizeCidr("1.2.3.4/32"), "1.2.3.4/32");
});

test("validateSqlStatement blocks destructive SQL", () => {
  assert.equal(validateSqlStatement("DROP TABLE users").allowed, false);
  assert.equal(validateSqlStatement("SELECT 1").allowed, true);
  assert.equal(validateSqlStatement("DELETE FROM users").allowed, false);
  assert.equal(validateSqlStatement("DELETE FROM users WHERE id=1").allowed, true);
});
