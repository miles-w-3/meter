import { randomBytes, randomUUID, timingSafeEqual } from "node:crypto";
import { existsSync } from "node:fs";
import { chmod, lstat, mkdir, readFile, realpath, rename, stat, unlink, writeFile } from "node:fs/promises";
import { createServer, type IncomingMessage, type ServerResponse } from "node:http";
import type { AddressInfo } from "node:net";
import { homedir } from "node:os";
import { dirname, isAbsolute, join, resolve } from "node:path";
import { DatabaseSync } from "node:sqlite";
import { BACKGROUND_CONTEXT as context } from "@earendil-works/chord/context";
import { AgentDoc, InboxDoc, LiveDoc } from "@earendil-works/pi-durable";
import { getSupportedThinkingLevels } from "@earendil-works/pi-ai/models";
import { ModelRuntime } from "@earendil-works/pi-coding-agent";
import type { Conversation, ConversationId, Cursor, EntryId } from "@earendil-works/pi-durable";
import { WebSocket, WebSocketServer } from "ws";
import { abortInput, conversationFrame, createInput, cwdInput, defaultSettings, layout, projectInput, projectRenameInput, selectionChange, submitInput, toolSettingsChange } from "../contracts/schemas.ts";
import type { z, ZodType } from "zod";
import { abortTarget, admit, contextTokens, entryText, entryImages, historyCursor, openHarness, project, projectEntries, RequestConflict, steerQueued, recallQueued } from "./durable.ts";
import type { Provider } from "./durable.ts";

import { WebSearch } from "./tools/web-search.ts";
import { discoverHistoryTree } from "./tree.ts";
import { dictation } from "./dictation.ts";
import { InvalidLayout, LayoutConflict, Workspaces } from "./workspaces.ts";

process.umask(0o077);
const configHome = process.platform === "darwin" ? join(homedir(), "Library", "Application Support") :
  process.env.XDG_CONFIG_HOME || join(homedir(), ".config");
const data = resolve(process.env.PI_WORKSPACE_HOME || join(configHome, "pi-workspace"));
const provider = process.env.PI_PROVIDER ?? "openai";
if (!provider || provider.length > 160) throw new Error("Invalid PI_PROVIDER");
await mkdir(data, { recursive: true, mode: 0o700 });
const directory = await lstat(data);
if (!directory.isDirectory() || directory.uid !== process.getuid?.() || (directory.mode & 0o077)) {
  throw new Error("Workspace data directory must be private and owned by the current user: " + data);
}
const lock = new DatabaseSync(join(data, "writer-lock.sqlite"));
lock.exec("BEGIN EXCLUSIVE");
const transport = process.env.PI_TRANSPORT || "unix";
if (transport !== "unix" && transport !== "tcp") throw new Error("PI_TRANSPORT must be unix or tcp");
const tcp = transport === "tcp";
const token = tcp ? randomBytes(32).toString("hex") : "";
const socketPath = join(data, "workspace.sock");
const endpointPath = join(data, "endpoint.json");
if (!tcp) {
  const previous = await lstat(socketPath).catch((error) => {
    if (error.code !== "ENOENT") throw error;
    return undefined;
  });
  if (previous) {
    if (!previous.isSocket()) throw new Error("Refusing to replace a non-socket path");
    await unlink(socketPath);
  }
}
function allowed(req: IncomingMessage) {
  if (req.headers.origin) return false;
  if (!tcp) return true;
  const expected = Buffer.from("Bearer " + token);
  const actual = Buffer.from(req.headers.authorization ?? "");
  return actual.length === expected.length && timingSafeEqual(actual, expected);
}
const idPath = join(data, "server-id");
let id = (await readFile(idPath, "utf8").catch((error) => {
  if (error.code !== "ENOENT") throw error;
  return "";
})).trim();
if (!id) {
  id = randomUUID();
  const temp = idPath + "." + randomUUID();
  await writeFile(temp, id + "\n", { flag: "wx" });
  await rename(temp, idPath);
}
const workspaces = new Workspaces(join(data, "app.sqlite"));
const webSearch = new WebSearch(() => workspaces.toolSettings());
const { harness, model, models, refreshTools } = await openHarness(join(data, "durable.sqlite"), provider as Provider, webSearch);
const generation = randomUUID();
const capabilities = ["history", "retry-submit", "steer", "followup", "abort-target", "catalog", "workspace-layouts", "model-inventory", "model-defaults", "conversation-models", "tool-settings"];
const dictationDir = process.env.PI_DICTATION_DIR ||
  (existsSync(join(data, "meter-dictation")) ? data : dirname(process.execPath));
