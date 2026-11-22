import assert from "node:assert/strict";
import { randomUUID } from "node:crypto";
import { layout } from "../contracts/schemas.ts";
import { InvalidLayout, Workspaces } from "../server/workspaces.ts";

const store = new Workspaces(":memory:");
try {
  const project = store.register("/terminal-test", "Terminal test");
  const node = { id: randomUUID(), clientManaged: { type: "terminal" }, cwd: "/terminal-test", name: "Shell", widthShare: 1 };
  const tab = { id: randomUUID(), projectId: project.id, root: node.id, nodes: [node] };
  const input = { revision: 0, activeProject: project.id, activeTab: tab.id, tabs: [tab] };
  const verify = () => { throw new Error("terminal must not require a conversation"); };
  for (const fields of [
    { empty: true }, { conversationId: "1" }, { axis: "horizontal" }, { first: randomUUID() },
    { treePosition: { conversationId: "1", entryId: "2" } },
    { pendingFork: { requestId: "fork-1", content: "hello" } },
    { resumeEntryId: "2" },
  ]) {
    const invalid = structuredClone(input);
    Object.assign(invalid.tabs[0].nodes[0], fields);
    await assert.rejects(store.save("invalid", layout.parse(invalid), verify), InvalidLayout);
  }
  for (const descriptor of [{ type: "other" }, { type: "terminal", extra: true }, {}, true]) {
    const invalid = structuredClone(input);
    invalid.tabs[0].nodes[0].clientManaged = descriptor;
    assert.equal(layout.safeParse(invalid).success, false);
  }
  const saved = await store.save("workspace", layout.parse(input), verify);
  assert.deepEqual(store.get("workspace"), saved);
  assert.deepEqual(saved.tabs[0].nodes[0].clientManaged, { type: "terminal" });
  const tree = structuredClone(input);
  tree.tabs[0].nodes[0] = {
    id: node.id, conversationId: "1", treePosition: { conversationId: "2", entryId: "3" },
    pendingFork: { requestId: "fork-1", content: "hello" },
  };
  const verified = [];
  await store.save("tree", layout.parse(tree), async (id) => { verified.push(id); });
  assert.deepEqual(verified.sort(), ["1", "2"]);
  const combined = structuredClone(input);
  combined.tabs[0].name = "Terminal";
  const treeTab = structuredClone(tree.tabs[0]);
  treeTab.id = randomUUID();
  treeTab.name = "History";
  treeTab.root = randomUUID();
  treeTab.nodes[0].id = treeTab.root;
  combined.tabs.push(treeTab);
  const combinedVerified = [];
  const together = await store.save("combined", layout.parse(combined), async (id) => { combinedVerified.push(id); });
  assert.deepEqual(combinedVerified.sort(), ["1", "2"]);
  assert.deepEqual(store.get("combined"), together);
  console.log("Client-managed terminal layout validation and restore passed");
} finally {
  store.db.close();
}
