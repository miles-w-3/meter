import { strict as assert } from "node:assert";
import { createAssistantMessageEventStream, type Api, type Model } from "@earendil-works/pi-ai";
import { createEventBridge } from "../server/claude-code/vendor/src/event-bridge.ts";

// Streamed tool arguments are parsed when the object completes, not after
// every input_json_delta: a large write in many small chunks must cost linear
// time, while the streaming partial and the final call stay what re-parsing
// every delta gave. Run on its own or through claude-provider-test.mjs.
const parse = JSON.parse;
let parses = 0;
const countingParse: typeof JSON.parse = (text, reviver) => {
  parses++;
  return parse(text, reviver);
};
const isObject = (value: unknown) => typeof value === "object" && value !== null && !Array.isArray(value);

/** What the bridge showed when it re-parsed after every delta, and at stop. */
const reference = (chunks: string[]) => {
  let buffer = "";
  let args: unknown = {};
  const partials = chunks.map((chunk) => {
    buffer += chunk;
    try {
      const parsed = parse(buffer);
      if (isObject(parsed)) args = parsed;
    } catch {}
    return args;
  });
  let final: unknown = buffer;
  if (buffer.trim() === "") final = {};
  else
    try {
      const parsed = parse(buffer);
      if (isObject(parsed)) final = parsed;
    } catch {}
  return { partials, final };
};

const streamTool = (chunks: string[]) => {
  const bridge = createEventBridge(createAssistantMessageEventStream(), { id: "model", provider: "pi-claude-cli" } as Model<Api>);
  const args = () => (bridge.getOutput().content[0] as { arguments: unknown }).arguments;
  bridge.handleEvent({ type: "message_start", message: { id: "msg_1" } });
  bridge.handleEvent({ type: "content_block_start", index: 0, content_block: { type: "tool_use", id: "toolu_1", name: "mcp__custom-tools__write" } });
  const partials: unknown[] = [];
  parses = 0;
  JSON.parse = countingParse;
  try {
    for (const chunk of chunks) {
      bridge.handleEvent({ type: "content_block_delta", index: 0, delta: { type: "input_json_delta", partial_json: chunk } });
      partials.push(args());
    }
  } finally {
    JSON.parse = parse;
  }
  const streamingParses = parses;
  bridge.handleEvent({ type: "content_block_stop", index: 0 });
  return { partials, final: args(), streamingParses };
};

const split = (text: string, size: number) => {
  const chunks: string[] = [];
  for (let i = 0; i < text.length; i += size) chunks.push(text.slice(i, i + size));
  return chunks;
};

// A ~100 KB file write in ~2,000 chunks: braces, quotes and backslashes in
// the content must not end the object early, and it is parsed once.
const line = 'export const f = (s: string) => { if (s === "}") return "\\\\\\"{[]}"; return `é ${s}`; }\n';
const input = { path: "src/big.ts", content: line.repeat(Math.ceil(100_000 / line.length)) };
const big = split(JSON.stringify(input), 50);
assert.ok(big.length >= 2_000, `streams ${big.length} chunks`);
const streamed = streamTool(big);
assert.deepEqual(streamed.final, input, "final arguments of a large streamed input");
assert.deepEqual(streamed.partials.at(-1), input, "the completing delta shows the arguments");
assert.ok(streamed.partials.slice(0, -1).every((args) => isObject(args) && Object.keys(args as object).length === 0));
assert.equal(streamed.streamingParses, 1, "a streamed tool input is parsed once while streaming");

// Every chunking of awkward inputs shows what per-delta parsing showed.
const tricky = JSON.stringify({ a: 'q"}\\', b: [{ c: "}]" }, []], d: "é {", e: '\\"', f: {} });
const cases = [
  tricky,
  ` \n${tricky}\t \r\n`,
  `${tricky} x`,
  `${tricky}}`,
  '{"u":"\\u00e9\\n\\\\"}',
  '{"a":1',
  '{"a":[}]',
  "{}",
  "[1,2]",
  '"text"',
  "null",
  "   ",
  "",
];
for (const text of cases) {
  const chunkings = [split(text, 1), [text]];
  for (let at = 1; at < text.length; at++) chunkings.push([text.slice(0, at), text.slice(at)]);
  for (const chunks of chunkings) {
    const actual = streamTool(chunks);
    const expected = reference(chunks);
    assert.deepEqual(actual.partials, expected.partials, `partials of ${JSON.stringify(chunks)}`);
    assert.deepEqual(actual.final, expected.final, `final of ${JSON.stringify(chunks)}`);
    assert.ok(actual.streamingParses <= 1, `parses of ${JSON.stringify(chunks)}`);
  }
}
console.log("Claude tool input: a 100 KB input in 2,000 deltas is parsed once; chunkings match per-delta parsing");
