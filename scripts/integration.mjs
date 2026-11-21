import assert from "node:assert/strict";
import { randomUUID } from "node:crypto";
import { execFile, spawn } from "node:child_process";
import { once } from "node:events";
import { mkdir, mkdtemp, readFile, realpath, rm, stat, writeFile } from "node:fs/promises";
import http from "node:http";
import { createConnection } from "node:net";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { setTimeout as sleep } from "node:timers/promises";
import { promisify } from "node:util";
import { WebSocket } from "ws";

const exec = promisify(execFile);
const transport = process.env.PI_TRANSPORT ?? "unix";
const directory = await mkdtemp(join(tmpdir(), "pi-m1-"));
const socket = join(directory, "workspace.sock");
const project = await mkdtemp(join(tmpdir(), "pi-m1-project-"));
const children = new Set();
const sockets = new Set();
const credentials = process.argv.includes("--provider") ? "openai-smoke" : "fixture";
const env = { ...process.env, PI_WORKSPACE_HOME: directory, PI_PROVIDER: credentials, PI_TRANSPORT: transport, PI_CRASH_POINT: "" };
const pathOf = (id) => "/v1/conversations/" + id;

async function until(predicate, name, timeout = 15_000) {
  const end = Date.now() + timeout;
  while (Date.now() < end) {
    const value = await predicate();
    if (value) return value;
    await sleep(20);
  }
  throw new Error("Timed out: " + name);
}
async function options(authorized = true) {
  if (transport === "unix") return { socketPath: socket };
  const endpoint = JSON.parse(await readFile(join(directory, "endpoint.json"), "utf8"));
  return { hostname: "127.0.0.1", port: endpoint.port, headers: authorized ? { authorization: "Bearer " + endpoint.token } : {} };
}
async function request(path, method = "GET", body, { drop = false, authorized = true } = {}) {
  const settings = await options(authorized);
  const data = body === undefined ? undefined : JSON.stringify(body);
  const headers = { ...settings.headers };
  if (data !== undefined) Object.assign(headers, { "content-type": "application/json", "content-length": Buffer.byteLength(data) });
  return new Promise((resolve, reject) => {
    const req = http.request({ ...settings, path, method, headers, agent: false }, async (res) => {
      if (drop) {
        // The request crossed the admission boundary. Discard the body as a client that never saw the receipt.
        res.destroy();
        resolve(res.statusCode);
        return;
      }
      try {
        const chunks = [];
        for await (const chunk of res) chunks.push(chunk);
        const text = Buffer.concat(chunks).toString("utf8");
        resolve({ status: res.statusCode, value: text && JSON.parse(text) });
      } catch (error) { reject(error); }
    });
    req.on("error", reject);
    req.setTimeout(6000, () => req.destroy(new Error("HTTP timeout")));
    req.end(data);
  });
}
const input = (content, requestId, mode = "prompt") => ({ content, requestId, mode });
const submit = (id, body, options) => request(pathOf(id) + "/messages", "POST", body, options);
const abort = (id, target) => request(pathOf(id) + "/abort", "POST", { target });
async function create(cwd = project) {
  const result = await request("/v1/conversations", "POST", { cwd });
  assert.equal(result.status, 201, JSON.stringify(result.value));
  return result.value.id;
}

function start(point = "") {
  const child = spawn(process.execPath, ["server/main.ts"], { env: { ...env, PI_CRASH_POINT: point }, stdio: ["ignore", "pipe", "pipe"] });
  children.add(child);
  let logs = "";
  for (const pipe of [child.stdout, child.stderr]) pipe.setEncoding("utf8").on("data", (text) => { logs += text; });
  const exited = once(child, "exit").then(([code, signal]) => {
    children.delete(child);
    return { code, signal, logs };
  });
  const ready = until(() => logs.includes("Workspace ready") || (child.exitCode !== null && { died: true }), "server started")
    .then((result) => {
      if (result.died) throw new Error(logs);
    });
  return { child, ready, exited };
}