const voice = dictation(dictationDir, data);
declare const METER_GIT_REVISION: string;
declare const METER_GIT_DIRTY: boolean;
const revision = typeof METER_GIT_REVISION === "string" ? METER_GIT_REVISION : undefined;
const dirty = typeof METER_GIT_DIRTY === "boolean" ? METER_GIT_DIRTY : undefined;
harness.resume();

function reply(res: ServerResponse, status: number, body?: unknown) {
  if (body === undefined) res.writeHead(status).end();
  else res.writeHead(status, { "content-type": "application/json" }).end(JSON.stringify(body));
}

class Problem extends Error {
  readonly status: number;
  readonly code: string;
  constructor(status: number, code: string, message: string) {
    super(message);
    this.status = status;
    this.code = code;
  }
}
async function parseBody<T>(req: IncomingMessage, schema: ZodType<T>, maxBytes = 224_000): Promise<T> {
  if (req.headers["content-type"]?.split(";")[0] !== "application/json") {
    throw new Problem(400, "invalid_request", "Expected a JSON body");
  }
  const chunks: Buffer[] = [];
  let size = 0;
  for await (const chunk of req.iterator({ destroyOnReturn: false })) {
    size += chunk.length;
    if (size > maxBytes) throw new Problem(400, "invalid_request", "Request body too large");
    chunks.push(chunk);
  }
  try { return schema.parse(JSON.parse(Buffer.concat(chunks).toString("utf8"))); }
  catch { throw new Problem(400, "invalid_request", "Invalid JSON request fields"); }
}

async function conversationFor(value: string) {
  const number = Number(value);
  if (!/^[1-9][0-9]*$/.test(value) || !Number.isSafeInteger(number)) {
    throw new Problem(404, "not_found", "Conversation not found");
  }
  const conversation = await harness.conversation(number as ConversationId, context);
  if (!conversation) throw new Problem(404, "not_found", "Conversation not found");
  return conversation;
}

const titles = new Map<string, string>();
async function titleFor(conversation: Conversation, id: string) {
  if (titles.has(id)) return titles.get(id);
  const history = await conversation.entries({ order: "ascending" }, 24, undefined, context);
  const first = history.items.filter((entry) => entry.kind === "pi.user")
    .flatMap((entry) => entry.model ?? []).find((message) => message.role === "user");
  if (!first) return;
  const plain = typeof first.content === "string" ? first.content : first.content
    .flatMap((block) => block.type === "text" ? [block.text] : []).join(" ");
  const line = (plain.split(/\r?\n/u).map((line) => line.trim())
    .find((line) => line && (provider !== "fixture" || !line.startsWith("fixture:"))) ?? "")
    .replace(/\s+/gu, " ");
  if (!line) return;
  const chars = [...line];
  const title = chars.length > 48 ? chars.slice(0, 47).join("").replace(/\s+\S*$/u, "") + "…" : line;
  titles.set(id, title);
  return title;
}

