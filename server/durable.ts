import { mermaidCatalogTool } from "./tools/mermaid-catalog.ts";
import { mermaidPreview } from "./fixtures/mermaid-preview.ts";
import { join } from "node:path";
import { BACKGROUND_CONTEXT as context } from "@earendil-works/chord/context";
import { calculateContextTokens, estimateMessageTokens } from "@earendil-works/pi-ai/utils/estimate";
import { createModels } from "@earendil-works/pi-ai/models";
import type { FauxResponseFactory } from "@earendil-works/pi-ai/providers/faux";
import { fauxAssistantMessage, fauxProvider, fauxText, fauxThinking, fauxToolCall } from "@earendil-works/pi-ai/providers/faux";
import { ModelRuntime } from "@earendil-works/pi-coding-agent";
import { createRegistry, defineDocFamily, defineExtension, Harness, InboxDoc, LiveDoc, ProviderDoc } from "@earendil-works/pi-durable";
import { ClaudeCwdBridge } from "./claude-code/bridge.ts";
import { ProviderPlugins } from "./provider-plugins.ts";
import type { AgentState, Conversation, ContextView, ConversationView, EntryId, EntryRecord, Harness as DurableHarness, InboxState, LiveState } from "@earendil-works/pi-durable";
import { NodeExecutionEnv } from "@earendil-works/pi-durable/env/node";
import { openNodeSqliteStorage } from "@earendil-works/pi-durable/storage/sqlite/node";
import { createBashTool, createEditTool, createReadTool, createWriteTool } from "@earendil-works/pi-durable/tools";
import { createHash } from "node:crypto";
import type { z } from "zod";
import { contextUsage, submitInput, view } from "../contracts/schemas.ts";

import type { WebSearch } from "./tools/web-search.ts";
import { webFetchTool } from "./tools/web-fetch.ts";
import { snapshot, compare } from "./tools/file-diffs.ts";

const answer = () => fauxAssistantMessage([fauxText("Fixture completed after running the coding tools.")]);
const tool = (name: string, args: Parameters<typeof fauxToolCall>[1]) =>
  fauxAssistantMessage([fauxToolCall(name, args, { id: "fixture_" + name })], { stopReason: "toolUse" });

function fixtureProvider() {
  const faux = fauxProvider({ tokensPerSecond: 120, tokenSize: { min: 4, max: 4 } });
  faux.setResponses(Array.from({ length: 4096 }, (): FauxResponseFactory => (request) => {
    const lastUser = request.messages.findLastIndex((message) => message.role === "user");
    const messages = request.messages.slice(lastUser);
    const user = messages[0];
    const prompt = user?.role === "user" && typeof user.content === "string" ? user.content : JSON.stringify(user);
    const results = messages.filter((message) => message.role === "toolResult");
    const preview = /^fixture:mermaid:(tiny|wide|tall|square|wide-data)$/.exec(prompt.trim());
    if (preview) return fauxAssistantMessage([fauxText(mermaidPreview[preview[1]])]);
    if (prompt === "fixture:thinking") return fauxAssistantMessage([fauxThinking("Let me consider the request and check the relevant details.\n\nFirst, I should understand what the user is asking and identify the constraints. The answer should be clear, direct, and grounded in the available information.\n\nNext, I can compare the possible approaches:\n- Keep the explanation concise.\n- Include an example where it helps.\n- Call out any uncertainty rather than making assumptions.\n\nWith those points in mind, I can put together a useful response."), fauxText("Here is the answer after considering it.")]);
    if (prompt.includes("fixture:demo")) return fauxAssistantMessage([fauxText("desktop reconnect output ".repeat(200))]);
    if (prompt.includes("fixture:slow")) return fauxAssistantMessage([fauxText("streamed model output ".repeat(45))]);
    if (prompt === "fixture:web-fetch" && !results.length) {
      return tool("web_fetch", { url: "http://127.0.0.1/" });
    }
    if (prompt === "fixture:web-search" && !results.length) {
      return tool("web_search", { query: "Meter search fixture", limit: 2 });
    }
    if (prompt.includes("fixture:bash-diff")) {
      if (!results.length) return tool("bash", { command: "printf 'updated\\n' > diff-fixture.txt" });
      return answer();
    }
    if (prompt.includes("fixture:tools")) {
      if (!results.length) return tool("read", { path: "notes.txt" });
      if (results.length === 1) return tool("write", { path: "result.txt", content: "written by Durable\n" });
    }
    if (prompt.includes("fixture:tools-slow")) return fauxAssistantMessage([fauxText("Fixture completed after running the coding tools. " + "streamed model output ".repeat(45))]);
    return answer();
  }));
  // The preview can deliberately simulate a cache miss without touching real providers.
  const provider = faux.provider;
  const fixture: typeof provider = {
    ...provider,
    streamSimple(model, request, options) {
      const last = request.messages.findLast((message) => message.role === "user");
      const content = last?.role === "user" ? (typeof last.content === "string" ? last.content : last.content.flatMap((block) => block.type === "text" ? [block.text] : []).join("")) : "";
      const miss = content.trim() === "fixture:cache-miss";
      return provider.streamSimple(model, request, miss ? { ...options, cacheRetention: "none" } : options);
    },
  };
  return fixture;
}