async function watch(id) {
  const config = await options();
  const ws = transport === "unix"
    ? new WebSocket("ws://workspace" + pathOf(id) + "/stream", { createConnection: () => createConnection(socket) })
    : new WebSocket("ws://127.0.0.1:" + config.port + pathOf(id) + "/stream", { headers: config.headers });
  sockets.add(ws);
  const frames = [];
  ws.on("message", (data) => frames.push(JSON.parse(String(data))));
  ws.on("close", () => sockets.delete(ws));
  await once(ws, "open");
  await until(() => frames[0], "initial snapshot");
  assert.equal(frames[0].type, "snapshot");
  assert.equal(frames[0].revision, 0);
  return { ws, frames };
}
const entries = (frame) => frame.view.entries;
const text = (frame) => entries(frame).map((entry) => entry.text).join("\n");
async function waitFor(watcher, predicate, name, timeout) {
  return until(() => watcher.frames.findLast(predicate), name, timeout);
}
const stop = async (server) => {
  assert.equal((await request("/v1/server/stop", "POST")).status, 202);
  assert.equal((await server.exited).code, 0);
};
async function kill(server) {
  server.child.kill("SIGKILL");
  assert.equal((await server.exited).signal, "SIGKILL");
}
async function recovered(id, previous, submitted, prompt, expectedText) {
  const viewer = await watch(id);
  const frame = await waitFor(viewer, (f) => f.view.status === "idle" && text(f).includes(expectedText), "recovered transcript", 35_000);
  assert.notEqual(frame.generation, previous);
  assert.equal(entries(frame).filter((entry) => entry.text === prompt.content).length, 1);
  const replay = await submit(id, prompt);
  if (submitted !== undefined) assert.equal(replay.value.submissionId, submitted);
  viewer.ws.close();
  return frame;
}

async function regular() {
  const go = (args) => exec(".bin/workspace", args, { env, timeout: 13_000 });
  // Real local CLI races to start a detached daemon before either connects.
  const [first, second] = await Promise.all([go(["server", "start"]), go(["server", "start"])]).catch(async (error) => {
    const logs = await readFile(join(directory, "server.log"), "utf8").catch(() => "");
    throw new Error(error.message + "\n" + logs);
  });
  assert.equal(first.stdout, second.stdout);
  assert.match((await go(["server", "logs"])).stdout, /Workspace ready/);
  const id = (await request("/v1/server")).value.id;
  await go(["server", "stop"]);
  await until(async () => {
    try { await request("/v1/server"); return false; }
    catch { return true; }
  }, "detached daemon stops");
  let server = start();
  await server.ready;
  assert.equal((await request("/v1/server")).value.id, id);
  if (transport === "tcp") {
    assert.equal((await request("/v1/server", "GET", undefined, { authorized: false })).status, 401);
    assert.equal((await stat(join(directory, "endpoint.json"))).mode & 0o777, 0o600);
  } else {
    assert.equal((await stat(socket)).mode & 0o777, 0o600);
  }
  assert.equal((await request("/v1/conversations", "POST", { cwd: "relative" })).value.code, "invalid_cwd");
  const conversation = await create();
  assert.equal((await request(pathOf(conversation))).value.cwd, await realpath(project));
  assert.equal((await submit(conversation, input("test", "idle-steer", "steer"))).value.code, "idle");
  const firstWatch = await watch(conversation);
  const secondWatch = await watch(conversation);

  const prompt = input("fixture:tools-slow", "lost-response");
  assert.equal(await submit(conversation, prompt, { drop: true }), 202);
  const replay = await submit(conversation, prompt);
  const active = await waitFor(firstWatch, (f) => f.view.partial?.includes("streamed model output"), "first visible model update");
  assert.equal(active.type, "update");
  firstWatch.ws.close();
  const finished = await waitFor(secondWatch, (f) => f.view.status === "idle" && text(f).includes("Fixture completed"), "second viewer finishes");
  const write = entries(finished).find((entry) => entry.toolName === "write" && entry.toolSucceeded === true);
  assert.ok(write);
  assert.equal(write.toolTarget, "result.txt");
  assert.ok(Number.isInteger(write.durationMs));
  const stored = await request(pathOf(conversation) + "/entries/" + write.id + "/output");
  assert.equal(stored.status, 200);
  assert.match(stored.value.text, /Successfully wrote to result\.txt/);
  assert.ok(finished.view.contextUsage.tokens > 0);
  assert.ok(finished.view.contextUsage.window > finished.view.contextUsage.tokens);
  assert.equal(await readFile(join(project, "result.txt"), "utf8"), "written by Durable\n");
  await kill(server);

  server = start();
  await server.ready;
  await recovered(conversation, active.generation, replay.value.submissionId, prompt, "Fixture completed");
  assert.equal((await submit(conversation, { ...prompt, content: "different" })).value.code, "request_conflict");

  const control = await create();
  const controlWatch = await watch(control);
  assert.equal((await submit(control, input("fixture:slow", "first-run"))).status, 202);
  const busy = await waitFor(controlWatch, (f) => f.view.partial && f.view.abortTarget, "first run target");
  assert.equal((await submit(control, input("busy reject", "busy-reject"))).value.code, "busy");
  assert.equal((await request(pathOf(control) + "/cwd", "PUT", { cwd: project })).value.code, "busy");
  assert.equal((await submit(control, input("steer this", "steer", "steer"))).status, 202);
  assert.equal((await submit(control, input("fixture:slow successor", "queued-run", "followup"))).status, 202);
  const successor = await waitFor(controlWatch, (f) => f.view.abortTarget && f.view.abortTarget !== busy.view.abortTarget &&
    text(f).includes("fixture:slow successor"), "follow-up starts");
  assert.equal((await abort(control, busy.view.abortTarget)).value.code, "stale_run");
  assert.equal((await abort(control, successor.view.abortTarget)).status, 202);
  await waitFor(controlWatch, (f) => f.view.failure === "aborted", "targeted run aborted");

  const long = await create();
  const longWatch = await watch(long);
  for (let i = 0; i < 15; i++) {
    const phrase = "history sample " + i;
    assert.equal((await submit(long, input(phrase, "history-" + i))).status, 202);
    await waitFor(longWatch, (f) => f.view.status === "idle" && entries(f).some((e) => e.text === phrase), "history turn");
  }
  const live = longWatch.frames.at(-1).view;
  assert.ok(live.before);
  const page = await request(pathOf(long) + "/entries?before=" + live.before);
  const keys = [...page.value.entries, ...live.entries].map((entry) => entry.id);
  assert.equal(new Set(keys).size, keys.length);
  assert.ok(page.value.entries.some((entry) => entry.text === "history sample 0"));
  await stop(server);
  console.log("Local startup race, concurrent attach, lost receipt replay, stale abort, and transcript pagination passed");
}

