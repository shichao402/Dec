import { spawnSync } from "node:child_process";

const chunks = [];
for await (const chunk of process.stdin) {
  chunks.push(chunk);
}

const python =
  process.env.PYTHON?.trim() || (process.platform === "win32" ? "python" : "python3");
const result = spawnSync(python, ["scripts/relkit_producer.py"], {
  cwd: process.cwd(),
  env: process.env,
  input: Buffer.concat(chunks),
  stdio: ["pipe", "inherit", "inherit"],
});

if (result.error) {
  process.stderr.write(`dec Relkit producer: ${result.error.message}\n`);
  process.exit(1);
}
process.exit(result.status ?? 1);
