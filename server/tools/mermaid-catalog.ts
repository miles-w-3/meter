import { defineTool } from "@earendil-works/pi-durable";
import { z } from "zod";
import { catalogEntries } from "./mermaid-catalog-data.ts";

const kind = z.enum(["diagram_type", "icon", "feature"]);
const parameters = z.strictObject({
  operation: z.enum(["list", "search", "lookup"]),
  kind: kind.optional(),
  query: z.string().trim().min(1).max(200).optional(),
  name: z.string().trim().min(1).max(100).optional(),
});
const schema = z.toJSONSchema(parameters);
delete schema.$schema;

// Catalog content is bundled, not taken from a project or the network. It
// describes the pinned preview renderer, not a runtime client handshake.
export function queryMermaidCatalog(input: unknown) {
  const { operation, kind, query, name } = parameters.parse(input);
  const entries = catalogEntries.filter((entry) => !kind || entry.kind === kind);
  const info = { catalogVersion: 1, renderer: "pele-go v0.2.0", compatibility: "Renderer availability and client version are not verified by this tool." };
  if (operation === "list") {
    if (query || name) throw new Error("list accepts only an optional kind");
    return { ...info, diagram_types: entries.filter((e) => e.kind === "diagram_type").map((e) => e.name), icons: entries.filter((e) => e.kind === "icon").map((e) => e.name), features: entries.filter((e) => e.kind === "feature").map((e) => e.name) };
  }
  if (operation === "lookup") {
    if (!kind || !name || query) throw new Error("lookup requires kind and name");
    const entry = entries.find((e) => e.name.toLowerCase() === name.toLowerCase());
    return { ...info, result: entry ?? null };
  }
  if (!query || name) throw new Error("search requires query and an optional kind");
  const text = query.toLowerCase();
  const matches = entries.map((entry) => {
    const searchable = [entry.name, entry.description, ...( "keywords" in entry ? entry.keywords : [])].join(" ").toLowerCase();
    const score = entry.name.toLowerCase() === text ? 2 : searchable.includes(text) ? 1 : 0;
    return { entry, score };
  }).filter((item) => item.score > 0).sort((a, b) => b.score - a.score || a.entry.name.localeCompare(b.entry.name));
  return { ...info, results: matches.slice(0, 20).map((item) => item.entry), truncated: matches.length > 20 };
}

export function mermaidCatalogTool() {
  return defineTool({
    name: "mermaid_catalog",
    description: "List, search or look up tested Mermaid diagram types, features and built-in architecture icons. Use it for specialized Mermaid syntax or infrastructure icons. The catalog targets pele-go v0.2.0; actual client availability is not verified.",
    parameters: schema,
    outputLimits: { maxBytes: 32768 },
    replay: "safe",
    execute: async (args) => {
      try { return { output: [{ type: "text" as const, text: JSON.stringify(queryMermaidCatalog(args)) }] }; }
      catch (error) { return { isError: true, output: [{ type: "text" as const, text: error instanceof Error ? error.message : "Invalid catalog request" }] }; }
    },
  });
}
