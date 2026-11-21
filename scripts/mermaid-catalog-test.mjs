import assert from "node:assert/strict";
import { queryMermaidCatalog, mermaidCatalogTool } from "../server/tools/mermaid-catalog.ts";

const all = queryMermaidCatalog({ operation: "list" });
assert.equal(all.diagram_types.length, 33);
assert.deepEqual(all.icons, ["cloud", "database", "disk", "internet", "server"]);
assert.equal(new Set(all.diagram_types).size, 33);
assert.deepEqual(queryMermaidCatalog({ operation: "list", kind: "icon" }).diagram_types, []);
const matches = queryMermaidCatalog({ operation: "search", query: "gateway" }).results;
assert(matches.some((entry) => entry.kind === "icon" && entry.name === "internet"));
assert.equal(queryMermaidCatalog({ operation: "search", query: "server" }).results[0].name, "server");
assert.equal(queryMermaidCatalog({ operation: "lookup", kind: "icon", name: "SERVER" }).result?.name, "server");
assert.equal(queryMermaidCatalog({ operation: "lookup", kind: "icon", name: "aws-vpc" }).result, null);
assert.throws(() => queryMermaidCatalog({ operation: "lookup", name: "server" }));
assert.throws(() => queryMermaidCatalog({ operation: "search", query: "" }));
assert.throws(() => queryMermaidCatalog({ operation: "list", client: "desktop" }));
assert.equal(mermaidCatalogTool().name, "mermaid_catalog");
console.log("mermaid catalog tests passed");