async function metadata(conversation: Conversation) {
  const agent = await conversation.agent(context);
  const id = String(conversation.id);
  const tail = await conversation.entries({ order: "descending" }, 8, undefined, context);
  const lastActivity = tail.items.flatMap((entry) => [...(entry.model ?? [])].reverse())
    .find((message) => Number.isSafeInteger(message.timestamp) && message.timestamp >= 0)?.timestamp;
  return { id, cwd: agent.cwd, lastActivity, title: workspaces.conversationTitle(id) ?? await titleFor(conversation, id), projectId: workspaces.projectFor(id, agent.cwd ?? ""), model: agent.model ? agent.model.provider + "/" + agent.model.modelId : "unconfigured", selection: { model: agent.model ?? null, thinkingLevel: agent.thinkingLevel } };
}

async function canonicalCwd(cwd: string) {
  if (!isAbsolute(cwd)) throw new Problem(400, "invalid_cwd", "Use an absolute execution-host directory");
  try {
    const path = await realpath(cwd);
    if ((await stat(path)).isDirectory()) return path;
  } catch { /* Invalid or inaccessible directory. */ }
  throw new Problem(400, "invalid_cwd", "Working directory is not accessible");
}

async function history(conversation: Conversation, before: string | null) {
  let maxEntryId: EntryId | undefined;
  if (before !== null) {
    if (!/^[a-zA-Z0-9_-]{1,32}$/.test(before)) throw new Problem(400, "invalid_cursor", "Invalid history cursor");
    const value = Number(Buffer.from(before, "base64url").toString("ascii"));
    if (!Number.isSafeInteger(value) || value < 1 || historyCursor(value) !== before) {
      throw new Problem(400, "invalid_cursor", "Invalid history cursor");
    }
    if (value === 1) return { entries: [] };
    maxEntryId = (value - 1) as EntryId;
  }
  const page = await conversation.entries({ maxEntryId, order: "descending" }, 48, undefined, context);
  const entries = [...page.items].reverse();
  return { entries: projectEntries(entries).projected, before: page.next && entries.length ? historyCursor(entries[0].id) : undefined };
}

// Serialize cwd changes with input admission for the same conversation.
const controls = new Map<string, Promise<unknown>>();
async function control<T>(id: string, operation: () => Promise<T>) {
  const previous = controls.get(id);
  const current = (previous?.catch(() => undefined) ?? Promise.resolve()).then(operation);
  controls.set(id, current);
  try { return await current; }
  finally { if (controls.get(id) === current) controls.delete(id); }
}

function validateSelection(value: { model: { provider: string; modelId: string }; thinkingLevel: z.infer<typeof selectionChange>["thinkingLevel"] }) {
  const selected = models.getModel(value.model.provider, value.model.modelId);
  if (!selected) throw new Problem(400, "invalid_model", "Model is not in the current catalog");
  if (!getSupportedThinkingLevels(selected).includes(value.thinkingLevel)) throw new Problem(400, "invalid_thinking", "Thinking level is not supported by this model");
}

function currentDefaults() {
  const saved = workspaces.defaults();
  if (saved) return { ...saved, source: "saved" as const };
  return { model: model ?? null, thinkingLevel: null, source: model ? provider === "fixture" ? "fixture" as const : "environment" as const : "unconfigured" as const };
}

async function inventory() {
  const providers = await Promise.all(models.getProviders().map(async (item) => {
    const methods = [
      ...(item.auth.apiKey ? [{ type: "api_key" as const, name: item.auth.apiKey.name, interactive: !!item.auth.apiKey.login }] : []),
      ...(item.auth.oauth ? [{ type: "oauth" as const, name: item.auth.oauth.name, interactive: true }] : []),
    ];
    try {
      const check = await models.checkAuth(item.id, { signal: AbortSignal.timeout(3000) });
      return { id: item.id, name: item.name, methods, status: check ? "configured" : "unconfigured", source: check?.source };
    } catch {
      return { id: item.id, name: item.name, methods, status: "unknown" };
    }
  }));
  return { providers, models: models.getModels().map((item) => ({
    provider: item.provider, modelId: item.id, name: item.name, contextWindow: item.contextWindow,
    reasoning: item.reasoning, thinkingLevels: getSupportedThinkingLevels(item),
  })) };
}

