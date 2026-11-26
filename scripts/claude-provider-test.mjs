import assert from "node:assert/strict";
import { mkdtemp, mkdir, readFile, realpath, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { BACKGROUND_CONTEXT as context } from "@earendil-works/chord/context";
import { DatabaseSync } from "node:sqlite";
import { ProviderDoc } from "@earendil-works/pi-durable";
import { openHarness } from "../server/durable.ts";
import { ClaudeCwdBridge } from "../server/claude-code/bridge.ts";
import { ProviderPlugins } from "../server/provider-plugins.ts";

const root = await mkdtemp(join(tmpdir(), "meter-claude-test-"));
const a = join(root, "a"), b = join(root, "b"), log = join(root, "cwd.log"), pidFile = join(root, "claude.pid");
await mkdir(a); await mkdir(b);
process.env.PI_CLAUDE_CLI_COMMAND = resolve("scripts/fixtures/claude-stub.cjs");
process.env.PI_CLAUDE_CLI_RELAY = "off";
process.env.PI_CLAUDE_CLI_TRACE = "off";
process.env.METER_STUB_CWD = log;
process.env.CLAUDE_STUB_PID_FILE = pidFile;
process.env.PI_CODING_AGENT_DIR = root;
let opened;
const bridge = new ClaudeCwdBridge();
assert.throws(() => bridge.take("missing"), /no trusted/);
bridge.set("1", "session", a);
assert.throws(() => bridge.set("2", "session", b), /another conversation/);
assert.equal(bridge.take("session"), a);
assert.throws(() => bridge.take("session"), /no trusted/);
const registry = new ProviderPlugins();
registry.register({ provider: { id: "claude-test" }, needsConversationCwd: true, close: async () => {} });
const handlers = bridge.extension(registry).hooks[0].handlers;
const hookApi = (id, provider) => ({ conversationId: id, snapshot: async (token) =>
  token === ProviderDoc ? { sessionId: `session-${id}` } : { cwd: a, model: { provider } } });
await handlers.beforeRequest({}, hookApi(1, "openai"), context);
assert.throws(() => bridge.take("session-1"), /no trusted/, "other providers never receive a mapping");
await handlers.beforeRequest({}, hookApi(1, "claude-test"), context);
handlers.afterResponse({}, hookApi(1, "claude-test"));
assert.throws(() => bridge.take("session-1"), /no trusted/, "unused mapping is cleared after response");
try {
  opened = await openHarness(join(root, "db.sqlite"), "", undefined, { claudeEnabled: true });
  const { harness, models, plugins } = opened;
  const stats = () => plugins.get("pi-claude-cli").sessionStats();
  assert.deepEqual(await stats(), { liveProcesses: 0, activeTurns: 0, parkedIdle: 0, parkedWaitingForTools: 0 });
  const model = models.getModels("pi-claude-cli")[0];
  assert.ok(model, "Claude provider registers models");
  const missing = await models.completeSimple(model, { messages: [{ role: "user", content: "hello", timestamp: Date.now() }] }, { sessionId: "unmapped" });
  assert.equal(missing.stopReason, "error");
  assert.match(missing.errorMessage, /no trusted/);
  await assert.rejects(readFile(log), { code: "ENOENT" });
  const conversation = await harness.createConversation({ ownership: { kind: "ownerless" }, agent: {
    cwd: a, model: { provider: model.provider, modelId: model.id },
  } }, context);
  const send = async (target, id, content = "hello", expected = "PORT-OK") => {
    const before = (await target.entries({ order: "ascending" }, 100, undefined, context)).items;
    const old = new Set(before.map((entry) => entry.id));
    await target.submit({ type: "input", requestId: id, content }, context);
    await target.waitForIdle(context);
    const entries = (await target.entries({ order: "ascending" }, 100, undefined, context)).items.filter((entry) => !old.has(entry.id));
    assert.ok(entries.some((entry) => entry.kind === "pi.assistant" && JSON.stringify(entry).includes(expected)), JSON.stringify(entries));
    return entries;
  };
  await writeFile(join(a, "stub.txt"), "hello from tool");
  await send(conversation, "one");
  const toolEntries = await send(conversation, "tool", "tool:read", "TOOL-DONE");
  assert.ok(JSON.stringify(toolEntries).includes("TOOL-DONE"), JSON.stringify(toolEntries));
  assert.equal((await readFile(log, "utf8")).trim().split("\n").length, 1, "warm process survives tool handoff");
  assert.deepEqual(await stats(), { liveProcesses: 1, activeTurns: 0, parkedIdle: 1, parkedWaitingForTools: 0 });
  await conversation.configure({ cwd: b }, context);
  await send(conversation, "two");
  const launches = (await readFile(log, "utf8")).trim().split("\n");
  assert.deepEqual(launches, [await realpath(a), await realpath(b)]);
  const conversationId = conversation.id;
  await opened.harness.close(context);
  await opened.closeClaude();
  // Simulate a database from before pi.provider was introduced.
  const db = new DatabaseSync(join(root, "db.sqlite"));
  db.prepare("DELETE FROM document_revisions WHERE document_id IN (SELECT id FROM documents WHERE json_extract(kind, '$') = 'pi.provider' AND owner_id = ?)").run(conversationId);
  const deleted = db.prepare("DELETE FROM documents WHERE json_extract(kind, '$') = 'pi.provider' AND owner_id = ?").run(conversationId);
  assert.equal(deleted.changes, 1);
  db.close();
  opened = await openHarness(join(root, "db.sqlite"), "", undefined, { claudeEnabled: true });
  assert.ok((await opened.harness.snapshot(ProviderDoc, conversationId, context))?.sessionId, "legacy provider session migrated before resume");
  const reopened = await opened.harness.conversation(conversationId, context);
  await send(reopened, "restart");
  const after = (await readFile(log, "utf8")).trim().split("\n");
  assert.deepEqual(after, [await realpath(a), await realpath(b), await realpath(b)]);
  const other = await opened.harness.createConversation({ ownership: { kind: "ownerless" }, agent: {
    cwd: a, model: { provider: model.provider, modelId: model.id },
  } }, context);
  await Promise.all([send(reopened, "parallel-a"), send(other, "parallel-b")]);
  const parallel = (await readFile(log, "utf8")).trim().split("\n");
  assert.equal(parallel.at(-1), await realpath(a), "other conversation launches in its own directory");
  const pid = Number((await readFile(pidFile, "utf8")).trim());
  await opened.setClaudeEnabled(false);
  assert.equal(opened.models.getModels("pi-claude-cli").length, 0, "disabled models are unavailable");
  for (let i = 0; i < 100; i++) {
    try { process.kill(pid, 0); await new Promise((resolve) => setTimeout(resolve, 20)); }
    catch (error) { if (error.code === "ESRCH") break; throw error; }
  }
  assert.throws(() => process.kill(pid, 0), { code: "ESRCH" }, "disabling retires parked processes");
  assert.equal((await other.agent(context)).model.provider, "pi-claude-cli", "disabling preserves selection");
  await opened.setClaudeEnabled(true);
  await send(other, "reenabled");
  // Pool limits apply while processes run: past the cap, the longest-parked
  // conversation loses its process and its next turn launches another.
  const sessions = opened.plugins.get("pi-claude-cli").sessions;
  assert.deepEqual(sessions.get(), { keepaliveMinutes: 60, toolWaitMinutes: 60, maxParked: 3, dateOverride: false }, "default pool limits");
  const launched = async () => (await readFile(log, "utf8")).trim().split("\n").length;
  await sessions.set({ keepaliveMinutes: 60, toolWaitMinutes: 60, maxParked: 1 });
  const base = await launched();
  await send(reopened, "pool-a");
  await send(other, "pool-b");
  await send(reopened, "pool-c");
  assert.equal(await launched(), base + 3, "with one parked process, alternating conversations evict each other");
  await sessions.set({ keepaliveMinutes: 60, toolWaitMinutes: 60, maxParked: 2 });
  await send(other, "pool-d");
  await send(reopened, "pool-e");
  await send(other, "pool-f");
  assert.equal(await launched(), base + 4, "with two parked processes, both conversations stay warm");
  await sessions.set({ keepaliveMinutes: 60, toolWaitMinutes: 60, maxParked: 1 });
  await send(other, "pool-g");
  assert.equal(await launched(), base + 4, "lowering the cap keeps the most recently parked process");
  await send(reopened, "pool-h");
  assert.equal(await launched(), base + 5, "lowering the cap retires the longest-parked process at once");
  console.log("Claude provider: fail-closed mapping, tool handoff, cwd switch, restart, concurrent conversations and pool limits passed");
} finally {
  if (opened) { await opened.harness.close(context); await opened.closeClaude(); }
  await rm(root, { recursive: true, force: true });
}
