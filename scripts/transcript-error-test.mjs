import assert from "node:assert/strict";
import { projectEntries } from "../server/durable.ts";
import { entry, page, view } from "../contracts/schemas.ts";

const timestamp = 1700000000000;
const assistant = (id, content, stopReason, errorMessage, extraBlocks = []) => ({
  id, conversationId: 1, kind: "pi.assistant",
  model: [{ role: "assistant", content: [...(content ? [{ type: "text", text: content }] : []), ...extraBlocks],
    timestamp, stopReason, errorMessage, usage: { totalTokens: 0 } }],
});
const { projected } = projectEntries([
  assistant(1, "", "error", "prefill memory exceeded"),
  assistant(2, "Partial response", "error", "connection lost"),
  assistant(3, "Complete", "stop", "not an error"),
  assistant(4, "Let me check.", "toolUse", undefined, [{ type: "toolCall", id: "read-1", name: "read", arguments: { path: "notes.txt" } }]),
  assistant(5, "   ", "stop"),
  assistant(6, "Output limit reached", "length"),
  assistant(7, "Answer with no calls", "toolUse"),
  assistant(8, "Interrupted", "aborted"),
  { id: 9, conversationId: 1, kind: "pi.user", model: [{ role: "user", content: "Follow-up", timestamp }] },
]);
assert.equal(projected[0].error, "prefill memory exceeded");
assert.equal(projected[0].text, "");
assert.equal(projected[1].error, "connection lost");
assert.equal(projected[1].text, "Partial response");
assert.equal(projected[2].error, undefined);
assert.deepEqual(projected.filter((item) => item.timestamp !== undefined).map((item) => item.id), ["3", "6", "7"]);
assert.equal(projected[2].timestamp, timestamp);
for (const item of projected) entry.parse(item);
page.parse({ entries: projected });
const running = view.parse({ conversationId: "1", status: "running", partial: "New response", entries: projected, tools: [] });
assert.equal(running.entries[2].timestamp, timestamp);
console.log("Transcript error projection passed");
