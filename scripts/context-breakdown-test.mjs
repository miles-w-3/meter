import assert from 'node:assert/strict';
import { contextBreakdown, contextTokens } from '../server/durable.ts';

const now = Date.now();
const messages = [
  {
    role: 'system', content: 'Base instructions', timestamp: now,
    sections: {
      preamble: '<preamble>\nBe useful.\n</preamble>',
      project_context: '<project_context>\nAGENTS.md instructions\n</project_context>',
      skills: '<skills>\n<available_skills>search</available_skills>\n</skills>',
      tools: '<tools>\nRead and write files.\n</tools>',
    },
    toolsAdded: [{ name: 'read', description: 'Read a file', parameters: { type: 'object' } }],
  },
  { role: 'user', content: 'Hello', timestamp: now + 1 },
  {
    role: 'assistant', content: [{ type: 'text', text: 'Hi' }], api: 'test', provider: 'test', model: 'test',
    usage: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, totalTokens: 0 }, stopReason: 'stop', timestamp: now + 2,
  },
  { role: 'toolResult', toolCallId: 'one', toolName: 'read', content: [{ type: 'text', text: 'file contents' }], isError: false, timestamp: now + 3 },
  { role: 'user', content: '<skill name="search" location="/skills/search/SKILL.md">\nSkill instructions\n</skill>', timestamp: now + 4 },
];
const active = { head: undefined, entries: [], contributions: [], messages };
const total = contextTokens(active);
const breakdown = contextBreakdown(active);
assert.equal(Object.values(breakdown).reduce((sum, value) => sum + value, 0), total);
for (const key of ['systemPrompt', 'projectInstructions', 'skills', 'toolDefinitions', 'userMessages', 'assistantMessages', 'toolResults']) {
  assert.ok(breakdown[key] > 0, `${key} should be represented`);
}
console.log('context breakdown tests passed');
