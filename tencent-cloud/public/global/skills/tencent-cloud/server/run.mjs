#!/usr/bin/env node
/**
 * MCP cold-start bootstrap: ensure node_modules before loading dist/.
 * Uses only Node builtins so it works before npm ci.
 * All diagnostics go to stderr (stdout is reserved for MCP JSON-RPC).
 */
import { createHash } from "node:crypto";
import { existsSync, readFileSync, writeFileSync } from "node:fs";
import { spawn, spawnSync } from "node:child_process";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const root = dirname(fileURLToPath(import.meta.url));
const lockPath = join(root, "package-lock.json");
const markerPath = join(root, "node_modules", "tencentcloud-sdk-nodejs", "package.json");
const stampPath = join(root, "node_modules", ".tencent-cloud-mcp-ci");
const entryPath = join(root, "dist", "index.js");

function lockHash() {
  if (!existsSync(lockPath)) {
    return "";
  }
  return createHash("sha256").update(readFileSync(lockPath)).digest("hex");
}

function needsInstall() {
  if (!existsSync(entryPath)) {
    console.error(
      `[tencent-cloud-mcp] missing ${entryPath}; run npm run build in the server directory`
    );
    process.exit(1);
  }
  if (!existsSync(markerPath) || !existsSync(stampPath)) {
    return true;
  }
  const expected = lockHash();
  if (!expected) {
    return true;
  }
  return readFileSync(stampPath, "utf8").trim() !== expected;
}

function ensureDeps() {
  if (!needsInstall()) {
    return;
  }
  if (!existsSync(lockPath)) {
    console.error(`[tencent-cloud-mcp] missing package-lock.json under ${root}`);
    process.exit(1);
  }
  console.error(`[tencent-cloud-mcp] installing dependencies (npm ci --omit=dev --ignore-scripts)...`);
  // Windows: Node cannot spawn the `npm` shim without shell / npm.cmd (ENOENT).
  // This is the recurring Cursor cold-start failure after every clean pull.
  const isWin = process.platform === "win32";
  const result = spawnSync(isWin ? "npm.cmd" : "npm", ["ci", "--omit=dev", "--ignore-scripts"], {
    cwd: root,
    env: process.env,
    encoding: "utf8",
    stdio: ["ignore", "pipe", "pipe"],
    shell: isWin
  });
  if (result.error) {
    console.error(`[tencent-cloud-mcp] npm ci spawn failed: ${result.error.message}`);
    process.exit(1);
  }
  if (result.stdout) {
    process.stderr.write(result.stdout);
  }
  if (result.stderr) {
    process.stderr.write(result.stderr);
  }
  if (result.status !== 0) {
    console.error(`[tencent-cloud-mcp] npm ci failed with exit ${result.status ?? 1}`);
    process.exit(result.status ?? 1);
  }
  writeFileSync(stampPath, `${lockHash()}\n`, "utf8");
  console.error(`[tencent-cloud-mcp] dependencies ready`);
}

ensureDeps();

const child = spawn(process.execPath, [entryPath], {
  cwd: root,
  env: process.env,
  stdio: "inherit"
});

child.on("error", (error) => {
  console.error(`[tencent-cloud-mcp] failed to start: ${error.message}`);
  process.exit(1);
});

child.on("exit", (code, signal) => {
  if (signal) {
    process.kill(process.pid, signal);
    return;
  }
  process.exit(code ?? 1);
});