class CrashProbeEnv extends NodeExecutionEnv {
  override async openBinaryReader(...args: Parameters<NodeExecutionEnv["openBinaryReader"]>) {
    if (process.env.PI_CRASH_POINT === "read" && args[0].endsWith("/notes.txt")) {
      process.kill(process.pid, "SIGKILL");
    }
    return super.openBinaryReader(...args);
  }

  override async writeFile(...args: Parameters<NodeExecutionEnv["writeFile"]>) {
    const result = await super.writeFile(...args);
    if (process.env.PI_CRASH_POINT === "write" && args[0].endsWith("/result.txt")) {
      process.kill(process.pid, "SIGKILL");
    }
    return result;
  }
}

class LiveProbeEnv extends NodeExecutionEnv {
  override openBinaryReader(...args: Parameters<NodeExecutionEnv["openBinaryReader"]>) {
    if (args[0] !== join(this.cwd, "notes.txt")) throw new Error("Live probe can only read notes.txt");
    return super.openBinaryReader(...args);
  }

  override writeFile(...args: Parameters<NodeExecutionEnv["writeFile"]>) {
    if (args[0] !== join(this.cwd, "result.txt")) throw new Error("Live probe can only write result.txt");
    return super.writeFile(...args);
  }
}

export type Provider = string;

