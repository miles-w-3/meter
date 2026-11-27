import { z } from "zod";

// Schemas given an id with .meta({ id }) become named OpenAPI components
// (see generate.ts). Wherever another schema uses one, the spec refers to it,
// so the Go generator emits a single named type instead of a copy per use.
// Give a schema an id when it is shared or when Go code names its type.

const opaque = z.string().min(1).max(160);
export const thinkingLevel = z.enum(["off", "minimal", "low", "medium", "high", "xhigh", "max"]).meta({ id: "ThinkingLevel" });
export const searchProvider = z.enum(["brave", "exa", "firecrawl"]).meta({ id: "SearchProvider" });
export const diffLayout = z.enum(["split", "unified"]).meta({ id: "DiffLayout" });
export const diffInline = z.enum(["words", "characters", "off"]).meta({ id: "DiffInline" });

export const serverInfo = z.object({
  id: z.uuid(), protocol: z.literal("v1"), ready: z.literal(true),
  capabilities: z.array(z.string()), provider: z.string(),
  revision: z.string().optional(), dirty: z.boolean().optional(),
  agentPath: z.string().optional(), agentPathSource: z.string().optional(),
  generation: z.uuid().optional(), transport: z.enum(["unix", "tcp"]).optional(),
  dictation: z.enum(["ready", "model_missing", "worker_missing"]).optional(),
}).meta({ id: "ServerInfo" });
export const modelRef = z.strictObject({ provider: opaque, modelId: opaque }).meta({ id: "ModelRef" });
export const defaultSettings = z.strictObject({ model: modelRef.nullable(), thinkingLevel: thinkingLevel.nullable() }).meta({ id: "DefaultSettings" });
export const settings = z.strictObject({
  model: modelRef.nullable(), thinkingLevel: thinkingLevel.nullable(),
  source: z.enum(["saved", "environment", "fixture", "unconfigured"]),
}).meta({ id: "Settings" });

