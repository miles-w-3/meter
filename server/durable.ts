import { join } from "node:path";
import { BACKGROUND_CONTEXT as context } from "@earendil-works/chord/context";
import { calculateContextTokens, estimateMessageTokens } from "@earendil-works/pi-ai/utils/estimate";
import { createModels } from "@earendil-works/pi-ai/models";
import type { FauxResponseFactory } from "@earendil-works/pi-ai/providers/faux";
import { fauxAssistantMessage, fauxProvider, fauxText, fauxToolCall } from "@earendil-works/pi-ai/providers/faux";
import { ModelRuntime } from "@earendil-works/pi-coding-agent";
import { createRegistry, defineDocFamily, defineExtension, Harness, InboxDoc, LiveDoc } from "@earendil-works/pi-durable";
import type { AgentState, Conversation, ContextView, ConversationView, EntryRecord, InboxState, LiveState } from "@earendil-works/pi-durable";
import { NodeExecutionEnv } from "@earendil-works/pi-durable/env/node";
import { openNodeSqliteStorage } from "@earendil-works/pi-durable/storage/sqlite/node";
import { CodingTools, createReadTool, createWriteTool } from "@earendil-works/pi-durable/tools";
import { createHash } from "node:crypto";
import type { z } from "zod";
import { contextUsage, submitInput, view } from "../contracts/schemas.ts";

import type { WebSearch } from "./tools/web-search.ts";

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
    if (prompt.includes("fixture:demo")) return fauxAssistantMessage([fauxText("desktop reconnect output ".repeat(200))]);
    if (prompt.includes("fixture:slow")) return fauxAssistantMessage([fauxText("streamed model output ".repeat(45))]);
    if (prompt === "fixture:web-search" && !results.length) {
      return tool("web_search", { query: "Meter search fixture", limit: 2 });
    }
    if (prompt.includes("fixture:tools")) {
      if (!results.length) return tool("read", { path: "notes.txt" });
      if (results.length === 1) return tool("write", { path: "result.txt", content: "written by Durable\n" });
    }
    if (prompt.includes("fixture:tools-slow")) return fauxAssistantMessage([fauxText("Fixture completed after running the coding tools. " + "streamed model output ".repeat(45))]);
    return answer();
  }));
  return faux.provider;
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

export async function openHarness(db: string, provider: Provider, webSearch?: WebSearch) {
  const useOpenAI = provider !== "fixture";
  const modelId = useOpenAI ? process.env.PI_MODEL : "faux-1";
  const providerId = provider === "openai-smoke" ? "openai" : provider;
  const models = useOpenAI ? await ModelRuntime.create({ allowModelNetwork: false }) : (() => {
    const fixtureModels = createModels();
    fixtureModels.setProvider(fixtureProvider());
    return fixtureModels;
  })();
  const registry = createRegistry();
  registry.install(provider === "openai-smoke"
    ? defineExtension({ name: "live-probe-tools", tools: [createReadTool(), createWriteTool()] })
    : CodingTools);
  const searchExtension = webSearch && defineExtension({ name: "web-search", tools: [webSearch.tool()] });
  let searchInstalled = false;
  function refreshTools() {
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
    env: ({ cwd }) => {
      if (!cwd) throw new Error("Conversation has no working directory");
      if (provider === "openai-smoke") return new LiveProbeEnv({ cwd, shellEnv });
      if (provider === "fixture") return new CrashProbeEnv({ cwd, shellEnv });
      return new NodeExecutionEnv({ cwd, shellEnv });
    },
  }, context);
  return { harness, models, refreshTools, model: modelId ? { provider: useOpenAI ? providerId : "faux", modelId } : undefined };
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
  const projected = entries.map((entry) => projectEntry(entry, targets));
  return { projected, targets };
}

export function entryImages(entry: EntryRecord) {
  return (entry.model ?? []).flatMap((message) =>
    message.role === "user" && Array.isArray(message.content)
      ? message.content.flatMap((block) => block.type === "image" && block.mimeType === "image/png" ? [{ data: block.data, mimeType: "image/png" as const }] : [])
      : []);
}

function projectEntry(entry: EntryRecord, targets: Map<string, string>) {
  const messages = entry.model ?? [];
  const assistant = messages.findLast((message) => message.role === "assistant");
  const tool = messages.find((message) => message.role === "toolResult");
  for (const block of assistant?.content ?? []) {
    if (block.type !== "toolCall") continue;
    const target = block.arguments.path ?? block.arguments.file_path ?? block.arguments.command ?? block.arguments.query;
    if (typeof target === "string") targets.set(block.id, clip(target.replace(/\s+/g, " ").trim(), 400));
  }
  const text = entryText(entry);
  const details = tool?.details;
  const diff = details && typeof details === "object" && "diff" in details && typeof details.diff === "string" ? details.diff : undefined;
  const changes = diff?.split("\n");
  return {
    id: String(entry.id), kind: entry.kind,
    text: clip(text),
    imageCount: entryImages(entry).length || undefined,
    toolName: tool?.toolName, toolSucceeded: tool ? !tool.isError : undefined,
    toolCallId: tool?.toolCallId, toolTarget: tool && targets.get(tool.toolCallId),
    durationMs: tool?.durationMs ?? assistant?.durationMs,
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
  const { projected, targets } = projectEntries(value.entries);
  const agent = value.docs["pi.agent"] as AgentState | undefined;
  return view.parse({
    conversationId: String(value.conversation.id), selection: { model: agent?.model ?? null, thinkingLevel: agent?.thinkingLevel ?? "off" },
    status: live?.run ? "running" : live?.runFailure ? "failed" : "idle",
    failure: live?.runFailure?.reason,
    abortTarget: live?.run && String(live.run.taskId),
    before: value.before && value.entries.length ? historyCursor(value.entries[0].id) : undefined,
    partial: partial ? clip(partial) : null,
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
      name: slot.name, status: slot.status, output: slot.output && clip(slot.output, 2500), target: targets.get(slot.callId),
    })) ?? [],
  });
}
