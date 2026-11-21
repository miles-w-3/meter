import { writeFile } from "node:fs/promises";
import { z } from "zod";
import { models, conversationFrame } from "./schemas.ts";

const schemas = Object.fromEntries(Object.entries(models).map(([name, model]) => {
  const schema = z.toJSONSchema(model, { target: "draft-2020-12" });
  delete schema.$schema;
  return [name, schema];
}));
const ref = (name: keyof typeof models) => ({ $ref: "#/components/schemas/" + name });
for (const name of ["ConversationSnapshot", "ConversationUpdate"]) {
  Object.assign(schemas[name].properties!, { view: ref("ConversationView") });
}
Object.assign((schemas.EntryImages.properties!.images as object), { items: ref("ImageAttachment") });
Object.assign((schemas.RecalledMessage.properties!.images as object), { items: ref("ImageAttachment") });
Object.assign((schemas.SubmitInput.properties!.images as object), { items: ref("ImageAttachment") });
Object.assign(schemas.ConversationView.properties!, { contextUsage: ref("ContextUsage"), selection: ref("Selection") });
Object.assign(schemas.Conversation.properties!, { selection: ref("Selection") });
Object.assign(schemas.CreateInput.properties!, { selection: ref("SelectionInput") });
Object.assign(schemas.SelectionChange.properties!, { expected: ref("Selection") });
Object.assign((schemas.ConversationView.properties!.entries as object), { items: ref("Entry") });
Object.assign((schemas.ConversationView.properties!.queued as object), { items: ref("QueuedMessage") });
Object.assign((schemas.HistoryPage.properties!.entries as object), { items: ref("Entry") });
for (const [model, property, item] of [
  ["Catalog", "projects", "Project"], ["Catalog", "conversations", "Conversation"],
  ["Layout", "tabs", "LayoutTab"], ["LayoutTab", "nodes", "LayoutNode"],
  ["ProviderInfo", "methods", "AuthMethod"], ["ModelInventory", "providers", "ProviderInfo"], ["ModelInventory", "models", "ModelInfo"],
] as const) {
  Object.assign(schemas[model].properties![property] as object, { items: ref(item) });
}
const webSearchProperties = (schemas.ToolsStatus.properties!.webSearch as unknown as { properties: Record<string, object> }).properties;
Object.assign(webSearchProperties.providers!, { items: ref("SearchProviderStatus") });
const toolChangeProperties = (schemas.ToolSettingsChange.properties!.webSearch as unknown as { properties: Record<string, object> }).properties;
toolChangeProperties.provider = ref("SearchProviderChange");
const body = (name: keyof typeof models) => ({ "application/json": { schema: ref(name) } });
const response = (description: string, name: keyof typeof models) => ({ description, content: body(name) });
Object.assign(schemas, {
  ConversationFrame: {
    oneOf: [ref("ConversationSnapshot"), ref("ConversationUpdate")],
    discriminator: {
      propertyName: "type",
      mapping: { snapshot: "#/components/schemas/ConversationSnapshot", update: "#/components/schemas/ConversationUpdate" },
    },
  },
});
const id = { in: "path", name: "id", required: true, schema: { type: "string" } };
const failures = {
  "400": response("Invalid request", "Problem"),
  "404": response("Unknown conversation", "Problem"),
  "409": response("Control conflict", "Problem"),
  "500": response("Server error", "Problem"),
};
const post = (operationId: string, input: keyof typeof models, status: string, output?: keyof typeof models) => ({
  operationId,
  requestBody: { required: true, content: body(input) },
  responses: { [status]: output ? response("Accepted", output) : { description: "Accepted" }, ...failures },
});
const spec = {
  openapi: "3.1.0",
  info: { title: "Pi Durable Workspace", version: "1.0.0" },
  paths: {
    "/v1/server": { get: { operationId: "GetServer", responses: { "200": response("Handshake", "ServerInfo") } } },
    "/v1/server/stop": { post: { operationId: "StopServer", responses: { "202": { description: "Stopping" } } } },
    "/v1/catalog": { get: { operationId: "GetCatalog", responses: { "200": response("Catalog", "Catalog"), ...failures } } },
    "/v1/models": { get: { operationId: "GetModels", responses: { "200": response("Provider and model inventory", "ModelInventory"), ...failures } } },
    "/v1/settings/tools": {
      get: { operationId: "GetTools", responses: { "200": response("Tool settings and status", "ToolsStatus"), ...failures } },
      patch: post("UpdateTools", "ToolSettingsChange", "200", "ToolsStatus"),
    },
    "/v1/settings/defaults": {
      get: { operationId: "GetDefaults", responses: { "200": response("Default model settings", "Settings"), ...failures } },
      put: post("SetDefaults", "DefaultSettings", "200", "Settings"),
    },
    "/v1/directories/resolve": { post: post("ResolveCwd", "CwdInput", "200", "CwdInput") },
    "/v1/projects": { post: post("RegisterProject", "ProjectInput", "201", "Project") },
    "/v1/projects/{id}": { parameters: [id], put: post("RenameProject", "ProjectRenameInput", "200", "Project") },
    "/v1/workspaces/{id}": { parameters: [id],
      get: { operationId: "GetLayout", responses: { "200": response("Layout", "Layout"), ...failures } },
      put: post("SaveLayout", "Layout", "200", "Layout"),
    },
    "/v1/conversations": { post: post("CreateConversation", "CreateInput", "201", "Conversation") },
    "/v1/conversations/{id}": { parameters: [id], get: {
      operationId: "GetConversation", responses: { "200": response("Conversation", "Conversation"), ...failures },
    } },
    "/v1/conversations/{id}/title": { parameters: [id], put: post("RenameConversation", "ProjectRenameInput", "200", "Conversation") },
    "/v1/conversations/{id}/cwd": { parameters: [id], put: post("SetConversationCwd", "CwdInput", "200", "Conversation") },
    "/v1/conversations/{id}/model": { parameters: [id], put: post("SetConversationModel", "SelectionChange", "200", "Conversation") },
    "/v1/conversations/{id}/entries": { parameters: [id], get: {
      operationId: "GetEntries",
      parameters: [{ in: "query", name: "before", schema: { type: "string" } }],
      responses: { "200": response("Transcript page", "HistoryPage"), ...failures },
    } },
    "/v1/conversations/{id}/entries/{entryId}/images": { parameters: [id, { ...id, name: "entryId" }], get: {
      operationId: "GetEntryImages", responses: { "200": response("Stored message images", "EntryImages"), ...failures },
    } },
    "/v1/conversations/{id}/entries/{entryId}/output": { parameters: [id, { ...id, name: "entryId" }], get: {
      operationId: "GetToolOutput", responses: { "200": response("Stored tool output", "ToolOutput"), ...failures },
    } },
    "/v1/conversations/{id}/messages": { parameters: [id], post: post("SubmitMessage", "SubmitInput", "202", "Receipt") },
    "/v1/conversations/{id}/queue/{submission}/recall": { parameters: [id, { in: "path", name: "submission", required: true, schema: { type: "string" } }], post: { operationId: "RecallQueued", responses: { "200": response("Recalled", "RecalledMessage"), ...failures } } },
    "/v1/conversations/{id}/queue/{submission}/steer": { parameters: [id, { in: "path", name: "submission", required: true, schema: { type: "string" } }], post: { operationId: "SteerQueued", responses: { "202": { description: "Steering" }, ...failures } } },
    "/v1/conversations/{id}/abort": { parameters: [id], post: post("AbortRun", "AbortInput", "202") },
  },
  components: { schemas },
};
const base = {
  conversationId: "conversation-1",
  status: "idle",
  partial: null,
  entries: [{ id: "entry-1", kind: "user", text: "hello" }],
  tools: [],
};
const fixtures = [
  { type: "snapshot", generation: "d6f2c699-f2dc-4884-86af-ccc78f16a35d", revision: 0, view: base },
  { type: "update", generation: "d6f2c699-f2dc-4884-86af-ccc78f16a35d", revision: 1, view: {
    ...base, status: "running", partial: "streaming",
    entries: [{ id: "entry-2", kind: "tool", text: "file", toolName: "read" }],
  } },
].map((value) => conversationFrame.parse(value));
const json = (value: unknown) => JSON.stringify(value, null, 2) + "\n";
await writeFile(new URL("openapi.generated.json", import.meta.url), json(spec));
await writeFile(new URL("../go/internal/protocol/testdata/frames.json", import.meta.url), json(fixtures));