export async function openHarness(db: string, provider: Provider, webSearch?: WebSearch, options?: { claudeEnabled?: boolean; computedDiffs?: () => boolean; webFetchEnabled?: () => boolean }) {
  const useOpenAI = provider !== "fixture";
  // An empty provider is the normal server mode: it has no environment default.
  // Explicit providers are still accepted by direct harness callers and the live smoke test.
  const modelId = useOpenAI ? (provider ? process.env.PI_MODEL : undefined) : "faux-1";
  const providerId = provider === "openai-smoke" ? "openai" : provider;
  const models = useOpenAI ? await ModelRuntime.create({ allowModelNetwork: false }) : (() => {
    const fixtureModels = createModels();
    fixtureModels.setProvider(fixtureProvider());
    return fixtureModels;
  })();
  const plugins = new ProviderPlugins();
  const bridge = new ClaudeCwdBridge();
  const claude = (await import("./claude-code/provider.ts")).createClaudeProvider(bridge, options?.claudeEnabled === true);
  plugins.register(claude);
  function registerClaude() {
    if (models instanceof ModelRuntime) models.registerNativeProvider(claude.provider);
    else models.setProvider(claude.provider);
  }
  registerClaude();
  const registry = createRegistry();
  registry.install(bridge.extension(plugins));
  const write = createWriteTool();
  const trackedWrite: typeof write = {
    ...write,
    async execute(args, api, context) {
      const env = api.env;
      if (!env || args.content.length > 128_000 || args.content.includes("\0")) return write.execute(args, api, context);
      // Match the built-in write tool's path normalization.
      const normalized = args.path.replace(/[\u00A0\u2000-\u200A\u202F\u205F\u3000]/g, " ");
      const path = await env.absolutePath(normalized.startsWith("@") ? normalized.slice(1) : normalized, context);
      if (!path.ok) return write.execute(args, api, context);
      const info = await env.fileInfo(path.value, context);
      let before: string | undefined;
      if (!info.ok && info.error.code === "not_found") before = "";
      else if (info.ok && info.value.kind === "file" && info.value.size <= 128_000) {
        const contents = await env.readTextFile(path.value, context);
        if (contents.ok && !contents.value.includes("\0")) before = contents.value;
      }
      const result = await write.execute(args, api, context);
      return before === undefined || before === args.content ? result : {
        ...result, details: { writeDiffs: [{ path: args.path, before, after: args.content }] },
      };
    },
  };
  const bash = createBashTool();
  const trackedBash: typeof bash = {
    ...bash,
    async execute(args, api, context) {
      if (!options?.computedDiffs?.() || !api.env) return bash.execute(args, api, context);
      const root = api.env.cwd;
      // Snapshot failure must never prevent the requested shell command.
      const before = await snapshot(root).catch(() => undefined);
      let result: Awaited<ReturnType<typeof bash.execute>>;
      try {
        result = await bash.execute(args, api, context);
      } catch (error) {
        // Retain a diff for errors such as timeouts when the invocation still
        // permits progress; cancellation can invalidate the execution API.
        if (!context.abortSignal?.aborted && before) {
          const after = await snapshot(root).catch(() => undefined);
          if (after) {
            const changes = compare(before, after);
            await api.details({ computedDiffs: changes.diffs, diffIncomplete: changes.incomplete }, context).catch(() => {});
          }
        }
        throw error;
      }
      if (!before) return { ...result, details: { computedDiffs: [], diffIncomplete: true } };
      const after = await snapshot(root).catch(() => undefined);
      if (!after) return { ...result, details: { computedDiffs: [], diffIncomplete: true } };
      const changes = compare(before, after);
      return { ...result, details: { computedDiffs: changes.diffs, diffIncomplete: changes.incomplete } };
    },
  };
  registry.install(provider === "openai-smoke"
    ? defineExtension({ name: "live-probe-tools", tools: [createReadTool(), createWriteTool()] })
    : defineExtension({ name: "coding-tools", tools: [createReadTool(), trackedWrite, createEditTool(), trackedBash] }));
  registry.install(defineExtension({ name: "mermaid-catalog", tools: [mermaidCatalogTool()] }));
  const searchExtension = webSearch && defineExtension({ name: "web-search", tools: [webSearch.tool()] });
  let searchInstalled = false;
  const fetchEnabled = () => options?.webFetchEnabled?.() ?? true;
  const fetchExtension = defineExtension({ name: "web-fetch", tools: [webFetchTool(fetchEnabled)] });
  let fetchInstalled = false;
  function refreshTools() {
    const fetchAvailable = provider !== "openai-smoke" && fetchEnabled();
    if (fetchAvailable !== fetchInstalled) {
      fetchInstalled = fetchAvailable;
      if (fetchAvailable) registry.install(fetchExtension);
      else registry.uninstall(fetchExtension);
    }
    if (!webSearch || !searchExtension) return;
    const available = provider !== "openai-smoke" && webSearch.available();
    if (available === searchInstalled) return;
    searchInstalled = available;
    if (available) registry.install(searchExtension);
    else registry.uninstall(searchExtension);
  }
  refreshTools();
  const shellEnv = { BRAVE_SEARCH_API_KEY: undefined, BRAVE_API_KEY: undefined, EXA_API_KEY: undefined, FIRECRAWL_API_KEY: undefined };
  const storage = await openNodeSqliteStorage(db);
  const harness = await Harness.open(storage, {
    models, registry,
    // Meter does not yet expose or visualize compaction. Keep automatic
    // compaction off until its effects on history and context are supported.
    settings: { compaction: { enabled: false } },
    env: ({ cwd }) => {
      if (!cwd) throw new Error("Conversation has no working directory");
      if (provider === "openai-smoke") return new LiveProbeEnv({ cwd, shellEnv });
      if (provider === "fixture") return new CrashProbeEnv({ cwd, shellEnv });
      return new NodeExecutionEnv({ cwd, shellEnv });
    },
  }, context);
  // Older databases may predate pi.provider. Initialize before the provider
  // can serve requests, including interrupted runs resumed on startup.
  async function migrateProviderSessions() {
    let cursor: import("@earendil-works/pi-durable").Cursor | undefined;
    do {
      const page = await storage.scanConversations({}, 100, cursor, context);
      for (const record of page.items) {
        if (await harness.snapshot(ProviderDoc, record.id, context)) continue;
        const conversation = await harness.conversation(record.id, context);
        if (!conversation) throw new Error(`Cannot initialize Claude session for conversation ${record.id}`);
        await conversation.commit(async (tx) => { await tx.doc(ProviderDoc, record.id); }, context);
      }
      cursor = page.next;
    } while (cursor);
  }
  if (options?.claudeEnabled) await migrateProviderSessions();
  async function setClaudeEnabled(enabled: boolean) {
    if (enabled === claude.enabled()) return;
    if (enabled) await migrateProviderSessions();
    await claude.setEnabled(enabled);
    registerClaude();
  }
  return { harness, models, plugins, refreshTools, setClaudeEnabled, closeClaude: () => claude.close(), model: modelId ? { provider: useOpenAI ? providerId : "faux", modelId } : undefined };
}

