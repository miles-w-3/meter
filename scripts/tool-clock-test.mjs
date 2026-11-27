import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { once } from 'node:events';
import { mkdtemp, readFile, rm } from 'node:fs/promises';
import { join } from 'node:path';
import { tmpdir } from 'node:os';
import { setTimeout as sleep } from 'node:timers/promises';
import { WebSocket } from 'ws';

const root = await mkdtemp(join(tmpdir(), 'meter-tool-clock-'));
const server = spawn(process.execPath, ['server/main.ts'], {
  env: { ...process.env, METER_HOME: root, PI_TRANSPORT: 'tcp', PI_PROVIDER: 'fixture' },
  stdio: ['ignore', 'pipe', 'pipe'],
});
let log = '';
server.stdout.on('data', chunk => { log += chunk; });
server.stderr.on('data', chunk => { log += chunk; });
const sockets = [];
async function until(fn, label) {
  for (let i = 0; i < 200; i++) {
    const value = await fn();
    if (value) return value;
    await sleep(25);
  }
  throw new Error(`Timed out: ${label}\n${log}`);
}
try {
  const endpoint = await until(async () => {
    try { return JSON.parse(await readFile(join(root, 'endpoint.json'), 'utf8')); } catch { return undefined; }
  }, 'server startup');
  const base = `http://127.0.0.1:${endpoint.port}`;
  async function post(path, body) {
    const response = await fetch(base + path, { method: 'POST', headers: { authorization: `Bearer ${endpoint.token}`, 'content-type': 'application/json' }, body: JSON.stringify(body) });
    if (!response.ok) throw new Error(await response.text());
    return response.json();
  }
  const conversation = await post('/v1/conversations', { cwd: root });
  const path = `/v1/conversations/${conversation.id}`;
  async function watch() {
    const ws = new WebSocket(`ws://127.0.0.1:${endpoint.port}${path}/stream`, { headers: { authorization: `Bearer ${endpoint.token}` } });
    sockets.push(ws);
    const frames = [];
    ws.on('message', data => frames.push(JSON.parse(String(data))));
    await once(ws, 'open');
    await until(() => frames[0], 'watch snapshot');
    return frames;
  }
  const frames = await watch();
  await post(`${path}/messages`, { content: 'fixture:tool-clock', requestId: 'clock', mode: 'prompt' });
  const tool = await until(() => frames.flatMap(frame => frame.view.tools).find(item => item.status === 'running' && item.startedAt), 'tool start time');
  assert.ok(tool.startedAt <= Date.now() && tool.startedAt > Date.now() - 10_000);
  const reconnected = await watch();
  assert.equal(reconnected[0].view.tools.find(item => item.taskId === tool.taskId)?.startedAt, tool.startedAt);
  // Updates carry only appended entries, so look for the bash entry in any frame.
  await until(() => frames.at(-1)?.view.status === 'idle' && frames.some(frame => frame.view.entries.some(entry => entry.toolName === 'bash')), 'tool completion');
  console.log('tool start time and reconnect tests passed');
} finally {
  for (const ws of sockets) ws.terminate();
  if (server.exitCode === null) { server.kill('SIGTERM'); await once(server, 'exit'); }
  await rm(root, { recursive: true, force: true });
}
