import { writeFile } from "node:fs/promises";
import { z } from "zod";
import { conversationFrame } from "./schemas.ts";

// Every schema registered with an id in schemas.ts becomes a component, and
// zod refers to it by $ref wherever another schema uses it.
const { schemas } = z.toJSONSchema(z.globalRegistry, { target: "draft-2020-12", uri: (id) => "#/components/schemas/" + id });
if ("__shared" in schemas) throw new Error("A schema without an id is shared recursively; give it an id in schemas.ts");
for (const schema of Object.values(schemas)) {
  for (const key of ["$schema", "$id", "id"] as const) delete (schema as Record<string, unknown>)[key];
}
type Model = keyof typeof schemas;
const ref = (name: Model) => {
  if (!(name in schemas)) throw new Error("Unknown schema " + name);
  return { $ref: "#/components/schemas/" + name };
};
// oapi-codegen needs the discriminator mapping to decode the union.
Object.assign(schemas, {
  ConversationFrame: {
    oneOf: [ref("ConversationSnapshot"), ref("ConversationUpdate")],
    discriminator: {
      propertyName: "type",
      mapping: { snapshot: "#/components/schemas/ConversationSnapshot", update: "#/components/schemas/ConversationUpdate" },
    },
  },
});
const body = (name: Model) => ({ "application/json": { schema: ref(name) } });
const response = (description: string, name: Model) => ({ description, content: body(name) });
const id = { in: "path", name: "id", required: true, schema: { type: "string" } };
const failures = {
  "400": response("Invalid request", "Problem"),
  "404": response("Not found", "Problem"),
  "409": response("Conflict", "Problem"),
  "500": response("Server error", "Problem"),
};
const operation = (operationId: string, input: Model, status: string, output?: Model) => ({
  operationId,
  requestBody: { required: true, content: body(input) },
  responses: { [status]: output ? response("OK", output) : { description: "Accepted" }, ...failures },
});
const entry = [id, { ...id, name: "entryId" }];
const spec = {
  openapi: "3.1.0",
  info: { title: "Pi Durable Workspace", version: "1.0.0" },
  paths: {
    "/v1/server": { get: { operationId: "GetServer", responses: { "200": response("Handshake", "ServerInfo") } } },
    "/v1/server/stop": { post: { operationId: "StopServer", responses: { "202": { description: "Stopping" } } } },
    "/v1/catalog": { get: { operationId: "GetCatalog", responses: { "200": response("Catalog", "Catalog"), ...failures } } },
    "/v1/models": { get: { operationId: "GetModels", responses: { "200": response("Provider and model inventory", "ModelInventory"), ...failures } } },
    "/v1/providers/{id}": { parameters: [id], put: operation("SetProvider", "ProviderSetting", "200", "ModelInventory") },
    "/v1/providers/{id}/refresh": { parameters: [id], post: { operationId: "RefreshProvider", responses: { "200": response("Provider and model inventory", "ModelInventory"), ...failures } } },
    "/v1/settings/tools": {
      get: { operationId: "GetTools", responses: { "200": response("Tool settings and status", "ToolsStatus"), ...failures } },
      patch: operation("UpdateTools", "ToolSettingsChange", "200", "ToolsStatus"),
    },
    "/v1/settings/defaults": {
      get: { operationId: "GetDefaults", responses: { "200": response("Default model settings", "Settings"), ...failures } },
      put: operation("SetDefaults", "DefaultSettings", "200", "Settings"),
    },
    "/v1/directories/resolve": { post: operation("ResolveCwd", "CwdInput", "200", "CwdInput") },
    "/v1/projects": { post: operation("RegisterProject", "ProjectInput", "201", "Project") },
    "/v1/projects/{id}": { parameters: [id], put: operation("RenameProject", "ProjectRenameInput", "200", "Project") },
    "/v1/workspaces/{id}": { parameters: [id],
      get: { operationId: "GetLayout", responses: { "200": response("Layout", "Layout"), ...failures } },
      put: operation("SaveLayout", "Layout", "200", "Layout"),
    },
    "/v1/conversations": { post: operation("CreateConversation", "CreateInput", "201", "Conversation") },
    "/v1/conversations/{id}": { parameters: [id], get: {
      operationId: "GetConversation", responses: { "200": response("Conversation", "Conversation"), ...failures },
    } },
    "/v1/conversations/{id}/tree": { parameters: [id], get: {
      operationId: "GetHistoryTree", responses: { "200": response("Conversation fork ancestry", "HistoryTree"), ...failures },
    } },
    "/v1/conversations/{id}/path": { parameters: [id], get: {
      operationId: "GetHistoricalPath",
      parameters: [
        { in: "query", name: "at", required: true, schema: { type: "string" }, description: "Entry ID on the conversation's history path" },
        { in: "query", name: "before", schema: { type: "string" } },
      ],
      responses: { "200": response("Historical transcript page and context usage", "HistoricalPath"), ...failures },
    } },
    "/v1/conversations/{id}/title": { parameters: [id], put: operation("RenameConversation", "ProjectRenameInput", "200", "Conversation") },
    "/v1/conversations/{id}/cwd": { parameters: [id], put: operation("SetConversationCwd", "CwdInput", "200", "Conversation") },
    "/v1/conversations/{id}/model": { parameters: [id], put: operation("SetConversationModel", "SelectionChange", "200", "Conversation") },
    "/v1/conversations/{id}/entries": { parameters: [id], get: {
      operationId: "GetEntries",
      parameters: [{ in: "query", name: "before", schema: { type: "string" } }],
      responses: { "200": response("Transcript page", "HistoryPage"), ...failures },
    } },
    "/v1/conversations/{id}/entries/{entryId}/message": { parameters: entry, get: {
      operationId: "GetUserMessage", responses: { "200": response("Full stored user message", "UserMessage"), ...failures },
    } },
    "/v1/conversations/{id}/entries/{entryId}/images": { parameters: entry, get: {
      operationId: "GetEntryImages", responses: { "200": response("Stored message images", "EntryImages"), ...failures },
    } },
    "/v1/conversations/{id}/entries/{entryId}/output": { parameters: entry, get: {
      operationId: "GetToolOutput", responses: { "200": response("Stored tool output", "ToolOutput"), ...failures },
    } },
    "/v1/conversations/{id}/entries/{entryId}/diffs": { parameters: entry, get: {
      operationId: "GetEntryDiffs", responses: { "200": response("Stored tool diffs", "EntryDiffs"), ...failures },
    } },
    "/v1/conversations/{id}/messages": { parameters: [id], post: operation("SubmitMessage", "SubmitInput", "202", "Receipt") },
    "/v1/conversations/{id}/fork-messages": { parameters: [id], post: operation("ForkAndSubmitMessage", "ForkInput", "202", "ForkReceipt") },
    "/v1/conversations/{id}/queue/{submission}/recall": { parameters: [id, { in: "path", name: "submission", required: true, schema: { type: "string" } }], post: { operationId: "RecallQueued", responses: { "200": response("Recalled", "RecalledMessage"), ...failures } } },
    "/v1/conversations/{id}/queue/{submission}/steer": { parameters: [id, { in: "path", name: "submission", required: true, schema: { type: "string" } }], post: { operationId: "SteerQueued", responses: { "202": { description: "Steering" }, ...failures } } },
    "/v1/conversations/{id}/abort": { parameters: [id], post: operation("AbortRun", "AbortInput", "202") },
  },
  components: { schemas },
};
const generation = "d6f2c699-f2dc-4884-86af-ccc78f16a35d";
const base = {
  conversationId: "conversation-1",
  status: "idle",
  partial: null,
  entries: [{ id: "entry-1", kind: "user", text: "hello" }],
  tools: [],
};
const fixtures = [
  { type: "snapshot", generation, revision: 0, view: base },
  { type: "update", generation, revision: 1, drop: 0, keep: 1, view: {
    ...base, status: "running", partial: "streaming",
    entries: [{ id: "entry-2", kind: "tool", text: "file", toolName: "read", diffCount: 1 }],
  } },
].map((value) => conversationFrame.parse(value));
const json = (value: unknown) => JSON.stringify(value, null, 2) + "\n";
await writeFile(new URL("openapi.generated.json", import.meta.url), json(spec));
await writeFile(new URL("../go/internal/protocol/testdata/frames.json", import.meta.url), json(fixtures));