export class RequestConflict extends Error {}

const receipts = defineDocFamily({
  kind: "workspace.admission", scope: "conversation", family: true, version: 1,
  history: "latest", fork: "initial",
  initial: (_key: string) => ({ fingerprint: "", submissionId: "" }),
  checkpointWhen: () => true,
});

export async function admit(conversation: Conversation, input: z.infer<typeof submitInput>, validate?: () => Promise<void>) {
  const fingerprint = createHash("sha256").update(JSON.stringify(
    input.images?.length ? [input.mode, input.content, input.images] : [input.mode, input.content]
  )).digest("hex");
  return conversation.commit(async (tx) => {
    const receipt = await tx.doc(receipts, conversation.id, input.requestId, input.requestId);
    if (receipt.submissionId) {
      if (receipt.fingerprint !== fingerprint) throw new RequestConflict("Request ID already used with different input");
      return receipt.submissionId;
    }
    await validate?.();
    const submission = await conversation.submit(tx, {
      type: "input", requestId: input.requestId,
      content: input.images?.length
        ? [...(input.content.trim() ? [{ type: "text" as const, text: input.content }] : []),
          ...input.images.map((image) => ({ type: "image" as const, data: image.data, mimeType: image.mimeType }))]
        : input.content,
      whenBusy: input.mode === "prompt" ? "reject" : input.mode === "steer" ? "steer" : "followUp",
      whenIdle: input.mode === "steer" ? "reject" : undefined,
    });
    receipt.fingerprint = fingerprint;
    receipt.submissionId = String(submission);
    return receipt.submissionId;
  }, context);
}

// Record the chosen physical fork in the same Durable commit that creates it.
// Admission has its own durable receipt, so either half can be retried after a crash.
const forkOperations = defineDocFamily({
  kind: "workspace.fork-operation", scope: "session", family: true, version: 1,
  initial: (_key: string) => ({ fingerprint: "", conversationId: "" }),
  checkpointWhen: () => true,
});

function forkFingerprint(attached: string, parent: string, at: string, input: { content: string; images?: { data: string; mimeType: "image/png" }[] }) {
  return createHash("sha256").update(JSON.stringify([attached, parent, at, "prompt", input.content, input.images ?? []])).digest("hex");
}

// Only a durably admitted request may clear a pending layout operation.
export async function completedFork(harness: DurableHarness, attached: Conversation, parentId: string, at: string, input: { requestId: string; content: string; images?: { data: string; mimeType: "image/png" }[] }, destination: string) {
  return harness.commit(async (tx) => {
    const operation = await tx.doc(forkOperations, input.requestId, input.requestId);
    if (operation.conversationId !== destination || operation.fingerprint !== forkFingerprint(String(attached.id), parentId, at, input)) return false;
    const receipt = await tx.doc(receipts, Number(destination) as Conversation["id"], input.requestId, input.requestId);
    return !!receipt.submissionId;
  }, context);
}

export class NavigationBusy extends Error {}
class ExistingFork extends Error {
  readonly conversationId: string;
  constructor(conversationId: string) {
    super("Fork already created");
    this.conversationId = conversationId;
  }
}

