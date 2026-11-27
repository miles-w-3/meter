import assert from "node:assert/strict";
import { FrameEncoder, historyCursor } from "../server/durable.ts";
import { conversationFrame } from "../contracts/schemas.ts";

const generation = "d6f2c699-f2dc-4884-86af-ccc78f16a35d";
const entry = (id, text = "entry " + id) => ({ id: String(id), kind: "pi.assistant", text });
const view = (ids, extra = {}) => ({ conversationId: "1", status: "idle", partial: null, entries: ids.map((id) => entry(id)), tools: [], ...extra });

// What a client holds after each frame, rebuilt as the Go client does.
function client() {
  let held = [];
  let revision = -1;
  return (encoded) => {
    assert.ok(encoded, "frame fits");
    const frame = conversationFrame.parse(JSON.parse(encoded.json));
    assert.equal(frame.revision, revision + 1);
    revision = frame.revision;
    held = frame.type === "update" ? [...held.slice(frame.drop, frame.drop + frame.keep), ...frame.view.entries] : frame.view.entries;
    return { frame, ids: held.map((item) => item.id), before: frame.view.before };
  };
}

{
  const frames = new FrameEncoder(generation);
  const apply = client();
  let step = apply(frames.next(view([1, 2, 3])));
  assert.equal(step.frame.type, "snapshot");
  assert.deepEqual(step.ids, ["1", "2", "3"]);

  step = apply(frames.next(view([1, 2, 3], { status: "running", partial: "streaming" })));
  assert.equal(step.frame.type, "update");
  assert.deepEqual([step.frame.drop, step.frame.keep, step.frame.view.entries.length], [0, 3, 0], "a token sends no entries");
  assert.equal(step.frame.view.partial, "streaming");

  step = apply(frames.next(view([1, 2, 3, 4])));
  assert.deepEqual([step.frame.drop, step.frame.keep, step.frame.view.entries.map((item) => item.id)], [0, 3, ["4"]]);
  step = apply(frames.next(view([3, 4, 5])));
  assert.deepEqual([step.frame.drop, step.frame.keep], [2, 2], "the window slides");
  assert.deepEqual(step.ids, ["3", "4", "5"]);
  step = apply(frames.next(view([9])));
  assert.deepEqual(step.ids, ["9"], "the window can jump past every held entry");
}

{
  // A view too large for one frame leaves out its oldest entries, and keeps
  // them out: the client pages them in through the before cursor.
  const big = (ids) => ({ ...view(ids), entries: ids.map((id) => entry(id, "x".repeat(400))) });
  const frames = new FrameEncoder(generation, 1500);
  const apply = client();
  let step = apply(frames.next(big([1, 2, 3, 4, 5, 6])));
  assert.equal(step.frame.type, "snapshot");
  assert.ok(step.ids.length < 6 && step.ids.length > 0, "trimmed to fit: " + step.ids);
  assert.equal(step.ids.at(-1), "6");
  assert.equal(step.before, historyCursor(Number(step.ids[0])));
  const kept = step.ids;
  step = apply(frames.next(big([1, 2, 3, 4, 5, 6, 7])));
  assert.equal(step.frame.type, "update", "later frames stay small updates");
  assert.ok(!step.ids.includes(String(Number(kept[0]) - 1)), "left-out entries stay out");
  assert.equal(step.ids.at(-1), "7");

  // Nothing fits: the server closes the stream instead of looping.
  assert.equal(new FrameEncoder(generation, 50).next(view([1])), undefined);
}

console.log("Stream frame encoding passed");
