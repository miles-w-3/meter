// Launch a separate fixture-backed workspace with four Mermaid examples.
// It never touches the user's regular workspace database.
import { randomUUID } from "node:crypto";
import { mkdtemp } from "node:fs/promises";
import http from "node:http";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { spawn, spawnSync } from "node:child_process";
import { setTimeout as sleep } from "node:timers/promises";

const root = resolve(import.meta.dirname, "..");
const data = await mkdtemp(join(tmpdir(), "meter-mermaid-preview-"));
const bin = join(root, ".bin/workspace");
const env = { ...process.env, PI_WORKSPACE_HOME: data, PI_WORKSPACE_SERVER: join(root, "server/main.ts"), PI_PROVIDER: "fixture", PI_TRANSPORT: "unix" };
const boot = spawnSync(bin, ["server", "start"], { cwd: root, env, encoding: "utf8" });
if (boot.status !== 0) throw new Error(boot.stderr || boot.stdout || "Could not start fixture backend");
const socketPath = join(data, "workspace.sock");
async function request(path, method = "GET", value) {
  const body = value === undefined ? undefined : JSON.stringify(value);
  return new Promise((resolve, reject) => {
    const req = http.request({ socketPath, path, method, headers: body === undefined ? {} : { "content-type": "application/json", "content-length": Buffer.byteLength(body) } }, (res) => {
      let data = "";
      res.setEncoding("utf8").on("data", (chunk) => data += chunk).on("end", () => {
        if (res.statusCode < 200 || res.statusCode >= 300) return reject(new Error(`${method} ${path}: ${res.statusCode} ${data}`));
        try { resolve(data ? JSON.parse(data) : undefined); } catch (err) { reject(err); }
      });
    });
    req.on("error", reject); req.setTimeout(10_000, () => req.destroy(new Error("Fixture request timed out")));
    req.end(body);
  });
}
try {
  const project = await request("/v1/projects", "POST", { cwd: root, name: "Mermaid preview" });
  const conversation = await request("/v1/conversations", "POST", { cwd: root, projectId: project.id });
  for (const [key, title] of [["tiny", "Small: two steps"], ["wide", "Wide: request lifecycle"], ["tall", "Tall: decision path"], ["square", "Balanced: conversation lifecycle"]]) {
    await request(`/v1/conversations/${conversation.id}/messages`, "POST", { content: `fixture:mermaid:${key}`, requestId: randomUUID(), mode: "prompt" });
    const deadline = Date.now() + 30_000;
    let found = false;
    while (Date.now() < deadline) {
      const page = await request(`/v1/conversations/${conversation.id}/entries`);
      if (page.entries.some((entry) => entry.text.includes(title))) { found = true; break; }
      await sleep(100);
    }
    if (!found) throw new Error(`Timed out waiting for ${key} example`);
  }
  const layout = await request("/v1/workspaces/default");
  const tab = randomUUID(), pane = randomUUID();
  await request("/v1/workspaces/default", "PUT", { ...layout, activeProject: project.id, activeTab: tab, tabs: [{ id: tab, name: "Diagrams", projectId: project.id, root: pane, nodes: [{ id: pane, conversationId: conversation.id }] }] });
  const gui = spawn(bin, ["gui", "--cwd", root], { cwd: root, env, detached: true, stdio: "ignore" });
  gui.unref();
  console.log(`Mermaid preview opened (GUI pid ${gui.pid}, conversation ${conversation.id}).`);
  console.log(`Isolated fixture data: ${data}`);
  console.log(`Stop its backend: PI_WORKSPACE_HOME='${data}' PI_TRANSPORT=unix '${bin}' server stop`);
} catch (err) {
  spawnSync(bin, ["server", "stop"], { cwd: root, env, stdio: "ignore" });
  throw err;
}