// Persisted tool settings. Not part of the wire contract.
export const toolSettings = z.strictObject({
  webFetch: z.strictObject({ enabled: z.boolean() }).default({ enabled: true }),
  webSearch: z.strictObject({
    enabled: z.boolean(), providers: z.record(searchProvider, z.boolean()),
  }),
  showThinking: z.boolean().default(true),
  computedDiffs: z.boolean().default(true),
  diffLayout: diffLayout.default("split"),
  diffInline: diffInline.default("words"),
});
export type ToolSettings = z.infer<typeof toolSettings>;
export const searchProviderChange = z.strictObject({ id: searchProvider, enabled: z.boolean() }).meta({ id: "SearchProviderChange" });
export const toolSettingsChange = z.strictObject({
  webFetch: z.strictObject({ enabled: z.boolean().optional() }).optional(),
  webSearch: z.strictObject({
    enabled: z.boolean().optional(),
    provider: searchProviderChange.optional(),
  }).optional(),
  showThinking: z.boolean().optional(),
  computedDiffs: z.boolean().optional(),
  diffLayout: diffLayout.optional(),
  diffInline: diffInline.optional(),
}).meta({ id: "ToolSettingsChange" });
export const searchProviderStatus = z.object({
  id: searchProvider, enabled: z.boolean(), configured: z.boolean(), retryAt: z.iso.datetime().optional(),
  status: z.enum(["ready", "disabled", "missing_key", "storage_rights_required", "rate_limited", "unavailable", "auth_error", "quota_exhausted"]),
}).meta({ id: "SearchProviderStatus" });
export const toolsStatus = z.object({
  webFetch: z.object({ enabled: z.boolean() }).optional(),
  showThinking: z.boolean().optional(),
  webSearch: z.object({ enabled: z.boolean(), providers: z.array(searchProviderStatus) }),
  computedDiffs: z.boolean(),
  diffLayout,
  diffInline,
}).meta({ id: "ToolsStatus" });
const authMethod = z.object({ type: z.enum(["api_key", "oauth"]), name: z.string(), interactive: z.boolean() }).meta({ id: "AuthMethod" });
// How long a provider keeps a conversation's session process, and how many it
// keeps at once (Claude Code: idle, mid tool call, and parked processes).
export const providerSessions = z.strictObject({
  keepaliveMinutes: z.number().int().min(1).max(240),
  toolWaitMinutes: z.number().int().min(1).max(60),
  maxParked: z.number().int().min(1).max(8),
  dateOverride: z.boolean().optional(),
}).meta({ id: "ProviderSessions" });
const providerSessionStats = z.object({
  liveProcesses: z.number().int().nonnegative(), activeTurns: z.number().int().nonnegative(),
  parkedIdle: z.number().int().nonnegative(), parkedWaitingForTools: z.number().int().nonnegative(),
}).meta({ id: "ProviderSessionStats" });
export const providerInfo = z.object({
  id: z.string(), name: z.string(), methods: z.array(authMethod),
  status: z.enum(["configured", "unconfigured", "unknown"]), source: z.string().optional(),
  externalAuth: z.string().optional(), catalogSource: z.enum(["builtin", "cached", "account"]).optional(), enabled: z.boolean().optional(),
  sessions: providerSessions.optional(), sessionsBusy: z.boolean().optional(), sessionStats: providerSessionStats.optional(),
}).meta({ id: "ProviderInfo" });
const modelInfo = z.object({
  provider: z.string(), modelId: z.string(), name: z.string(), contextWindow: z.number(),
  reasoning: z.boolean(), thinkingLevels: z.array(thinkingLevel),
}).meta({ id: "ModelInfo" });
export const modelInventory = z.object({ providers: z.array(providerInfo), models: z.array(modelInfo) }).meta({ id: "ModelInventory" });
export const providerSetting = z.strictObject({ enabled: z.boolean().optional(), sessions: providerSessions.optional() })
  .refine((value) => value.enabled !== undefined || value.sessions !== undefined).meta({ id: "ProviderSetting" });
const cwd = z.string().min(1).max(4096);
export const cwdInput = z.strictObject({ cwd }).meta({ id: "CwdInput" });
export const selection = z.strictObject({ model: modelRef.nullable(), thinkingLevel }).meta({ id: "Selection" });
export const selectionInput = z.strictObject({ model: modelRef, thinkingLevel }).meta({ id: "SelectionInput" });
export const selectionChange = z.strictObject({ model: modelRef, thinkingLevel, expected: selection }).meta({ id: "SelectionChange" });
export const createInput = z.strictObject({ cwd, projectId: z.uuid().optional(), selection: selectionInput.optional() }).meta({ id: "CreateInput" });
export const conversation = z.object({ id: opaque, cwd: z.string(), model: z.string(), selection: selection.optional(), title: z.string().optional(), lastActivity: z.number().int().nonnegative().optional(), projectId: z.uuid().optional() }).meta({ id: "Conversation" });
export const imageAttachment = z.strictObject({
  data: z.string().min(4).max(2_796_204).regex(/^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/),
  mimeType: z.literal("image/png"),
}).meta({ id: "ImageAttachment" });

