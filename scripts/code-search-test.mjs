import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { mkdtemp, mkdir, writeFile, rm, chmod } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { BACKGROUND_CONTEXT, withCancel } from '@earendil-works/chord/context';
import { NodeExecutionEnv } from '@earendil-works/pi-durable/env/node';
import { searchCode, codeSearchTools } from '../server/tools/code-search.ts';
import { openHarness } from '../server/durable.ts';

const executable = process.env.RG_TEST_PATH || process.env.METER_RG_PATH || 'rg';
if (spawnSync(executable, ['--version']).status !== 0) {
  if (process.env.RG_TEST_PATH || process.env.METER_RG_PATH) throw new Error(`Cannot run test ripgrep: ${executable}`);
  console.log('code search tests skipped: install rg or set RG_TEST_PATH');
  process.exit(0);
}
const root = await mkdtemp(join(tmpdir(), 'meter-code-search-'));
const saved = process.env.METER_RG_PATH;
if (process.env.RG_TEST_PATH) process.env.METER_RG_PATH = process.env.RG_TEST_PATH;
const env = new NodeExecutionEnv({ cwd: root });
try {
  await mkdir(join(root, '.git'));
  await mkdir(join(root, 'sub'));
  await writeFile(join(root, '.gitignore'), 'ignored.ts\n');
  await writeFile(join(root, 'a file.ts'), 'first\nNeedle here\nlast\n');
  await writeFile(join(root, 'sub', 'other.ts'), 'needle two\n');
  await writeFile(join(root, '.hidden.ts'), 'needle hidden\n');
  await writeFile(join(root, 'ignored.ts'), 'needle ignored\n');
  await writeFile(join(root, 'sub', 'a.txt'), 'needle text\n');
  assert.deepEqual(codeSearchTools().map(tool => tool.name), ['grep', 'find']);
  const grep = args => searchCode('grep', args, env, BACKGROUND_CONTEXT);
  const find = args => searchCode('find', args, env, BACKGROUND_CONTEXT);
  const all = await grep({ pattern: 'needle', ignoreCase: true });
  assert.match(all, /a file\.ts:2: Needle here/);
  assert.match(all, /\.hidden\.ts:1: needle hidden/);
  assert.doesNotMatch(all, /ignored\.ts/);
  assert.match(await grep({ pattern: 'Needle', context: 1 }), /a file\.ts-1- first/);
  assert.equal(await grep({ pattern: 'needle', path: 'sub', glob: '*.txt' }), 'sub/a.txt:1: needle text');
  assert.equal(await grep({ pattern: 'nothing-like-this' }), 'No matches found');
  assert.equal(await grep({ pattern: '[', literal: true }), 'No matches found');
  await assert.rejects(grep({ pattern: '[' }), /regex|parse/i);
  await assert.rejects(grep({ pattern: 'x', path: 'missing' }), /No such file|not found|cannot find/i);
  const names = await find({ pattern: '*.ts' });
  assert.match(names, /a file\.ts/); assert.match(names, /\.hidden\.ts/);
  assert.doesNotMatch(names, /ignored\.ts|a\.txt/);
  assert.equal(await find({ pattern: 'sub/**/*.ts' }), 'sub/other.ts');
  assert.doesNotMatch(await grep({ pattern: 'needle', ignoreCase: true, glob: '*.ts' }), /ignored\.ts|a\.txt/);
  assert.match(await grep({ pattern: 'needle', ignoreCase: true, limit: 1 }), /Search truncated/);
  assert.match(await find({ pattern: '*.ts', limit: 1 }), /Search truncated/);
  const cancelled = withCancel(BACKGROUND_CONTEXT); cancelled.cancel();
  await assert.rejects(searchCode('grep', { pattern: 'x' }, env, cancelled.context), /aborted/i);
  await writeFile(join(root, 'bulk.txt'), ('long-match ' + 'x'.repeat(490) + '\n').repeat(200));
  const bulk = await grep({ pattern: 'long-match', path: 'bulk.txt', limit: 1000 });
  assert.match(bulk, /Search truncated/);
  assert.ok(Buffer.byteLength(bulk) <= 50 * 1024 + 1024);
  await writeFile(join(root, 'huge.txt'), 'X'.repeat(2 * 1024 * 1024) + 'needle\n');
  assert.match(await grep({ pattern: 'needle', path: 'huge.txt' }), /Search truncated/);
  const slowRG = join(root, 'slow-rg');
  await writeFile(slowRG, '#!/bin/sh\nexec /bin/sleep 30\n');
  await chmod(slowRG, 0o755);
  const actualRG = process.env.METER_RG_PATH;
  process.env.METER_RG_PATH = slowRG;
  const duringSearch = withCancel(BACKGROUND_CONTEXT);
  const pendingSearch = searchCode('grep', { pattern: 'x' }, env, duringSearch.context);
  setTimeout(() => duringSearch.cancel(), 100);
  await assert.rejects(pendingSearch, /aborted/i);
  if (actualRG === undefined) delete process.env.METER_RG_PATH; else process.env.METER_RG_PATH = actualRG;
  const { harness } = await openHarness(join(root, 'fixture.sqlite'), 'fixture');
  try {
    const conversation = await harness.createConversation({ ownership: { kind: 'ownerless' }, agent: { cwd: root, model: { provider: 'faux', modelId: 'faux-1' } } }, BACKGROUND_CONTEXT);
    await conversation.submit({ type: 'input', content: 'fixture:code-search', requestId: 'code-search' }, BACKGROUND_CONTEXT);
    await conversation.waitForIdle(BACKGROUND_CONTEXT);
    const entries = (await conversation.entries({ order: 'ascending' }, 100, undefined, BACKGROUND_CONTEXT)).items;
    const toolResults = entries.flatMap(entry => entry.model ?? []).filter(message => message.role === 'toolResult');
    const grepResult = toolResults.find(message => message.toolName === 'grep');
    const findResult = toolResults.find(message => message.toolName === 'find');
    assert.equal(grepResult?.isError, false);
    assert.equal(findResult?.isError, false);
    assert.match(JSON.stringify(grepResult.content), /Needle here/);
    assert.match(JSON.stringify(findResult.content), /a file\.ts/);
  } finally { await harness.close(BACKGROUND_CONTEXT); }
  process.env.METER_RG_PATH = join(root, 'missing-rg');
  await assert.rejects(grep({ pattern: 'x' }), /Unable to start ripgrep/);
  console.log('code search tests passed');
} finally {
  if (saved === undefined) delete process.env.METER_RG_PATH; else process.env.METER_RG_PATH = saved;
  await env.cleanup(BACKGROUND_CONTEXT);
  await rm(root, { recursive: true, force: true });
}
