import assert from "node:assert/strict";
import { join } from "node:path";
import test from "node:test";

import { createCosClient, createCdbClient, createCvmClient, createDnspodClient } from "../dist/clients.js";
import { resolveCredentials } from "../dist/lib.js";

const projectRoot = join(import.meta.dirname, "../../AgentsHelpMe");

test("cvm.describe_instances live", { skip: !process.env.RUN_LIVE_TESTS }, async () => {
  const creds = resolveCredentials(projectRoot);
  const client = createCvmClient(creds);
  const result = await client.DescribeInstances({ Limit: 1, Offset: 0 });
  assert.ok(result);
});

test("cos.list_objects live", { skip: !process.env.RUN_LIVE_TESTS }, async () => {
  const creds = resolveCredentials(projectRoot);
  if (!creds.cosBucket) return;
  const cos = createCosClient(creds);
  const result = await new Promise((resolve, reject) => {
    cos.getBucket({ Bucket: creds.cosBucket, Region: creds.cosRegion, MaxKeys: 1 }, (err, data) => {
      if (err) reject(err);
      else resolve(data);
    });
  });
  assert.ok(result);
});

test("dns.describe_domains live", { skip: !process.env.RUN_LIVE_TESTS }, async () => {
  const creds = resolveCredentials(projectRoot);
  const client = createDnspodClient(creds);
  const result = await client.DescribeDomainList({ Limit: 1, Offset: 0 });
  assert.ok(result);
});

test("cdb.describe_instances live", { skip: !process.env.RUN_LIVE_TESTS }, async () => {
  const creds = resolveCredentials(projectRoot);
  const client = createCdbClient(creds);
  const params = { Limit: 1, Offset: 0 };
  if (creds.cdbInstanceId) params.InstanceIds = [creds.cdbInstanceId];
  const result = await client.DescribeDBInstances(params);
  assert.ok(result);
});
