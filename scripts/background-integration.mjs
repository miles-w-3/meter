// Real daemon + fixture model + shell processes; isolated from user data.
import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { mkdtemp, rm } from 'node:fs/promises';
import http from 'node:http';
import { createConnection } from 'node:net';
import { WebSocket } from 'ws';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { spawnSync } from 'node:child_process';
import { setTimeout as sleep } from 'node:timers/promises';

const root = resolve(import.meta.dirname, '..');
const home = await mkdtemp(join(tmpdir(), 'meter-background-integration-'));
const bin = join(root, '.bin/workspace');
const env = { ...process.env, METER_HOME: home, PI_WORKSPACE_SERVER: join(root, 'server/main.ts'), PI_PROVIDER: 'fixture', PI_TRANSPORT: 'unix' };
const boot = spawnSync(bin, ['server', 'start'], { cwd: root, env, encoding: 'utf8' });
if (boot.status !== 0) throw new Error(boot.stderr || boot.stdout);
const socketPath = join(home, 'workspace.sock');
function request(path, method = 'GET', value) {
  const body = value === undefined ? undefined : JSON.stringify(value);
  return new Promise((resolve, reject) => {
    const req = http.request({ socketPath, path, method, headers: body === undefined ? {} : { 'content-type': 'application/json', 'content-length': Buffer.byteLength(body) } }, res => {
      let text = ''; res.setEncoding('utf8').on('data', chunk => text += chunk).on('end', () => {
        if (res.statusCode < 200 || res.statusCode >= 300) return reject(new Error(`${method} ${path}: ${res.statusCode} ${text}`));
        resolve(text ? JSON.parse(text) : undefined);
      });
    });
    req.on('error', reject); req.setTimeout(10000, () => req.destroy(new Error('Request timed out'))); req.end(body);
  });
}
async function until(fn, predicate, description) {
  const deadline = Date.now() + 15000;
  while (Date.now() < deadline) { const value = await fn(); if (predicate(value)) return value; await sleep(100); }
  throw new Error(`Timed out waiting for ${description}`);
}
try {
  const project = await request('/v1/projects', 'POST', { cwd: root, name: 'Background integration' });
  const conversation = await request('/v1/conversations', 'POST', { cwd: root, projectId: project.id });
  const other = await request('/v1/conversations', 'POST', { cwd: root, projectId: project.id });
  const base = `/v1/conversations/${conversation.id}`;
  await request(base + '/messages', 'POST', { content: 'fixture:background-test', requestId: randomUUID(), mode: 'prompt' });
  const state = await until(() => request(base + '/background'), x => x.jobs.length === 2, 'two agent-started jobs');
  const monitor = state.jobs.find(x => x.mode === 'monitor');
  assert.ok(monitor); assert.equal((await request(`/v1/conversations/${other.id}/background`)).jobs.length, 0);
  await assert.rejects(request(`/v1/conversations/${other.id}/background/${monitor.id}/stop`, 'POST'), /404/);
  await until(() => request(base + '/entries'), x => x.entries.some(e => e.kind === 'meter.background' && e.text.includes('[Background output: CI status') && e.text.includes('CI queued')), 'monitor event in conversation');
  await until(() => request(base + '/entries'), x => x.entries.some(e => e.kind === 'meter.background' && e.text.includes('[Background exit: Unit tests') && e.text.includes('Unit tests passed')), 'completion notice in conversation');
  await request(`${base}/background/${monitor.id}/stop`, 'POST');
  await until(() => request(base + '/entries'), x => x.entries.some(e => e.text.includes('[Background exit: CI status') && e.text.includes('Status: stopped')), 'stop notice in conversation');
  // Hold the model in a foreground tool while a monitor event queues. Verify
  // source metadata and that user-queue controls cannot mutate the event.
  const busy = await request('/v1/conversations', 'POST', { cwd: root, projectId: project.id });
  const busyBase = `/v1/conversations/${busy.id}`;
  let latest;
  const ws = new WebSocket(`ws://workspace${busyBase}/stream`, { createConnection: () => createConnection(socketPath) });
  await new Promise((resolve, reject) => { ws.once('open', resolve); ws.once('error', reject); });
  ws.on('message', data => { const frame = JSON.parse(data.toString()); latest = frame.view; });
  await request(busyBase + '/messages', 'POST', { content: 'fixture:background-queue', requestId: randomUUID(), mode: 'prompt' });
  await until(() => request(busyBase + '/background'), x => x.jobs.length === 1, 'busy monitor start');
  await request(busyBase + '/messages', 'POST', { content: 'a real user follow-up', requestId: randomUUID(), mode: 'followup' });
  await until(async () => latest, x => x?.queued?.some(q => q.background?.label === 'Queued CI'), 'queued monitor metadata');
  const event = latest.queued.find(q => q.background);
  const followup = latest.queued.find(q => !q.background);
  assert.equal(event.background.notice, 'output');
  assert.equal(event.background.mode, 'monitor');
  assert.ok(followup);
  await assert.rejects(request(`${busyBase}/queue/${event.id}/steer`, 'POST'), /409/);
  await assert.rejects(request(`${busyBase}/queue/${event.id}/recall`, 'POST'), /409/);
  await request(`${busyBase}/queue/${followup.id}/recall`, 'POST');
  await until(async () => latest, x => x?.queued?.some(q => q.id === event.id) && !x.queued.some(q => q.id === followup.id), 'monitor preserved after recalling user follow-up');
  ws.close();
  const remaining = await request(busyBase + '/background');
  await request(`${busyBase}/background/${remaining.jobs[0].id}/stop`, 'POST');
  console.log('Background integration passed: agent start, scoped list, event, exit, stop and protected queued metadata.');
} finally {
  spawnSync(bin, ['server', 'stop'], { cwd: root, env, stdio: 'ignore' });
  await sleep(200);
  await rm(home, { recursive: true, force: true });
}
