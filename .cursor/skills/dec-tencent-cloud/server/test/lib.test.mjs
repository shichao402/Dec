import assert from "node:assert/strict";
import { mkdtempSync, mkdirSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

import {
  listBlockedOperations,
  listNamespaces,
  loadEnvFromMiseLocal,
  resolveCredentials
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

test("resolveCredentials merges conf.d secrets", () => {
  const dir = mkdtempSync(join(tmpdir(), "tcm-"));
  const confd = join(dir, ".config/mise/conf.d");
  mkdirSync(confd, { recursive: true });
  writeFileSync(
    join(confd, "tencent-cloud.toml"),
    `[env]\nTENCENTCLOUD_SECRET_ID="sid"\nTENCENTCLOUD_SECRET_KEY="skey"\nCOS_BUCKET="bkt"\nCDB_INSTANCE_ID="cdb-xxx"\n`
  );
  const creds = resolveCredentials(dir);
  assert.equal(creds.secretId, "sid");
  assert.equal(creds.cosBucket, "bkt");
  assert.equal(creds.cdbInstanceId, "cdb-xxx");
});

test("listNamespaces includes v0.2 tools", () => {
  const names = listNamespaces().flatMap((n) => n.tools);
  assert.ok(names.includes("cvm.describe_instances"));
  assert.ok(names.includes("cvm.run_command"));
  assert.ok(names.includes("cos.download_object"));
  assert.ok(names.includes("dns.create_record"));
  assert.ok(names.includes("cdb.describe_instances"));
  assert.ok(names.includes("cdb.query_sql"));
  assert.ok(names.includes("meta.list_blocked_operations"));
});

test("listBlockedOperations documents sensitive ops", () => {
  const blocked = listBlockedOperations();
  assert.ok(blocked.blocked.cvm.length > 0);
  assert.ok(blocked.blocked.cos.some((s) => s.includes("deleteBucket")));
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