async function workspaceChecks() {
  let server = start();
  await server.ready;
  const id = await create();
  const other = await create();
  const projectRecord = await request("/v1/projects", "POST", { cwd: await realpath(project), name: "Scratch project" });
  assert.equal(projectRecord.status, 201);
  const duplicate = await request("/v1/projects", "POST", { cwd: join(project, "."), name: "Accidental rename" });
  assert.equal(duplicate.status, 409);
  assert.equal((await request("/v1/catalog")).value.projects[0].name, "Scratch project");
  const renamed = await request("/v1/projects/" + projectRecord.value.id, "PUT", { name: "Renamed project" });
  assert.equal(renamed.status, 200);
  assert.equal(renamed.value.id, projectRecord.value.id);
  const catalog = await request("/v1/catalog");
  assert.equal(catalog.value.projects[0].name, "Renamed project");
  assert.ok(catalog.value.conversations.some((item) => item.id === id));
  assert.ok(catalog.value.conversations.some((item) => item.id === other));

  const movedRoot = join(project, "subdir");
  await mkdir(movedRoot);
  await writeFile(join(movedRoot, "notes.txt"), "From the pane's directory\n");
  const resolvePath = await request("/v1/directories/resolve", "POST", { cwd: join(movedRoot, "..", "subdir") });
  assert.deepEqual(resolvePath, { status: 200, value: { cwd: await realpath(movedRoot) } });
  const created = await request("/v1/conversations", "POST", { cwd: project, projectId: projectRecord.value.id });
  const movedID = created.value.id;
  const move = await request(pathOf(movedID) + "/cwd", "PUT", { cwd: movedRoot });
  assert.equal(move.status, 200);
  assert.equal(move.value.projectId, projectRecord.value.id);
  assert.equal(move.value.cwd, await realpath(movedRoot));
  assert.equal((await request(pathOf(id))).value.cwd, await realpath(project));
  assert.equal((await request("/v1/catalog")).value.projects[0].cwd, await realpath(project));
  const movedView = await watch(movedID);
  assert.equal((await submit(movedID, input("Review the pane directory\nfixture:tools", "changed-cwd"))).status, 202);
  await waitFor(movedView, (f) => f.view.status === "idle" && text(f).includes("Fixture completed"), "run in new pane directory");
  assert.equal(await readFile(join(movedRoot, "result.txt"), "utf8"), "written by Durable\n");
  const movedMetadata = (await request(pathOf(movedID))).value;
  assert.equal(movedMetadata.title, "Review the pane directory");
  assert.equal((await request(pathOf(movedID) + "/title", "PUT", { name: "Renamed conversation" })).value.title, "Renamed conversation");
  assert.ok(movedMetadata.lastActivity > 0);
  movedView.ws.close();

  const path = "/v1/workspaces/integration";
  assert.deepEqual((await request(path)).value, { revision: 0, tabs: [] });
  const leaf1 = { id: randomUUID(), conversationId: id, widthShare: .6, name: "Session one" };
  const leaf2 = { id: randomUUID(), conversationId: other, widthShare: .4 };
  const branch = { id: randomUUID(), axis: "horizontal", ratio: .47, first: leaf1.id, second: leaf2.id };
  const projectId = projectRecord.value.id;
  const tab = { id: randomUUID(), name: "Reliability", projectId, root: branch.id, nodes: [branch, leaf1, leaf2] };
  const empty = { id: randomUUID(), empty: true, cwd: await realpath(movedRoot) };
  const scratch = { id: randomUUID(), name: "Scratch", projectId, root: empty.id, nodes: [empty] };
  const body = { revision: 0, activeTab: tab.id, activeProject: projectId, tabs: [tab, scratch] };

  const duplicateTab = structuredClone(body);
  duplicateTab.tabs[1].name = "reliability";
  assert.equal((await request(path, "PUT", duplicateTab)).value.code, "invalid_layout");
  const invalid = structuredClone(body);
  invalid.tabs[0].nodes[0].first = branch.id;
  assert.equal((await request(path, "PUT", invalid)).value.code, "invalid_layout");
  const missing = structuredClone(body);
  missing.tabs[0].nodes[2].conversationId = "900000000";
  assert.equal((await request(path, "PUT", missing)).value.code, "not_found");
  assert.equal((await request(path)).value.revision, 0);
  const attempts = await Promise.all([request(path, "PUT", body), request(path, "PUT", body)]);
  assert.deepEqual(attempts.map((res) => res.status).sort(), [200, 409]);
  assert.equal(attempts.find((res) => res.status === 409).value.code, "layout_conflict");
  const saved = attempts.find((res) => res.status === 200).value;
  assert.equal(saved.revision, 1);
  await stop(server);

  server = start();
  await server.ready;
  assert.deepEqual((await request(path)).value, saved);
  assert.equal((await request(pathOf(movedID))).value.title, "Renamed conversation");
  assert.equal((await request(pathOf(movedID))).value.cwd, await realpath(movedRoot));
  assert.equal((await request(pathOf(movedID))).value.projectId, projectId);
  assert.equal((await request("/v1/catalog")).value.projects[0].id, renamed.value.id);
  assert.equal((await request(path, "PUT", { revision: 1, tabs: [] })).status, 200);
  assert.equal((await request(pathOf(id))).status, 200);
  assert.equal((await request(pathOf(other))).status, 200);
  await stop(server);
  console.log("Project catalog, nested layout validation, revision race, and restore after restart passed");
}

