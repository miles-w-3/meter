import { strict as assert } from "node:assert";
import { canonicalSessionContext } from "../server/claude-code/vendor/src/relay-rewrite.ts";

const environment = "<system-reminder>\n# Environment\ntest\n</system-reminder>";
const model = "<system-reminder>\nYou are powered by the model named Sonnet.\n</system-reminder>";
const date = "<system-reminder>\nToday's date is 2026-11-26.\n</system-reminder>";
const text = (text: string) => ({ type: "text", text });
const imported = () => ({
  messages: [
    { role: "user", content: [text(environment), text(`${model}\n`), text("hello")] },
    { role: "assistant", content: [{ type: "tool_use", id: "toolu_01", name: "mcp__custom-tools__empty", input: {} }] },
    { role: "user", content: [{ type: "tool_result", tool_use_id: "toolu_01", content: [text("tool done\n"), text(date)], cache_control: { type: "ephemeral" } }] },
  ],
});
const expected = imported();
expected.messages[0].content = [text(environment), text(model), text(`${date}\n`), text("hello")];
(expected.messages[2].content[0] as { content: unknown[] }).content = [text("tool done")];
const input = JSON.stringify(imported());
assert.deepEqual(canonicalSessionContext(input), { body: input, outcome: "unchanged" });
const moved = canonicalSessionContext(input, true);
assert.equal(moved.outcome, "moved");
assert.deepEqual(JSON.parse(moved.body), expected);
assert.equal(canonicalSessionContext(moved.body, true).outcome, "unchanged");

const checkUnchanged = (value: unknown) => {
  const body = JSON.stringify(value);
  assert.deepEqual(canonicalSessionContext(body, true), { body, outcome: "unchanged" });
};
const ambiguous = imported();
ambiguous.messages[0].content.push(text(date));
checkUnchanged(ambiguous);
const wrongModel = imported();
wrongModel.messages[0].content[1] = text("not a Claude model reminder\n");
checkUnchanged(wrongModel);
const noNewline = imported();
(noNewline.messages[2].content[0] as { content: unknown[] }).content = [text("tool done"), text(date)];
checkUnchanged(noNewline);
const markedDate = imported();
(markedDate.messages[2].content[0] as { content: unknown[] }).content = [text("tool done\n"), { ...text(date), cache_control: { type: "ephemeral" } }];
checkUnchanged(markedDate);
// The older context-plus-date path remains independent of the date-only case.
const classic = imported();
const context = "<system-reminder>\nAs you answer the user's questions, you can use the following context:\ntest\n</system-reminder>";
(classic.messages[2].content[0] as { content: unknown[] }).content = [text("tool done\n"), text(context), text(date)];
const classicMoved = canonicalSessionContext(JSON.stringify(classic));
assert.equal(classicMoved.outcome, "moved");
const classicExpected = structuredClone(expected);
classicExpected.messages[0].content.splice(2, 0, text(context));
assert.deepEqual(JSON.parse(classicMoved.body), classicExpected);

const multipleResults = imported();
multipleResults.messages[2].content.push({ type: "tool_result", tool_use_id: "toolu_02", content: [text("other")] } as never);
checkUnchanged(multipleResults);
console.log("Claude relay date-only layout: moved, idempotent, ambiguous layouts unchanged");