let stopping = false;
async function route(req: IncomingMessage, res: ServerResponse) {
  if (!allowed(req)) throw new Problem(401, "unauthorized", "Unauthorized local endpoint");
  const url = new URL(req.url ?? "/", "http://workspace");
  if (url.pathname === "/v1/server" && req.method === "GET") {
    const status = voice.status;
    return reply(res, 200, {
      id, protocol: "v1", ready: true, provider, revision, dirty, generation, transport, dictation: status,
      capabilities: status === "ready" ? [...capabilities, "dictation"] : capabilities,
    });
  }
  if (stopping) throw new Problem(503, "server_stopping", "Backend is shutting down");
  if (url.pathname === "/v1/server/stop" && req.method === "POST") {
    res.once("finish", shutdown);
    return reply(res, 202);
  }
  if (url.pathname === "/v1/models" && req.method === "GET") {
    if (models instanceof ModelRuntime) {
      await models.refresh({ allowNetwork: false });
      const error = models.getError();
      if (error) throw new Problem(500, "model_refresh_failed", error);
    }
    return reply(res, 200, await inventory());
  }
  if (url.pathname === "/v1/settings/tools") {
    if (req.method === "GET") return reply(res, 200, webSearch.status());
    if (req.method === "PATCH") {
      workspaces.updateToolSettings(await parseBody(req, toolSettingsChange));
      refreshTools();
      return reply(res, 200, webSearch.status());
    }
  }
  if (url.pathname === "/v1/settings/defaults") {
    if (req.method === "GET") return reply(res, 200, currentDefaults());
    if (req.method === "PUT") {
      const value = await parseBody(req, defaultSettings);
      if (!value.model && value.thinkingLevel) throw new Problem(400, "invalid_model", "Choose a model before setting thinking level");
      if (value.model) {
        const selected = models.getModel(value.model.provider, value.model.modelId);
        if (!selected) throw new Problem(400, "invalid_model", "Model is not in the current catalog");
        if (value.thinkingLevel && !getSupportedThinkingLevels(selected).includes(value.thinkingLevel)) {
          throw new Problem(400, "invalid_thinking", "Thinking level is not supported by this model");
        }
      }
      workspaces.saveDefaults(value);
      return reply(res, 200, currentDefaults());
    }
  }
  if (url.pathname === "/v1/catalog" && req.method === "GET") {
    const conversations = [];
    let cursor: Cursor | undefined;
    do {
      const page = await harness.commit((tx) => tx.scanConversations({ order: "descending" }, 80, cursor), context);
      for (const row of page.items) {
        const conversation = await harness.conversation(row.id, context);
        if (conversation) conversations.push(await metadata(conversation));
      }
      cursor = page.next;
    } while (cursor);
    return reply(res, 200, { projects: workspaces.projects(), conversations });
  }
  if (url.pathname === "/v1/directories/resolve" && req.method === "POST") {
    const { cwd } = await parseBody(req, cwdInput);
    return reply(res, 200, { cwd: await canonicalCwd(cwd) });
  }
  if (url.pathname === "/v1/projects" && req.method === "POST") {
    const { cwd, name } = await parseBody(req, projectInput);
    const created = workspaces.register(await canonicalCwd(cwd), name);
    if (!created) throw new Problem(409, "project_exists", "A project already uses this directory. Select it or rename it instead.");
    return reply(res, 201, created);
  }
  const projectPath = url.pathname.match(/^\/v1\/projects\/([a-zA-Z0-9_-]{1,64})$/);
  if (projectPath && req.method === "PUT") {
    const { name } = await parseBody(req, projectRenameInput);
    const updated = workspaces.renameProject(projectPath[1], name);
    if (!updated) throw new Problem(404, "not_found", "Project not found");
    return reply(res, 200, updated);
  }
  const workspace = url.pathname.match(/^\/v1\/workspaces\/([a-zA-Z0-9_-]{1,64})$/);
  if (workspace) {
    if (req.method === "GET") return reply(res, 200, workspaces.get(workspace[1]));
    if (req.method === "PUT") {
      const requested = await parseBody(req, layout);
      return reply(res, 200, await workspaces.save(workspace[1], requested, conversationFor));
    }
  }
  if (url.pathname === "/v1/conversations" && req.method === "POST") {
    const { cwd, projectId, selection } = await parseBody(req, createInput);
    if (selection) validateSelection(selection);
    if (projectId && !workspaces.projectExists(projectId)) throw new Problem(404, "not_found", "Project not found");
    const defaults = currentDefaults();
    const conversation = await harness.createConversation({
      ownership: { kind: "ownerless" }, agent: { cwd: await canonicalCwd(cwd), model: selection?.model ?? defaults.model ?? undefined, thinkingLevel: selection?.thinkingLevel ?? defaults.thinkingLevel ?? undefined },
    }, context);
    if (projectId) workspaces.associate(String(conversation.id), projectId);
    return reply(res, 201, await metadata(conversation));
  }
  const treePath = url.pathname.match(/^\/v1\/conversations\/([1-9][0-9]*)\/tree$/);
  if (treePath && req.method === "GET") {
    const conversation = await conversationFor(treePath[1]);
    const tree = await discoverHistoryTree(harness, conversation.id);
    if (!tree) throw new Problem(404, "not_found", "Conversation not found");
    return reply(res, 200, tree);
  }
  const titlePath = url.pathname.match(/^\/v1\/conversations\/([1-9][0-9]*)\/title$/);
  if (titlePath && req.method === "PUT") {
    const { name } = await parseBody(req, projectRenameInput);
    const conversation = await conversationFor(titlePath[1]);
    workspaces.renameConversation(titlePath[1], name);
    return reply(res, 200, await metadata(conversation));
  }
  const imagePath = url.pathname.match(/^\/v1\/conversations\/([^/]+)\/entries\/([1-9][0-9]*)\/images$/);
  if (req.method === "GET" && imagePath) {
    const id = Number(imagePath[2]) as EntryId;
    if (!Number.isSafeInteger(id)) throw new Problem(400, "invalid_entry", "Invalid entry ID");
    const conversation = await conversationFor(imagePath[1]);
    const page = await conversation.entries({ minEntryId: id, maxEntryId: id }, 1, undefined, context);
    const entry = page.items.find((item) => item.id === id);
    if (!entry) throw new Problem(404, "not_found", "Message not found");
    return reply(res, 200, { images: entryImages(entry) });
  }
  const outputPath = url.pathname.match(/^\/v1\/conversations\/([^/]+)\/entries\/([1-9][0-9]*)\/output$/);
  if (req.method === "GET" && outputPath) {
    const id = Number(outputPath[2]) as EntryId;
    if (!Number.isSafeInteger(id)) throw new Problem(400, "invalid_entry", "Invalid entry ID");
    const conversation = await conversationFor(outputPath[1]);
    const page = await conversation.entries({ minEntryId: id, maxEntryId: id }, 1, undefined, context);
    const result = page.items.find((entry) => entry.id === id && entry.model?.some((message) => message.role === "toolResult"));
    if (!result) throw new Problem(404, "not_found", "Tool result not found");
    return reply(res, 200, { text: entryText(result) });
  }
  const queued = url.pathname.match(/^\/v1\/conversations\/([1-9][0-9]*)\/queue\/([1-9][0-9]*)\/(steer|recall)$/);
  if (queued && req.method === "POST") {
    const conversation = await conversationFor(queued[1]);
    if (queued[3] === "recall") {
      const result = await control(queued[1], () => recallQueued(conversation, queued[2]));
      if (!result) throw new Problem(409, "stale_queue", "Message is no longer queued");
      return reply(res, 200, result);
    }
    if (!await control(queued[1], () => steerQueued(conversation, queued[2]))) {
      throw new Problem(409, "stale_queue", "Message is no longer queued on an active run");
    }
    return reply(res, 202);
  }
  const match = url.pathname.match(/^\/v1\/conversations\/([^/]+)(?:\/(entries|messages|abort|cwd|model))?$/);
  if (match) {
    const conversation = await conversationFor(match[1]);
    if (req.method === "GET" && !match[2]) return reply(res, 200, await metadata(conversation));
    if (req.method === "GET" && match[2] === "entries") return reply(res, 200, await history(conversation, url.searchParams.get("before")));
    if (req.method === "PUT" && match[2] === "cwd") {
      const input = await parseBody(req, cwdInput);
      const cwd = await canonicalCwd(input.cwd);
      const updated = await control(match[1], async () => {
        const live = await harness.snapshot(LiveDoc, conversation.id, context);
        if (live?.run) throw new Problem(409, "busy", "Wait for the conversation to finish before changing its directory");
        const before = await metadata(conversation);
        if (before.projectId) workspaces.associate(before.id, before.projectId);
        await conversation.configure({ cwd }, context);
        return metadata(conversation);
      });
      return reply(res, 200, updated);
    }
    if (req.method === "PUT" && match[2] === "model") {
      const input = await parseBody(req, selectionChange);
      validateSelection(input);
      const updated = await control(match[1], async () => {
        await conversation.commit(async (tx) => {
          const live = await tx.doc(LiveDoc, conversation.id);
          const inbox = await tx.doc(InboxDoc, conversation.id);
          if (live.run || inbox.items.length) throw new Problem(409, "busy", "Wait for the conversation and queued messages to finish");
          const agent = await tx.doc(AgentDoc, conversation.id);
          if ((agent.model?.provider ?? null) !== (input.expected.model?.provider ?? null) ||
              (agent.model?.modelId ?? null) !== (input.expected.model?.modelId ?? null) ||
              (agent.thinkingLevel ?? "off") !== input.expected.thinkingLevel) {
            throw new Problem(409, "selection_conflict", "Model selection changed in another window");
          }
          agent.model = input.model;
          agent.thinkingLevel = input.thinkingLevel;
        }, context);
        return metadata(conversation);
      });
      return reply(res, 200, updated);
    }
    if (req.method === "POST" && match[2] === "messages") {
      const input = await parseBody(req, submitInput, 11_300_000);
      const submissionId = await control(match[1], async () => {
        const agent = await conversation.agent(context);
        return admit(conversation, input, async () => {
          if (!agent.model) throw new Problem(409, "model_unconfigured", "Set a default model and create a new conversation");
          if (!models.getModel(agent.model.provider, agent.model.modelId)) throw new Problem(409, "model_unavailable", "The conversation's model is not in the current catalog");
        });
      });
      return reply(res, 202, { submissionId });
    }
    if (req.method === "POST" && match[2] === "abort") {
      const { target } = await parseBody(req, abortInput);
      if (!await abortTarget(harness, conversation, target)) throw new Problem(409, "stale_run", "The active run changed");
      return reply(res, 202);
    }
  }
  throw new Problem(404, "not_found", "Unknown endpoint");
}