export const submitInput = z.strictObject({
  requestId: opaque,
  mode: z.enum(["prompt", "followup", "steer"]),
  content: z.string().max(32_000),
  images: z.array(imageAttachment).max(4).optional(),
}).refine((value) => value.content.trim().length > 0 || (value.images?.length ?? 0) > 0).meta({ id: "SubmitInput" });
export const forkInput = z.strictObject({
  parentConversationId: opaque, at: opaque, requestId: opaque,
  content: z.string().max(32_000), images: z.array(imageAttachment).max(4).optional(),
}).refine((value) => value.content.trim().length > 0 || (value.images?.length ?? 0) > 0).meta({ id: "ForkInput" });
export const forkReceipt = z.object({ conversationId: opaque, submissionId: opaque }).meta({ id: "ForkReceipt" });
export const recalledMessage = z.object({ content: z.string(), images: z.array(imageAttachment), clearedAfter: z.number().int().nonnegative() }).meta({ id: "RecalledMessage" });
export const userMessage = z.object({ content: z.string(), images: z.array(imageAttachment) }).meta({ id: "UserMessage" });
export const receipt = z.object({ submissionId: opaque }).meta({ id: "Receipt" });
export const abortInput = z.strictObject({ target: opaque }).meta({ id: "AbortInput" });
export const problem = z.object({ code: z.string(), message: z.string() }).meta({ id: "Problem" });
const transcriptBlock = z.object({ type: z.enum(["text", "thinking"]), text: z.string() }).meta({ id: "TranscriptBlock" });
export const fileDiff = z.object({ path: z.string(), before: z.string(), after: z.string() }).meta({ id: "FileDiff" });
// File contents a tool changed. They are fetched per entry, on demand, so a
// transcript of many edits stays small on the conversation stream.
export const entryDiffs = z.object({
  patch: z.string().optional().meta({ description: "Unified patch reported by the edit tool" }),
  files: z.array(fileDiff).meta({ description: "Before and after contents of files written by the tool, or changed during a bash call" }),
}).meta({ id: "EntryDiffs" });
export const entry = z.object({
  blocks: z.array(transcriptBlock).optional(),
  id: opaque, kind: z.string(), text: z.string(), error: z.string().optional(), imageCount: z.number().int().positive().optional(),
  toolName: z.string().optional(), toolSucceeded: z.boolean().optional(),
  toolCallId: z.string().optional(), toolTarget: z.string().optional(), toolCommand: z.string().optional(),
  diffCount: z.number().int().positive().optional().meta({ description: "Number of diffs stored for this entry; fetch them with GetEntryDiffs" }),
  diffIncomplete: z.boolean().optional(), durationMs: z.number().int().nonnegative().optional(),
  timestamp: z.number().int().nonnegative().optional(), cacheInput: z.number().int().nonnegative().optional(), cacheRead: z.number().int().nonnegative().optional(), cacheWrite: z.number().int().nonnegative().optional(), cacheModel: z.string().optional(),
  tokens: z.number().int().nonnegative().optional(), added: z.number().int().nonnegative().optional(),
  removed: z.number().int().nonnegative().optional(), outputClipped: z.boolean().optional(),
}).meta({ id: "Entry" });
const toolOutput = z.object({ text: z.string() }).meta({ id: "ToolOutput" });
const entryImages = z.object({ images: z.array(imageAttachment) }).meta({ id: "EntryImages" });
export const page = z.object({ entries: z.array(entry), before: opaque.optional() }).meta({ id: "HistoryPage" });
export const historyBranch = z.object({ conversationId: opaque, parentConversationId: opaque.optional(), at: opaque.optional() }).meta({ id: "HistoryBranch" });
export const historyTree = z.object({ rootConversationId: opaque, branches: z.array(historyBranch) }).meta({ id: "HistoryTree" });
export const historicalPath = z.object({ entries: z.array(entry), before: opaque.optional(), contextTokens: z.number().int().nonnegative() }).meta({ id: "HistoricalPath" });
export const contextUsage = z.object({ tokens: z.number().int().nonnegative(), window: z.number().int().positive() }).meta({ id: "ContextUsage" });
const liveTool = z.object({ callId: z.string().optional(), taskId: opaque.optional(), entryId: opaque.optional(), name: z.string(), status: z.string(), startedAt: z.number().int().nonnegative().optional(), output: z.string().optional(), target: z.string().optional(), command: z.string().optional() }).meta({ id: "LiveTool" });
export const queuedMessage = z.object({ id: opaque, text: z.string(), images: z.number().int().nonnegative(), mode: z.enum(["followUp", "steer"]) }).meta({ id: "QueuedMessage" });
export const view = z.object({
  conversationId: opaque, status: z.enum(["idle", "running", "failed"]),
  partial: z.string().nullable(), partialBlocks: z.array(transcriptBlock).optional(), failure: z.string().optional(),
  abortTarget: opaque.optional(), before: opaque.optional(),
  entries: z.array(entry), tools: z.array(liveTool), queued: z.array(queuedMessage).optional(),
  contextUsage: contextUsage.optional(), selection: selection.optional(),
}).meta({ id: "ConversationView" });
const version = { revision: z.number().int().min(0), generation: z.uuid() };
export const conversationSnapshot = z.object({ type: z.literal("snapshot"), ...version, view }).meta({
  id: "ConversationSnapshot",
  description: "The whole view. The first frame of a stream is a snapshot; a later snapshot (revision + 1) replaces the view.",
});
export const conversationUpdate = z.object({
  type: z.literal("update"), ...version,
  drop: z.number().int().nonnegative().meta({ description: "How many entries to remove from the front of the previous view's entries" }),
  keep: z.number().int().nonnegative().meta({ description: "How many of the previous view's entries, after the dropped ones, to keep" }),
  view,
}).meta({
  id: "ConversationUpdate",
  description: "The next view, as a change to the previous one. Its entries are previous.entries[drop : drop+keep] followed by view.entries; every other field of view is complete.",
});
export const conversationFrame = z.discriminatedUnion("type", [conversationSnapshot, conversationUpdate]).meta({ id: "ConversationFrame" });
export const projectInput = z.strictObject({ cwd: z.string().min(1).max(4096), name: z.string().trim().min(1).max(80) }).meta({ id: "ProjectInput" });
export const projectRenameInput = z.strictObject({ name: z.string().trim().min(1).max(80) }).meta({ id: "ProjectRenameInput" });
export const projectRecord = z.object({ id: opaque, cwd: z.string(), name: z.string() }).meta({ id: "Project" });
export const catalog = z.object({ projects: z.array(projectRecord), conversations: z.array(conversation) }).meta({ id: "Catalog" });
// restoreEntryId is accepted only for compatibility with layouts saved by earlier builds.
export const treePosition = z.strictObject({ conversationId: opaque, entryId: opaque, restoreEntryId: opaque.optional() }).meta({ id: "TreePosition" });
export const pendingFork = z.strictObject({ requestId: opaque, content: z.string().max(32_000), images: z.array(imageAttachment).max(4).optional() }).meta({ id: "PendingFork" });
export const layoutNode = z.strictObject({
  id: z.uuid(), conversationId: opaque.optional(), treePosition: treePosition.optional(), pendingFork: pendingFork.optional(), resumeEntryId: opaque.optional(), name: z.string().trim().min(1).max(80).optional(), widthShare: z.number().min(.01).max(1).optional(), empty: z.literal(true).optional(), clientManaged: z.strictObject({ type: z.literal("terminal") }).optional(), cwd: cwd.optional(),
  axis: z.enum(["horizontal", "vertical"]).optional(), ratio: z.number().min(.15).max(.85).optional(),
  first: z.uuid().optional(), second: z.uuid().optional(),
}).meta({ id: "LayoutNode" });
export const layoutTab = z.strictObject({ id: z.uuid(), name: z.string().trim().min(1).max(80).optional(), projectId: z.uuid().optional(), root: z.uuid(), nodes: z.array(layoutNode).min(1).max(31) }).meta({ id: "LayoutTab" });
export const layout = z.strictObject({
  revision: z.number().int().nonnegative().max(Number.MAX_SAFE_INTEGER),
  activeTab: z.uuid().optional(), activeProject: z.uuid().optional(), closedProjects: z.array(z.uuid()).optional(), tabs: z.array(layoutTab).max(24),
}).meta({ id: "Layout" });
// Unexported schemas with an id, such as toolOutput and entryImages, are still
// in the spec: generate.ts emits every schema registered with an id.
