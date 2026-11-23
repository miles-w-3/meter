import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { once } from "node:events";
import { mkdtemp, mkdir, rm, writeFile } from "node:fs/promises";
import http from "node:http";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { setTimeout as sleep } from "node:timers/promises";
import { createConnection } from "node:net";
import { WebSocket } from "ws";

const dir = await mkdtemp(join(tmpdir(), "meter-models-"));
const agentDir = join(dir, "pi");
await mkdir(agentDir);
const socketPath = join(dir, "workspace.sock");
let child;
async function request(path, method = "GET", body) {
  const data = body === undefined ? undefined : JSON.stringify(body);
  return new Promise((resolve, reject) => {
    const req = http.request({ socketPath, path, method, headers: data === undefined ? {} : {
      "content-type": "application/json", "content-length": Buffer.byteLength(data),
    } }, async (res) => {
      const chunks = [];
      for await (const chunk of res) chunks.push(chunk);
      const text = Buffer.concat(chunks).toString();
      resolve({ status: res.statusCode, value: text ? JSON.parse(text) : undefined });
    });
    req.on("error", reject);
    req.end(data);
  });
}
async function start(extra = {}) {
  child = spawn(process.execPath, ["server/main.ts"], {
    env: { PATH: process.env.PATH, HOME: dir, PI_CODING_AGENT_DIR: agentDir, METER_HOME: dir, PI_PROVIDER: "openai", ...extra },
    stdio: ["ignore", "pipe", "pipe"],
  });
  let logs = "";
  child.stdout.on("data", (data) => { logs += data; });
  child.stderr.on("data", (data) => { logs += data; });
  for (let i = 0; i < 500; i++) {
    if (logs.includes("Workspace ready")) return;
    if (child.exitCode !== null) throw new Error(logs);
    await sleep(20);
  }
  throw new Error("Server startup timed out: " + logs);
}
async function stop() {
  const exited = once(child, "exit");
  assert.equal((await request("/v1/server/stop", "POST")).status, 202);
  assert.equal((await exited)[0], 0);
  child = undefined;
}
try {
  // Legacy provider/model environment values must not create a normal-mode default.
  await start({ PI_PROVIDER: "openai-codex", PI_MODEL: "legacy-model" });
  assert.equal((await request("/v1/server")).value.provider, "");
  assert.deepEqual((await request("/v1/settings/defaults")).value, { model: null, thinkingLevel: null, source: "unconfigured" });
  await stop();
  await start();
  const initial = await request("/v1/settings/defaults");
  assert.deepEqual(initial.value, { model: null, thinkingLevel: null, source: "unconfigured" });
  const inventory = await request("/v1/models");
  assert.equal(inventory.status, 200);
  await writeFile(join(agentDir, "models.json"), JSON.stringify({ providers: {
    localtest: { baseUrl: "http://localhost:11434/v1", api: "openai-completions", apiKey: "test", models: [{ id: "new-model" }] },
  } }));
  const refreshed = await request("/v1/models");
  assert.equal(refreshed.status, 200);
  assert.ok(refreshed.value.models.some((m) => m.provider === "localtest" && m.modelId === "new-model"));
  await writeFile(join(agentDir, "models.json"), JSON.stringify({ providers: {} }));
  assert.ok(!(await request("/v1/models")).value.models.some((m) => m.provider === "localtest"));
  const openai = inventory.value.providers.find((p) => p.id === "openai");
  assert.equal(openai.status, "unconfigured");
  assert.ok(openai.methods.some((m) => m.type === "api_key"));
  assert.ok(openai.methods.some((m) => m.type === "oauth"));
  const item = inventory.value.models.find((m) => m.provider === "openai");
  assert.ok(item);
  const selected = { model: { provider: item.provider, modelId: item.modelId }, thinkingLevel: item.thinkingLevels[0] };
  assert.equal((await request("/v1/settings/defaults", "PUT", { model: { provider: "bad", modelId: "bad" }, thinkingLevel: null })).status, 400);
  assert.equal((await request("/v1/settings/defaults", "PUT", selected)).status, 200);
  const created = await request("/v1/conversations", "POST", { cwd: dir });
  assert.equal(created.status, 201);
  assert.equal(created.value.model, `${item.provider}/${item.modelId}`);
  const stream = new WebSocket(`ws://workspace/v1/conversations/${created.value.id}/stream`, { createConnection: () => createConnection(socketPath) });
  const frames = [];
  stream.on("message", (data) => frames.push(JSON.parse(String(data))));
  const secondStream = new WebSocket(`ws://workspace/v1/conversations/${created.value.id}/stream`, { createConnection: () => createConnection(socketPath) });
  const secondFrames = [];
  secondStream.on("message", (data) => secondFrames.push(JSON.parse(String(data))));
  await Promise.all([once(stream, "open"), once(secondStream, "open")]);
  while (!frames.length || !secondFrames.length) await sleep(10);
  const second = inventory.value.models.find((m) => m.provider === "openai" && m.modelId !== item.modelId);
  assert.ok(second);
  const change = { model: { provider: second.provider, modelId: second.modelId }, thinkingLevel: second.thinkingLevels[0], expected: frames[0].view.selection };
  assert.equal((await request(`/v1/conversations/${created.value.id}/model`, "PUT", change)).status, 200);
  for (let i = 0; i < 200 && ![frames, secondFrames].every((list) => list.some((f) => f.view.selection?.model?.modelId === second.modelId)); i++) await sleep(10);
  assert.ok([frames, secondFrames].every((list) => list.some((f) => f.view.selection?.model?.modelId === second.modelId)), "both watches publish model change");
  const explicit = await request("/v1/conversations", "POST", { cwd: dir, selection: { model: change.model, thinkingLevel: change.thinkingLevel } });
  assert.equal(explicit.status, 201);
  assert.equal(explicit.value.model, `${second.provider}/${second.modelId}`);
  assert.deepEqual(explicit.value.selection, { model: change.model, thinkingLevel: change.thinkingLevel });
  assert.equal((await request(`/v1/conversations/${created.value.id}/model`, "PUT", change)).value.code, "selection_conflict");
  const invalid = { ...change, expected: { model: change.model, thinkingLevel: change.thinkingLevel }, thinkingLevel: "max" };
  if (!second.thinkingLevels.includes("max")) assert.equal((await request(`/v1/conversations/${created.value.id}/model`, "PUT", invalid)).value.code, "invalid_thinking");
  stream.close();
  secondStream.close();
  await stop();
  await start({ PI_MODEL: "some-other-model" });
  assert.deepEqual((await request("/v1/settings/defaults")).value, { ...selected, source: "saved" });
  assert.equal((await request("/v1/conversations/" + created.value.id)).value.model, `${second.provider}/${second.modelId}`);
  assert.equal((await request("/v1/settings/defaults", "PUT", { model: null, thinkingLevel: null })).status, 200);
  const empty = await request("/v1/conversations", "POST", { cwd: dir });
  assert.equal(empty.value.model, "unconfigured");
  const submission = await request(`/v1/conversations/${empty.value.id}/messages`, "POST", { requestId: "no-model", mode: "prompt", content: "hello" });
  assert.equal(submission.value.code, "model_unconfigured");
  await stop();
  console.log("Model inventory, credential-free startup, and durable defaults passed");
} finally {
  if (child) { child.kill("SIGKILL"); await once(child, "exit").catch(() => {}); }
  await rm(dir, { recursive: true, force: true });
}
