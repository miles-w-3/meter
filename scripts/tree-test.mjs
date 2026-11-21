import assert from "node:assert/strict";
import { mkdtemp, rm } from "node:fs/promises";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { BACKGROUND_CONTEXT as context } from "@earendil-works/chord/context";
import { openHarness } from "../server/durable.ts";
import { discoverHistoryTree, historyTree } from "../server/tree.ts";

const data = await mkdtemp(join(tmpdir(), "meter-tree-"));
const db = join(data, "durable.sqlite");
let { harness } = await openHarness(db, "fixture");
try {
  const root = await harness.createConversation({ ownership: { kind: "ownerless" }, agent: { cwd: data, model: { provider: "faux", modelId: "faux-1" } } }, context);
  await root.submit({ type: "input", content: "root message", requestId: "one" }, context);
  await root.waitForIdle(context);
  const entries = (await root.entries({ order: "ascending" }, 100, undefined, context)).items;
  const at = entries.find((entry) => entry.kind === "pi.assistant").id;
  const left = await root.fork(at, { ownership: { kind: "ownerless" } }, context);
  const right = await root.fork(at, { ownership: { kind: "ownerless" } }, context);
  const unrelated = await harness.createConversation({ ownership: { kind: "ownerless" }, agent: { cwd: data } }, context);
  const expected = {
    rootConversationId: String(root.id),
    branches: [
      { conversationId: String(root.id) },
      { conversationId: String(left.id), parentConversationId: String(root.id), at: String(at) },
      { conversationId: String(right.id), parentConversationId: String(root.id), at: String(at) },
    ],
  };
  assert.deepEqual(await discoverHistoryTree(harness, left.id), expected);
  assert.deepEqual(await discoverHistoryTree(harness, right.id), expected);
  assert.deepEqual(await discoverHistoryTree(harness, unrelated.id), { rootConversationId: String(unrelated.id), branches: [{ conversationId: String(unrelated.id) }] });
  await harness.close(context);
  ({ harness } = await openHarness(db, "fixture"));
  assert.deepEqual(await discoverHistoryTree(harness, left.id), expected);
  assert.equal(historyTree([], 999), undefined);
  assert.throws(() => historyTree([{ id: 1, parent: { conversationId: 2, at: 1 } }, { id: 2, parent: { conversationId: 1, at: 1 } }], 1), /cycle/);
  console.log("History tree discovers sibling forks and survives restart");
} finally {
  await harness.close(context);
  await rm(data, { recursive: true, force: true });
}