/** Retry-safe physical fork and admission. Caller validates tree/path membership. */
export async function forkAndAdmit(harness: DurableHarness, attached: Conversation, parent: Conversation, at: EntryId, input: z.infer<typeof submitInput>, validate?: () => Promise<void>) {
  if (input.mode !== "prompt") throw new Error("Fork input must be a prompt");
  const fingerprint = forkFingerprint(String(attached.id), String(parent.id), String(at), input);
  let fork: Conversation;
  try {
    fork = await parent.fork(at, {
      ownership: { kind: "ownerless" },
      init: async (tx, id) => {
        const operation = await tx.doc(forkOperations, input.requestId, input.requestId);
        if (operation.conversationId) {
          if (operation.fingerprint !== fingerprint) throw new RequestConflict("Request ID already used with different fork input");
          throw new ExistingFork(operation.conversationId);
        }
        const live = await tx.doc(LiveDoc, attached.id);
        const inbox = await tx.doc(InboxDoc, attached.id);
        if (live.run || inbox.items.length) throw new NavigationBusy("Wait for the current conversation and queued messages to finish");
        operation.fingerprint = fingerprint;
        operation.conversationId = String(id);
      },
    }, context);
  } catch (error) {
    if (!(error instanceof ExistingFork)) throw error;
    const restored = await harness.conversation(Number(error.conversationId) as Conversation["id"], context);
    if (!restored) throw new Error("Recorded fork is missing");
    fork = restored;
  }
  if (process.env.PI_CRASH_POINT === "fork-created") process.kill(process.pid, "SIGKILL");
  const submissionId = await admit(fork, input, validate);
  if (process.env.PI_CRASH_POINT === "fork-admitted") process.kill(process.pid, "SIGKILL");
  return { conversationId: String(fork.id), submissionId };
}

const recalls = defineDocFamily({
  kind: "workspace.queue-recall", scope: "conversation", family: true, version: 1,
  history: "latest", fork: "initial",
  initial: (_key) => ({ content: "", images: [] as { data: string; mimeType: "image/png" }[], recalled: false as boolean, clearedAfter: 0 }),
  checkpointWhen: () => true,
});

export async function recallQueued(conversation: Conversation, submission: string) {
  return conversation.commit(async (tx) => {
    const saved = await tx.doc(recalls, conversation.id, submission, submission);
    if (saved.recalled) return { content: saved.content, images: saved.images.map((image) => ({ data: image.data, mimeType: image.mimeType })), clearedAfter: saved.clearedAfter };
    const inbox = await tx.doc(InboxDoc, conversation.id);
    const index = inbox.items.findIndex((item) => String(item.id) === submission);
    if (index < 0) return undefined;
    const item = inbox.items[index];
    if (item.mode === "write") return undefined;
    const blocks = typeof item.content === "string" ? [] : item.content;
    saved.content = typeof item.content === "string" ? item.content : blocks.flatMap((block) => block.type === "text" ? [block.text] : []).join("\n");
    saved.images = blocks.flatMap((block) => block.type === "image" && block.mimeType === "image/png" ? [{ data: block.data, mimeType: "image/png" as const }] : []);
    saved.recalled = true;
    for (let i = inbox.items.length - 1; i >= index; i--) {
      const removed = inbox.items[i];
      if (removed.mode === "write") continue;
      inbox.items.splice(i, 1);
      tx.settleSubmission(removed.id, { status: "unanswered", reason: i === index ? "recalled" : "discarded_after_recall" });
      if (i !== index) saved.clearedAfter++;
    }
    return { content: saved.content, images: saved.images.map((image) => ({ data: image.data, mimeType: image.mimeType })), clearedAfter: saved.clearedAfter };
  }, context);
}

export async function steerQueued(conversation: Conversation, submission: string) {
  return conversation.commit(async (tx) => {
    const live = await tx.doc(LiveDoc, conversation.id);
    if (!live.run) return false;
    const inbox = await tx.doc(InboxDoc, conversation.id);
    const item = inbox.items.find((item) => String(item.id) === submission);
    if (!item || item.mode === "write") return false;
    item.mode = "steer";
    return true;
  }, context);
}

export async function abortTarget(harness: Harness, conversation: Conversation, target: string) {
  const live = await harness.snapshot(LiveDoc, conversation.id, context);
  if (!live?.run || String(live.run.taskId) !== target) return false;
  // Target the unique generation task. A handoff between the read and the
  // abort can retire this task, but can never redirect the abort to its successor.
  return (await harness.abortTask(live.run.taskId, context)) === "marked";
}

