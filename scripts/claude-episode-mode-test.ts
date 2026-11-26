import { strict as assert } from "node:assert";
import { Option } from "effect";
import type { Message } from "@earendil-works/pi-ai";
import { decideEpisodeMode, type LaunchSignature, type ParkedProcessFacts } from "../server/claude-code/vendor/src/episode-mode.ts";

const signature: LaunchSignature = { model: "model", prompt: "prompt", cwd: "/tmp", handoff: true };
const assistant = { role: "assistant", provider: "pi-claude-cli", content: [] } as unknown as Message;
const result = { role: "toolResult", toolCallId: "toolu_1", content: [] } as unknown as Message;
const user = { role: "user", content: "steer" } as unknown as Message;
const parked: ParkedProcessFacts = { signature, acknowledged: Option.some({ length: 1, hash: "match" }), turnActive: true, awaitsToolResults: true };
const decide = (messages: Message[], facts = parked, sig = signature, hash = "match") =>
  decideEpisodeMode(Option.some(facts), { signature: sig, messages, historyHashOfPrefix: () => hash });
assert.deepEqual(decide([assistant, result]), { mode: "handoff" });
assert.deepEqual(decide([assistant, result, user]), { mode: "spawn", reason: "handoffMismatch" });
assert.deepEqual(decide([assistant, result], { ...parked, awaitsToolResults: false }), { mode: "spawn", reason: "handoffMismatch" });
assert.deepEqual(decide([assistant, result], parked, signature, "changed"), { mode: "spawn", reason: "history" });
assert.deepEqual(decide([assistant, result], parked, { ...signature, cwd: "/other" }), { mode: "spawn", reason: "signature.cwd" });
assert.deepEqual(decide([assistant, user], { ...parked, turnActive: false }), { mode: "turn" });
assert.deepEqual(decideEpisodeMode(Option.none(), { signature, messages: [assistant, result], historyHashOfPrefix: () => "match" }), { mode: "spawn", reason: "noParked" });
console.log("Claude episode modes: handoff, steering mismatch, history and cwd tested");
