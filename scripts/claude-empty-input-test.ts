import { strict as assert } from "node:assert";
import { createAssistantMessageEventStream, type Api, type Model } from "@earendil-works/pi-ai";
import { createEventBridge } from "../server/claude-code/vendor/src/event-bridge.ts";

// Model output seen in the local Claude 2.1.332 mock: whitespace is not the
// same as no input delta. This documents the current fail-closed mismatch.
const exercise = (delta: string | undefined, recorded: unknown) => {
  const stream = createAssistantMessageEventStream();
  const bridge = createEventBridge(stream, { id: "model", provider: "pi-claude-cli" } as Model<Api>);
  bridge.handleEvent({ type: "message_start", message: { id: "msg_1" } });
  bridge.handleEvent({ type: "content_block_start", index: 0, content_block: { type: "tool_use", id: "toolu_1", name: "mcp__custom-tools__empty" } });
  if (delta !== undefined) bridge.handleEvent({ type: "content_block_delta", index: 0, delta: { type: "input_json_delta", partial_json: delta } });
  bridge.handleEvent({ type: "content_block_stop", index: 0 });
  bridge.recordAssistantBlocks("msg_1", [{ type: "tool_use", id: "toolu_1", name: "mcp__custom-tools__empty", input: recorded }]);
  return { output: bridge.getOutput(), mismatch: bridge.streamMismatch() };
};
assert.equal(exercise(undefined, {}).mismatch, undefined);
assert.equal(exercise("{}", {}).mismatch, undefined);
const whitespace = exercise("   ", { __unparsedToolInput: { raw: "   ", len: 3 } });
assert.deepEqual((whitespace.output.content[0] as { arguments: unknown }).arguments, {});
assert.match(whitespace.mismatch ?? "", /differs/);
console.log("Claude input bridge: no-delta and {} match; whitespace triggers recorded-input mismatch");