async function recovery(point) {
  let server = start(point);
  await server.ready;
  const id = await create();
  const viewer = await watch(id);
  const prompt = input(point === "model" ? "fixture:slow" : "fixture:tools", "crash-" + point);
  const accepted = await submit(id, prompt);
  if (point === "model") {
    await waitFor(viewer, (f) => f.view.partial, "model output before crash");
    await kill(server);
  } else {
    assert.equal((await server.exited).signal, "SIGKILL");
    if (point === "write") {
      assert.equal(await readFile(join(project, "result.txt"), "utf8"), "written by Durable\n");
      await writeFile(join(project, "result.txt"), "sentinel after interrupted write\n");
    }
  }
  server = start();
  await server.ready;
  const frame = await recovered(id, viewer.frames[0].generation, accepted.value.submissionId, prompt,
    point === "model" ? "streamed model output ".repeat(45) : "Fixture completed");
  if (point !== "model") assert.match(text(frame), new RegExp("Tool " + point + " was interrupted"));
  if (point === "write") assert.equal(await readFile(join(project, "result.txt"), "utf8"), "sentinel after interrupted write\n");
  await stop(server);
  console.log("Recovered after SIGKILL during " + point);
}

try {
  await writeFile(join(project, "notes.txt"), "Milestone 1 scratch file\n");
  if (credentials === "openai-smoke") {
    const server = start();
    await server.ready;
    const id = await create();
    const viewer = await watch(id);
    assert.equal((await submit(id, input("Read notes.txt, copy its exact contents to result.txt, then report completion.", "live-1"))).status, 202);
    const finished = await waitFor(viewer, (f) => f.view.status === "failed" ||
      (f.view.status === "idle" && entries(f).some((entry) => entry.toolName === "write")), "live provider", 90_000);
    assert.equal(finished.view.status, "idle", JSON.stringify(finished.view.failure));
    assert.equal(await readFile(join(project, "result.txt"), "utf8"), "Milestone 1 scratch file\n");
    await stop(server);
    console.log("Real provider scratch read/write passed");
  } else {
    await regular();
    await workspaceChecks();
    for (const point of ["model", "read", "write"]) await recovery(point);
  }
} finally {
  for (const ws of sockets) ws.terminate();
  await request("/v1/server/stop", "POST").catch(() => {});
  await Promise.all([...children].map(async (child) => {
    const exited = once(child, "exit");
    child.kill("SIGKILL");
    await exited;
  }));
  await rm(directory, { recursive: true, force: true });
  await rm(project, { recursive: true, force: true });
}