const clip = (text: string, limit = 12_000) =>
  text.length > limit ? text.slice(0, limit) + "\n[output truncated]" : text;

export function entryText(entry: EntryRecord) {
  return (entry.model ?? []).flatMap((message) => {
    if (typeof message.content === "string") return [message.content];
    return message.content.flatMap((block) => block.type === "text" ? [block.text] : []);
  }).join("\n");
}

export function projectEntries(entries: readonly EntryRecord[]) {
  const targets = new Map<string, string>();
  const inputs = new Map<string, string>();
  const projected = entries.map((entry) => projectEntry(entry, targets, inputs));
  return { projected, targets, inputs };
}

// Restore the actual user input, not its clipped transcript projection.
export function entryUserMessage(entry: EntryRecord) {
  if (entry.kind !== "pi.user") return undefined;
  const messages = (entry.model ?? []).filter((message) => message.role === "user");
  if (!messages.length) return undefined;
  const content = messages.flatMap((message) => typeof message.content === "string"
    ? [message.content]
    : message.content.flatMap((block) => block.type === "text" ? [block.text] : [])).join("\n");
  return { content, images: entryImages(entry) };
}

export function entryImages(entry: EntryRecord) {
  return (entry.model ?? []).flatMap((message) =>
    message.role === "user" && Array.isArray(message.content)
      ? message.content.flatMap((block) => block.type === "image" && block.mimeType === "image/png" ? [{ data: block.data, mimeType: "image/png" as const }] : [])
      : []);
}

function projectEntry(entry: EntryRecord, targets: Map<string, string>, inputs: Map<string, string>) {
  const messages = entry.model ?? [];
  const assistant = messages.findLast((message) => message.role === "assistant");
  const tool = messages.find((message) => message.role === "toolResult");
  for (const block of assistant?.content ?? []) {
    if (block.type !== "toolCall") continue;
    const target = block.arguments.path ?? block.arguments.file_path ?? block.arguments.command ?? block.arguments.query ?? block.arguments.url;
    if (typeof target === "string") targets.set(block.id, target);
    const input = block.arguments.command;
    inputs.set(block.id, typeof input === "string" ? input : JSON.stringify(block.arguments, null, 2));
  }
  const text = entryText(entry);
  // Match the harness's answer boundary, even if a queued run has already started.
  const finalText = entry.kind === "pi.assistant" && assistant?.content.some((block) => block.type === "text" && block.text.trim()) &&
    (assistant.stopReason === "stop" || assistant.stopReason === "length" ||
      (assistant.stopReason === "toolUse" && !assistant.content.some((block) => block.type === "toolCall")));
  const details = tool?.details;
  const diff = details && typeof details === "object" && "diff" in details && typeof details.diff === "string" ? details.diff : undefined;
  const changes = diff?.split("\n");
  return {
    id: String(entry.id), kind: entry.kind,
    blocks: assistant?.content.flatMap<{ type: "text" | "thinking"; text: string }>((block) => block.type === "text" ? [{ type: "text" as const, text: clip(block.text) }] : block.type === "thinking" && block.thinking ? [{ type: "thinking" as const, text: clip(block.thinking) }] : []),
    text: clip(text),
    error: assistant?.stopReason === "error" && assistant.errorMessage ? clip(assistant.errorMessage) : undefined,
    imageCount: entryImages(entry).length || undefined,
    toolName: tool?.toolName, toolSucceeded: tool ? !tool.isError : undefined,
    toolCallId: tool?.toolCallId,
    toolTarget: tool && targets.get(tool.toolCallId) ? clip(targets.get(tool.toolCallId)!.replace(/\s+/g, " ").trim(), 400) : undefined,
    toolCommand: tool && inputs.get(tool.toolCallId),
    writeDiffs: details && typeof details === "object" && "writeDiffs" in details && Array.isArray(details.writeDiffs)
      ? details.writeDiffs.flatMap((item) => item && typeof item === "object" && typeof item.path === "string" && typeof item.before === "string" && typeof item.after === "string" ? [{ path: item.path, before: item.before, after: item.after }] : []) : undefined,
    computedDiffs: details && typeof details === "object" && "computedDiffs" in details && Array.isArray(details.computedDiffs)
      ? details.computedDiffs.flatMap((item) => item && typeof item === "object" && typeof item.path === "string" && typeof item.before === "string" && typeof item.after === "string" ? [{ path: item.path, before: item.before, after: item.after }] : []) : undefined,
    diffIncomplete: details && typeof details === "object" && "diffIncomplete" in details && details.diffIncomplete === true ? true : undefined,
    // Preserve complete patches only: a clipped unified patch is not parseable.
    diffPatch: details && typeof details === "object" && "patch" in details && typeof details.patch === "string" && details.patch.length <= 100_000 ? details.patch : undefined,
    durationMs: tool?.durationMs ?? assistant?.durationMs,
    timestamp: finalText ? assistant?.timestamp : undefined,
    cacheInput: assistant?.usage.input, cacheRead: assistant?.usage.cacheRead, cacheWrite: assistant?.usage.cacheWrite,
    cacheModel: assistant ? `${assistant.provider}/${assistant.model}` : undefined,
    tokens: assistant?.usage.totalTokens,
    added: changes?.filter((line) => /^\+(?!\+\+)/.test(line)).length,
    removed: changes?.filter((line) => /^-(?!--)/.test(line)).length,
    outputClipped: tool && text.length > 12_000 ? true : undefined,
  };
}

