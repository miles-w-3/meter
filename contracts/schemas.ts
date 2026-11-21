import { z } from "zod";

const opaque = z.string().min(1).max(160);
export const serverInfo = z.object({
  id: z.uuid(), protocol: z.literal("v1"), ready: z.literal(true),
  capabilities: z.array(z.string()), provider: z.string(),
  revision: z.string().optional(), dirty: z.boolean().optional(),
  generation: z.uuid().optional(), transport: z.enum(["unix", "tcp"]).optional(),
  dictation: z.enum(["ready", "model_missing", "worker_missing"]).optional(),
});
export const modelRef = z.strictObject({ provider: opaque, modelId: opaque });
export const thinkingLevel = z.enum(["off", "minimal", "low", "medium", "high", "xhigh", "max"]);
export const defaultSettings = z.strictObject({ model: modelRef.nullable(), thinkingLevel: thinkingLevel.nullable() });
export const settings = defaultSettings.extend({ source: z.enum(["saved", "environment", "fixture", "unconfigured"]) });
export const searchProvider = z.enum(["brave", "exa", "firecrawl"]);
export const toolSettings = z.strictObject({
  webSearch: z.strictObject({
    enabled: z.boolean(), providers: z.record(searchProvider, z.boolean()),
  }),
});
export type ToolSettings = z.infer<typeof toolSettings>;
export const searchProviderChange = z.strictObject({ id: searchProvider, enabled: z.boolean() });
export const toolSettingsChange = z.strictObject({
  webSearch: z.strictObject({
    enabled: z.boolean().optional(),
    provider: searchProviderChange.optional(),
  }),
});
export const searchProviderStatus = z.object({
  id: searchProvider, enabled: z.boolean(), configured: z.boolean(), retryAt: z.iso.datetime().optional(),
  status: z.enum(["ready", "disabled", "missing_key", "storage_rights_required", "rate_limited", "unavailable", "auth_error", "quota_exhausted"]),
});
export const toolsStatus = z.object({
  webSearch: z.object({ enabled: z.boolean(), providers: z.array(searchProviderStatus) }),
});
const authMethod = z.object({ type: z.enum(["api_key", "oauth"]), name: z.string(), interactive: z.boolean() });
const providerInfo = z.object({
  id: z.string(), name: z.string(), methods: z.array(authMethod),
  status: z.enum(["configured", "unconfigured", "unknown"]), source: z.string().optional(),
});
const modelInfo = z.object({
  provider: z.string(), modelId: z.string(), name: z.string(), contextWindow: z.number(),
  reasoning: z.boolean(), thinkingLevels: z.array(thinkingLevel),
});
export const modelInventory = z.object({ providers: z.array(providerInfo), models: z.array(modelInfo) });
const cwd = z.string().min(1).max(4096);
export const cwdInput = z.strictObject({ cwd });
export const selection = z.strictObject({ model: modelRef.nullable(), thinkingLevel });
export const selectionInput = z.strictObject({ model: modelRef, thinkingLevel });
export const selectionChange = selectionInput.extend({ expected: selection });
export const createInput = z.strictObject({ cwd, projectId: z.uuid().optional(), selection: selectionInput.optional() });
export const conversation = z.object({ id: opaque, cwd: z.string(), model: z.string(), selection: selection.optional(), title: z.string().optional(), lastActivity: z.number().int().nonnegative().optional(), projectId: z.uuid().optional() });
export const imageAttachment = z.strictObject({
  data: z.string().min(4).max(2_796_204).regex(/^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/),
  mimeType: z.literal("image/png"),
});

