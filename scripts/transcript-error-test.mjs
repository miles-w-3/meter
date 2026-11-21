import assert from "node:assert/strict";
import { projectEntries } from "../server/durable.ts";
import { entry, page, view } from "../contracts/schemas.ts";

const assistant = (id, content, stopReason, errorMessage) => ({
  id, conversationId: 1, kind: "pi.assistant",
  model: [{ role: "assistant", content: content ? [{ type: "text", text: content }] : [],
    stopReason, errorMessage, usage: { totalTokens: 0 } }],
});
const { projected } = projectEntries([
  assistant(1, "", "error", "prefill memory exceeded"),
  assistant(2, "Partial response", "error", "connection lost"),
  assistant(3, "Complete", "stop", "not an error"),
]);
assert.equal(projected[0].error, "prefill memory exceeded");
assert.equal(projected[0].text, "");
assert.equal(projected[1].error, "connection lost");
assert.equal(projected[1].text, "Partial response");
assert.equal(projected[2].error, undefined);
for (const item of projected) entry.parse(item);
page.parse({ entries: projected });
view.parse({ conversationId: "1", status: "idle", partial: null, entries: projected, tools: [] });
console.log("Transcript error projection passed");