export const historyCursor = (id: number) => Buffer.from(String(id)).toString("base64url");

// Anchor estimates to the last measured generation after the active head.
export function contextTokens(active: ContextView) {
  const head = active.head?.id ?? -1;
  for (let i = active.entries.length - 1; i >= 0 && active.entries[i].id > head; i--) {
    const measured = active.contributions[i]
      .findLast((message) => message.role === "assistant" && calculateContextTokens(message.usage) > 0);
    if (measured?.role === "assistant") {
      const tail = active.messages.slice(active.messages.lastIndexOf(measured) + 1);
      return tail.reduce((sum, message) => sum + estimateMessageTokens(message), calculateContextTokens(measured.usage));
    }
  }
  return active.messages.reduce((sum, message) => sum + estimateMessageTokens(message), 0);
}

export function project(value: ConversationView, usage?: z.infer<typeof contextUsage>) {
  const live = value.docs["pi.live"] as LiveState | undefined;
  const partial = live?.generation?.message?.content?.flatMap((block) => block.type === "text" ? [block.text] : []).join("");
  const partialBlocks = live?.generation?.message?.content?.flatMap<{ type: "text" | "thinking"; text: string }>((block) => block.type === "text" ? [{ type: "text" as const, text: clip(block.text) }] : block.type === "thinking" && block.thinking ? [{ type: "thinking" as const, text: clip(block.thinking) }] : []);
  const { projected, targets, inputs } = projectEntries(value.entries);
  const agent = value.docs["pi.agent"] as AgentState | undefined;
  return view.parse({
    conversationId: String(value.conversation.id), selection: { model: agent?.model ?? null, thinkingLevel: agent?.thinkingLevel ?? "off" },
    status: live?.run ? "running" : live?.runFailure ? "failed" : "idle",
    failure: live?.runFailure?.reason,
    abortTarget: live?.run && String(live.run.taskId),
    before: value.before && value.entries.length ? historyCursor(value.entries[0].id) : undefined,
    partial: partial ? clip(partial) : null, partialBlocks,
    entries: projected, contextUsage: usage,
    queued: ((value.docs["pi.inbox"] as InboxState | undefined)?.items ?? [])
      .filter((item) => item.mode !== "write").map((item) => {
        const blocks = typeof item.content === "string" ? [] : item.content;
        const text = typeof item.content === "string" ? item.content : blocks.flatMap((block) => block.type === "text" ? [block.text] : []).join("\n");
        return { id: String(item.id), mode: item.mode, text: clip(text, 2_000), images: blocks.filter((block) => block.type === "image").length };
      }),
    tools: live?.tools?.slice(0, 24).map((slot) => ({
      callId: slot.callId, taskId: slot.taskId === undefined ? undefined : String(slot.taskId),
      entryId: slot.entry === undefined ? undefined : String(slot.entry),
      name: slot.name, status: slot.status, output: slot.output && clip(slot.output, 2500), command: inputs.get(slot.callId), target: targets.get(slot.callId) && clip(targets.get(slot.callId)!.replace(/\s+/g, " ").trim(), 400),
    })) ?? [],
  });
}