export const submitInput = z.strictObject({
  requestId: opaque,
  mode: z.enum(["prompt", "followup", "steer"]),
  content: z.string().max(32_000),
  images: z.array(imageAttachment).max(4).optional(),
}).refine((value) => value.content.trim().length > 0 || (value.images?.length ?? 0) > 0);
export const recalledMessage = z.object({ content: z.string(), images: z.array(imageAttachment), clearedAfter: z.number().int().nonnegative() });
export const receipt = z.object({ submissionId: opaque });
export const abortInput = z.strictObject({ target: opaque });
export const problem = z.object({ code: z.string(), message: z.string() });
export const entry = z.object({
  id: opaque, kind: z.string(), text: z.string(), error: z.string().optional(), imageCount: z.number().int().positive().optional(), toolName: z.string().optional(), toolSucceeded: z.boolean().optional(),
  toolCallId: z.string().optional(), toolTarget: z.string().optional(), durationMs: z.number().int().nonnegative().optional(),
  tokens: z.number().int().nonnegative().optional(), added: z.number().int().nonnegative().optional(),
  removed: z.number().int().nonnegative().optional(), outputClipped: z.boolean().optional(),
});
const toolOutput = z.object({ text: z.string() });
const entryImages = z.object({ images: z.array(imageAttachment) });
export const page = z.object({ entries: z.array(entry), before: opaque.optional() });
export const historyBranch = z.object({ conversationId: opaque, parentConversationId: opaque.optional(), at: opaque.optional() });
export const historyTree = z.object({ rootConversationId: opaque, branches: z.array(historyBranch) });
export const contextUsage = z.object({ tokens: z.number().int().nonnegative(), window: z.number().int().positive() });
const tool = z.object({ callId: z.string().optional(), taskId: opaque.optional(), entryId: opaque.optional(), name: z.string(), status: z.string(), output: z.string().optional(), target: z.string().optional() });
export const queuedMessage = z.object({ id: opaque, text: z.string(), images: z.number().int().nonnegative(), mode: z.enum(["followUp", "steer"]) });
export const view = z.object({
  conversationId: opaque, status: z.enum(["idle", "running", "failed"]),
  partial: z.string().nullable(), failure: z.string().optional(),
  abortTarget: opaque.optional(), before: opaque.optional(),
  entries: z.array(entry), tools: z.array(tool), queued: z.array(queuedMessage).optional(),
  contextUsage: contextUsage.optional(), selection: selection.optional(),
});
const version = { revision: z.number().int().min(0), generation: z.uuid() };
const snapshot = z.object({ type: z.literal("snapshot"), ...version, view });
const update = z.object({ type: z.literal("update"), ...version, view });
export const conversationFrame = z.discriminatedUnion("type", [snapshot, update]);
export const projectInput = z.strictObject({ cwd: z.string().min(1).max(4096), name: z.string().trim().min(1).max(80) });
export const projectRenameInput = z.strictObject({ name: z.string().trim().min(1).max(80) });
export const projectRecord = z.object({ id: opaque, cwd: z.string(), name: z.string() });
export const catalog = z.object({ projects: z.array(projectRecord), conversations: z.array(conversation) });
export const layoutNode = z.strictObject({
  id: z.uuid(), conversationId: opaque.optional(), name: z.string().trim().min(1).max(80).optional(), widthShare: z.number().min(.01).max(1).optional(), empty: z.literal(true).optional(), cwd: cwd.optional(),
  axis: z.enum(["horizontal", "vertical"]).optional(), ratio: z.number().min(.15).max(.85).optional(),
  first: z.uuid().optional(), second: z.uuid().optional(),
});
export const layoutTab = z.strictObject({ id: z.uuid(), name: z.string().trim().min(1).max(80).optional(), projectId: z.uuid().optional(), root: z.uuid(), nodes: z.array(layoutNode).min(1).max(31) });
export const layout = z.strictObject({
  revision: z.number().int().nonnegative().max(Number.MAX_SAFE_INTEGER),
  activeTab: z.uuid().optional(), activeProject: z.uuid().optional(), closedProjects: z.array(z.uuid()).optional(), tabs: z.array(layoutTab).max(24),
});
export const models = {
  ImageAttachment: imageAttachment, ServerInfo: serverInfo, CreateInput: createInput, CwdInput: cwdInput, Conversation: conversation, SubmitInput: submitInput,
  QueuedMessage: queuedMessage, RecalledMessage: recalledMessage, Receipt: receipt, AbortInput: abortInput, Problem: problem, Entry: entry, HistoryPage: page, ToolOutput: toolOutput, EntryImages: entryImages,
  HistoryBranch: historyBranch, HistoryTree: historyTree,
  ContextUsage: contextUsage, ConversationView: view, ConversationSnapshot: snapshot, ConversationUpdate: update,
  ConversationFrame: conversationFrame, ProjectInput: projectInput, ProjectRenameInput: projectRenameInput, Project: projectRecord, Catalog: catalog,
  LayoutNode: layoutNode, LayoutTab: layoutTab, Layout: layout,
  ModelRef: modelRef, Selection: selection, SelectionInput: selectionInput, SelectionChange: selectionChange, DefaultSettings: defaultSettings, Settings: settings,
  ToolSettingsChange: toolSettingsChange, SearchProviderChange: searchProviderChange, ToolsStatus: toolsStatus, SearchProviderStatus: searchProviderStatus,
  AuthMethod: authMethod, ProviderInfo: providerInfo, ModelInfo: modelInfo, ModelInventory: modelInventory,
};
