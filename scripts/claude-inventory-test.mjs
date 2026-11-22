import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { once } from "node:events";
import { mkdtemp, mkdir, readFile, rm, writeFile } from "node:fs/promises";
import http from "node:http";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { setTimeout as sleep } from "node:timers/promises";

const root = await mkdtemp(join(tmpdir(), "meter-claude-inventory-"));
const agent = join(root, "agent"), auth = join(root, "auth.json"), count = join(root, "probes"), socketPath = join(root, "workspace.sock");
await mkdir(agent);
await writeFile(auth, JSON.stringify({ loggedIn: false }));
const child = spawn(process.execPath, ["server/main.ts"], { env: {
  ...process.env, HOME: root, PI_CODING_AGENT_DIR: agent, PI_WORKSPACE_HOME: root,
  PI_PROVIDER: "pi-claude-cli", PI_CLAUDE_CLI_COMMAND: resolve("scripts/fixtures/claude-stub.cjs"),
  PI_CLAUDE_CLI_TRACE: "off", METER_STUB_AUTH_FILE: auth, METER_STUB_AUTH_COUNT: count,
  CLAUDE_STUB_MODELS: JSON.stringify([{ value: "claude-test-99", displayName: "Test Claude" }]),
}, stdio: ["ignore", "pipe", "pipe"] });
let logs = "";
for (const pipe of [child.stdout, child.stderr]) pipe.on("data", (data) => { logs += data; });
async function request(path, method = "GET") {
  return await new Promise((resolve, reject) => {
    const req = http.request({ socketPath, path, method }, async (res) => {
      const chunks = []; for await (const chunk of res) chunks.push(chunk);
      resolve({ status: res.statusCode, value: JSON.parse(Buffer.concat(chunks).toString()) });
    });
    req.on("error", reject); req.end();
  });
}
try {
  for (let i = 0; i < 500 && !logs.includes("Workspace ready"); i++) {
    if (child.exitCode !== null) throw new Error(logs);
    await sleep(20);
  }
  assert.match(logs, /Workspace ready/);
  const first = await request("/v1/models");
  const status = (inventory) => inventory.value.providers.find((p) => p.id === "pi-claude-cli");
  assert.equal(status(first).status, "unconfigured");
  assert.match(status(first).externalAuth, /claude auth login/);
  assert.equal(status(first).catalogSource, "builtin");
  assert.deepEqual(status(first).methods, []);
  await Promise.all([request("/v1/models"), request("/v1/models")]);
  assert.equal((await readFile(count, "utf8")).trim().split("\n").length, 1, "repeated checks share auth probe");
  await writeFile(auth, JSON.stringify({ loggedIn: true, subscriptionType: "pro" }));
  const refreshed = await request("/v1/providers/pi-claude-cli/refresh", "POST");
  assert.equal(refreshed.status, 200, JSON.stringify(refreshed));
  assert.equal(status(refreshed).status, "configured");
  assert.equal(status(refreshed).catalogSource, "account");
  assert.deepEqual(refreshed.value.models.filter((m) => m.provider === "pi-claude-cli").map((m) => m.modelId), ["claude-test-99"]);
  await writeFile(auth, "not-json");
  const unknown = await request("/v1/providers/pi-claude-cli/refresh", "POST");
  assert.equal(status(unknown).status, "unknown");
  console.log("Claude inventory: cached auth, external login status and explicit account discovery passed");
} finally {
  child.kill("SIGTERM");
  if (child.exitCode === null) await once(child, "exit").catch(() => {});
  await rm(root, { recursive: true, force: true });
}
