import { join } from "node:path";
import { BACKGROUND_CONTEXT as context } from "@earendil-works/chord/context";
import { calculateContextTokens, estimateMessageTokens } from "@earendil-works/pi-ai/utils/estimate";
import { createModels } from "@earendil-works/pi-ai/models";
import type { FauxResponseFactory } from "@earendil-works/pi-ai/providers/faux";
import { fauxAssistantMessage, fauxProvider, fauxText, fauxToolCall } from "@earendil-works/pi-ai/providers/faux";
import { ModelRuntime } from "@earendil-works/pi-coding-agent";
import { createRegistry, defineDocFamily, defineExtension, Harness, LiveDoc } from "@earendil-works/pi-durable";
import type { Conversation, ContextView, ConversationView, EntryRecord, LiveState } from "@earendil-works/pi-durable";
import { NodeExecutionEnv } from "@earendil-works/pi-durable/env/node";
import { openNodeSqliteStorage } from "@earendil-works/pi-durable/storage/sqlite/node";
import { CodingTools, createReadTool, createWriteTool } from "@earendil-works/pi-durable/tools";
import { createHash } from "node:crypto";
import type { z } from "zod";
import { contextUsage, submitInput, view } from "../contracts/schemas.ts";

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

export type Provider = "fixture" | "openai" | "openai-codex" | "openai-smoke";

export async function openHarness(db: string, provider: Provider) {
  const useOpenAI = provider !== "fixture";
  const modelId = useOpenAI ? process.env.PI_MODEL : "faux-1";
  const providerId = provider === "openai-codex" ? "openai-codex" : "openai";
  const models = useOpenAI ? await ModelRuntime.create({ refreshOnCreate: false }) : (() => {
    const fixtureModels = createModels();
    fixtureModels.setProvider(fixtureProvider());
    return fixtureModels;
  })();
  if (useOpenAI) {
    if (!modelId) throw new Error("Set PI_MODEL");
    if (!await models.checkAuth(providerId)) throw new Error(`No ${providerId} credentials found; log in with Pi or set OPENAI_API_KEY for openai`);
  }
  const registry = createRegistry();
  registry.install(provider === "openai-smoke"
    ? defineExtension({ name: "live-probe-tools", tools: [createReadTool(), createWriteTool()] })
    : CodingTools);
  const storage = await openNodeSqliteStorage(db);
  const harness = await Harness.open(storage, {
    models, registry,
    env: ({ cwd }) => {
      if (!cwd) throw new Error("Conversation has no working directory");
      if (provider === "openai-smoke") return new LiveProbeEnv({ cwd });
      if (provider === "fixture") return new CrashProbeEnv({ cwd });
      return new NodeExecutionEnv({ cwd });
    },
  }, context);
  return { harness, models, model: { provider: useOpenAI ? providerId : "faux", modelId: modelId! } };
}

export class RequestConflict extends Error {}

const receipts = defineDocFamily({
  kind: "workspace.admission", scope: "conversation", family: true, version: 1,
  history: "latest", fork: "initial",
  initial: (_key: string) => ({ fingerprint: "", submissionId: "" }),
  checkpointWhen: () => true,
});

export async function admit(conversation: Conversation, input: z.infer<typeof submitInput>) {
  const fingerprint = createHash("sha256").update(JSON.stringify([input.mode, input.content])).digest("hex");
  return conversation.commit(async (tx) => {
    const receipt = await tx.doc(receipts, conversation.id, input.requestId, input.requestId);
    if (receipt.submissionId) {
      if (receipt.fingerprint !== fingerprint) throw new RequestConflict("Request ID already used with different input");
      return receipt.submissionId;
    }
    const submission = await conversation.submit(tx, {
      type: "input", requestId: input.requestId, content: input.content,
      whenBusy: input.mode === "prompt" ? "reject" : input.mode === "steer" ? "steer" : "followUp",
      whenIdle: input.mode === "steer" ? "reject" : undefined,
    });
    receipt.fingerprint = fingerprint;
    receipt.submissionId = String(submission);
    return receipt.submissionId;
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

export function projectEntry(entry: EntryRecord) {
  const messages = entry.model ?? [];
  const tool = messages.find((message) => message.role === "toolResult");
  return {
    id: String(entry.id), kind: entry.kind,
    text: clip(messages.flatMap((message) => {
      if (typeof message.content === "string") return [message.content];
      return message.content.flatMap((block) => block.type === "text" ? [block.text] : []);
    }).join("\n")),
    toolName: tool?.toolName, toolSucceeded: tool ? !tool.isError : undefined,
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
  return view.parse({
    conversationId: String(value.conversation.id),
    status: live?.run ? "running" : live?.runFailure ? "failed" : "idle",
    failure: live?.runFailure?.reason,
    abortTarget: live?.run && String(live.run.taskId),
    before: value.before && value.entries.length ? historyCursor(value.entries[0].id) : undefined,
    partial: partial ? clip(partial) : null,
    entries: value.entries.map(projectEntry), contextUsage: usage,
    tools: live?.tools?.slice(0, 24).map((slot) => ({
      callId: slot.callId, taskId: slot.taskId === undefined ? undefined : String(slot.taskId),
      entryId: slot.entry === undefined ? undefined : String(slot.entry),
      name: slot.name, status: slot.status, output: slot.output && clip(slot.output, 2500),
    })) ?? [],
  });
}
