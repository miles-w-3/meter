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
let child;
let logs = "";
async function start() {
  logs = "";
  child = spawn(process.execPath, ["server/main.ts"], { env: {
    ...process.env, HOME: root, PI_CODING_AGENT_DIR: agent, METER_HOME: root,
    PI_PROVIDER: "", PI_CLAUDE_CODE: "1", PI_CLAUDE_CLI_COMMAND: resolve("scripts/fixtures/claude-stub.cjs"),
    PI_CLAUDE_CLI_TRACE: "off", METER_STUB_AUTH_FILE: auth, METER_STUB_AUTH_COUNT: count,
    CLAUDE_STUB_MODELS: JSON.stringify([{ value: "claude-test-99", displayName: "Test Claude" }]),
  }, stdio: ["ignore", "pipe", "pipe"] });
  for (const pipe of [child.stdout, child.stderr]) pipe.on("data", (data) => { logs += data; });
  for (let i = 0; i < 500 && !logs.includes("Workspace ready"); i++) {
    if (child.exitCode !== null) throw new Error(logs);
    await sleep(20);
  }
  assert.match(logs, /Workspace ready/);
}
async function stop() {
  child.kill("SIGTERM");
  if (child.exitCode === null) await once(child, "exit");
  assert.equal(child.exitCode, 0, logs);
  child = undefined;
}
async function request(path, method = "GET", body) {
  return await new Promise((resolve, reject) => {
    const data = body === undefined ? undefined : JSON.stringify(body);
    const req = http.request({ socketPath, path, method, headers: data === undefined ? {} : { "content-type": "application/json", "content-length": Buffer.byteLength(data) } }, async (res) => {
      const chunks = []; for await (const chunk of res) chunks.push(chunk);
      resolve({ status: res.statusCode, value: JSON.parse(Buffer.concat(chunks).toString()) });
    });
    req.on("error", reject); req.end(data);
  });
}
try {
  await start();
  const first = await request("/v1/models");
  const status = (inventory) => inventory.value.providers.find((p) => p.id === "pi-claude-cli");
  assert.equal(status(first).enabled, false);
  assert.equal(status(first).status, "unconfigured");
  assert.match(status(first).externalAuth, /claude auth login/);
  await assert.rejects(readFile(count), { code: "ENOENT" });
  const enabled = await request("/v1/providers/pi-claude-cli", "PUT", { enabled: true });
  assert.equal(enabled.status, 200);
  assert.equal(status(enabled).enabled, true);
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
  const selectedModel = refreshed.value.models.find((m) => m.provider === "pi-claude-cli");
  const selection = { model: { provider: selectedModel.provider, modelId: selectedModel.modelId }, thinkingLevel: selectedModel.thinkingLevels[0] };
  const created = await request("/v1/conversations", "POST", { cwd: root, selection });
  assert.equal(created.status, 201, JSON.stringify(created.value));
  const disabled = await request("/v1/providers/pi-claude-cli", "PUT", { enabled: false });
  assert.equal(status(disabled).enabled, false);
  assert.ok(!disabled.value.models.some((m) => m.provider === "pi-claude-cli"));
  assert.equal((await request("/v1/providers/pi-claude-cli/refresh", "POST")).status, 409);
  const conversation = await request(`/v1/conversations/${created.value.id}`);
  assert.deepEqual(conversation.value.selection, selection, "disabled provider preserves the conversation selection");
  const refused = await request(`/v1/conversations/${created.value.id}/messages`, "POST", { requestId: "disabled", mode: "prompt", content: "hello" });
  assert.equal(refused.value.code, "model_unavailable");
  const reenabled = await request("/v1/providers/pi-claude-cli", "PUT", { enabled: true });
  assert.equal(status(reenabled).enabled, true);
  assert.ok(reenabled.value.models.some((m) => m.provider === "pi-claude-cli"));
  await writeFile(auth, "not-json");
  const unknown = await request("/v1/providers/pi-claude-cli/refresh", "POST");
  assert.equal(status(unknown).status, "unknown");
  await request("/v1/providers/pi-claude-cli", "PUT", { enabled: false });
  await stop();
  await start();
  assert.equal(status(await request("/v1/models")).enabled, false, "disabled setting survives restart and ignores legacy env");
  await writeFile(auth, JSON.stringify({ loggedIn: true, subscriptionType: "pro" }));
  await request("/v1/providers/pi-claude-cli", "PUT", { enabled: true });
  await stop();
  await start();
  assert.equal(status(await request("/v1/models")).enabled, true, "enabled setting survives restart");
  console.log("Claude inventory: GUI setting, persistence, cached auth and explicit account discovery passed");
} finally {
  if (child) { child.kill("SIGTERM"); if (child.exitCode === null) await once(child, "exit").catch(() => {}); }
  await rm(root, { recursive: true, force: true });
}