const server = createServer({ requestTimeout: 7000 }, (req, res) => {
  void route(req, res).catch((error: unknown) => {
    const known = error instanceof Problem ? error :
      error instanceof LayoutConflict ? new Problem(409, "layout_conflict", error.message) :
      error instanceof InvalidLayout ? new Problem(400, "invalid_layout", error.message) :
      error instanceof RequestConflict ? new Problem(409, "request_conflict", error.message) :
      error instanceof Error && error.constructor.name === "ConversationBusy" ? new Problem(409, "busy", "Conversation already running") :
      error instanceof Error && error.constructor.name === "ConversationIdle" ? new Problem(409, "idle", "Steering requires an active run") : undefined;
    if (!known) console.error(error);
    if (!res.headersSent) reply(res, known?.status ?? 500, { code: known?.code ?? "internal", message: known?.message ?? "Server error" });
  });
});
const sockets = new WebSocketServer({ noServer: true, maxPayload: 512 });
const voiceSockets = new WebSocketServer({ noServer: true, maxPayload: voice.maxAudioFrame });
const responsive = new WeakSet<WebSocket>();
const heartbeat = setInterval(() => {
  for (const ws of sockets.clients) {
    if (!responsive.delete(ws)) ws.terminate();
    else ws.ping();
  }
}, 10_000);
heartbeat.unref();

