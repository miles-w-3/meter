import assert from "node:assert/strict";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { BACKGROUND_CONTEXT as context } from "@earendil-works/chord/context";
import { CompactionEntry } from "@earendil-works/pi-durable";
import { openHarness } from "../server/durable.ts";

const data = await mkdtemp(join(tmpdir(), "meter-compaction-"));
const db = join(data, "durable.sqlite");
let { harness } = await openHarness(db, "fixture");
try {
  // Force a very low automatic threshold while leaving the policy's enabled
  // flag to Meter's harness. With Durable's default this triggers compaction.
  const conversation = await harness.createConversation({
    ownership: { kind: "ownerless" },
    agent: {
      cwd: data, model: { provider: "faux", modelId: "faux-1" },
      compaction: { reserveTokens: 127900, keepRecentTokens: 1, backgroundTokens: 0 },
    },
  }, context);
  for (let i = 0; i < 3; i++) {
    await conversation.submit({ type: "input", requestId: "compaction-" + i, content: `turn ${i}: ` + "long context ".repeat(100) }, context);
    await conversation.waitForIdle(context);
  }
  const entries = (await conversation.entries({ order: "ascending" }, 100, undefined, context)).items;
  assert.ok(entries.filter((entry) => entry.kind === "pi.assistant").length >= 3, "all generations completed");
  assert.ok(!entries.some((entry) => CompactionEntry.is(entry)), "automatic compaction must remain disabled");
  await harness.close(context);
  ({ harness } = await openHarness(db, "fixture"));
  const reopened = await harness.conversation(conversation.id, context);
  await reopened.submit({ type: "input", requestId: "compaction-reopened", content: "continue after restart" }, context);
  await reopened.waitForIdle(context);
  const afterRestart = (await reopened.entries({ order: "ascending" }, 100, undefined, context)).items;
  assert.ok(!afterRestart.some((entry) => CompactionEntry.is(entry)), "automatic compaction must remain disabled after restart");
  console.log("Automatic compaction remains disabled across restart");
} finally {
  await harness.close(context);
  await rm(data, { recursive: true, force: true });
}