server.on("upgrade", (req, socket, head) => {
  if (!allowed(req) || stopping) return socket.destroy();
  if (req.url === "/v1/dictation") {
    voiceSockets.handleUpgrade(req, socket, head, voice.attach);
    return;
  }
  const match = req.url?.match(/^\/v1\/conversations\/([1-9][0-9]*)\/stream$/);
  if (!match) return socket.destroy();
  sockets.handleUpgrade(req, socket, head, (ws) => {
    ws.on("error", () => ws.terminate());
    ws.on("pong", () => responsive.add(ws));
    responsive.add(ws);
    void attach(ws, match[1]).catch((error) => {
      if (!(error instanceof Problem)) console.error(error);
      ws.close(error instanceof Problem ? 1008 : 1011, error instanceof Problem ? error.message : "Watch failed");
    });
  });
});

async function attach(ws: WebSocket, id: string) {
  const conversation = await conversationFor(id);
  if (ws.readyState !== WebSocket.OPEN) return;
  const watch = await conversation.watch(context, { min: 48, max: 96 });
  ws.once("close", () => { void watch.stop(); });
  if (ws.readyState !== WebSocket.OPEN) return watch.stop();
  let revision = 0;
  const send = (type: "snapshot" | "update", value: ReturnType<typeof project>) => {
    if (ws.readyState !== WebSocket.OPEN) return;
    const frame = JSON.stringify(conversationFrame.parse({ type, generation, revision: revision++, view: value }));
    if (Buffer.byteLength(frame) > 2_500_000 || ws.bufferedAmount > 2_500_000) return ws.terminate();
    ws.send(frame);
  };
  let committed: EntryId | undefined;
  let usage: { tokens: number; window: number } | undefined;
  const frame = async (value: typeof watch.value) => {
    const last = value.entries.at(-1)?.id;
    const projected = project(value);
    const model = projected.selection?.model;
    const window = model && models.getModel(model.provider, model.modelId)?.contextWindow;
    if (window && (!usage || usage.window !== window || last !== committed)) {
      usage = { tokens: contextTokens(await conversation.context(context)), window };
      committed = last;
    } else if (!window) usage = undefined;
    return { ...projected, contextUsage: usage };
  };
  send("snapshot", await frame(watch.value));
  if (ws.readyState !== WebSocket.OPEN) return watch.stop();
  watch.start(async (call) => {
    if (call.kind === "value") send("update", await frame(call.value));
    else ws.close();
  });
}

function shutdown() {
  if (stopping) return;
  stopping = true;
  clearInterval(heartbeat);
  for (const ws of sockets.clients) ws.terminate();
  voice.close();
  void harness.close(context).then(async () => {
    if (models instanceof ModelRuntime) await models.settleCredentialWrites();
    workspaces.db.close();
    lock.close();
    if (tcp) await unlink(endpointPath).catch((error) => {
      if (error.code !== "ENOENT") console.error(error);
    });
    server.close();
    process.exit(0);
  }, (error) => {
    console.error(error);
    process.exit(1);
  });
}
process.once("SIGTERM", shutdown);
process.once("SIGINT", shutdown);
await new Promise<void>((ready, fail) => {
  server.once("error", fail);
  if (tcp) server.listen(0, "127.0.0.1", ready);
  else server.listen(socketPath, ready);
});
if (tcp) {
  const { port } = server.address() as AddressInfo;
  const temp = endpointPath + "." + generation;
  await writeFile(temp, JSON.stringify({ port, token }), { mode: 0o600, flag: "wx" });
  await rename(temp, endpointPath);
} else {
  await chmod(socketPath, 0o600);
}
console.log("Workspace ready", transport, provider);
