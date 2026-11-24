import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { once } from "node:events";
import { mkdtemp, readFile, rm } from "node:fs/promises";
import http from "node:http";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { setTimeout as sleep } from "node:timers/promises";
import { WebSearch } from "../server/tools/web-search.ts";

const data = await mkdtemp(join(tmpdir(), "meter-search-"));
const endpointPath = join(data, "endpoint.json");
const effects = join(data, "search-attempts");
const settings = "/v1/settings/tools";
const query = "Meter search fixture";
const mock = `
  import assert from "node:assert/strict";
  import { appendFileSync } from "node:fs";
  globalThis.fetch = async (target, options) => {
    const url = new URL(target);
    const body = options.body && JSON.parse(options.body);
    appendFileSync(process.env.SEARCH_FIXTURE_EFFECTS, url.hostname + "\\n");
    if (url.hostname === "api.search.brave.com") {
      assert.equal(url.searchParams.get("q"), ${JSON.stringify(query)});
      assert.equal(options.headers["X-Subscription-Token"], "fixture-brave");
      if (process.env.SEARCH_FIXTURE_OUTAGE) {
        return new Response(null, { status: 429, headers: { "Retry-After": "60" } });
      }
      return Response.json({ type: "search", query: { original: ${JSON.stringify(query)} } });
    }
    assert.equal(body.query, ${JSON.stringify(query)});
    if (url.hostname === "api.exa.ai") {
      assert.equal(options.headers["x-api-key"], "fixture-exa");
      if (process.env.SEARCH_FIXTURE_OUTAGE === "both") return new Response(null, { status: 503 });
      return Response.json({ results: [{ title: "Exa result", url: "https://example.org/exa", highlights: ["Source excerpt"] }] });
    }
    assert.equal(url.hostname, "api.firecrawl.dev");
    assert.equal(options.headers.Authorization, "Bearer fixture-firecrawl");
    assert.equal(body.scrapeOptions, undefined);
    return Response.json({ success: true, data: { web: [{ title: "Firecrawl result", url: "https://example.org/firecrawl", description: "Source snippet" }] } });
  }
`;
const preload = "data:text/javascript;base64," + Buffer.from(mock).toString("base64");
let running;

async function start({ outage = true, storageRights = true } = {}) {
  let logs = "";
  const child = spawn(process.execPath, ["--import", preload, "server/main.ts"], {
    env: { ...process.env, METER_HOME: data, PI_PROVIDER: "fixture", PI_TRANSPORT: "tcp",
      BRAVE_SEARCH_API_KEY: "fixture-brave", BRAVE_SEARCH_STORAGE_RIGHTS: storageRights ? "1" : "",
      EXA_API_KEY: "fixture-exa", FIRECRAWL_API_KEY: "fixture-firecrawl", SEARCH_FIXTURE_EFFECTS: effects,
      SEARCH_FIXTURE_OUTAGE: outage === "both" ? outage : outage ? "1" : "" },
    stdio: ["ignore", "pipe", "pipe"],
  });
  for (const pipe of [child.stdout, child.stderr]) pipe.setEncoding("utf8").on("data", chunk => { logs += chunk; });
  running = { child, exit: once(child, "exit") };
  for (let i = 0; i < 200; i++) {
    if (logs.includes("Workspace ready")) return;
    if (child.exitCode !== null) break;
    await sleep(25);
  }
  throw new Error("Search fixture failed to start: " + logs);
}

async function request(path, method = "GET", value) {
  const { port, token } = JSON.parse(await readFile(endpointPath, "utf8"));
  return new Promise((resolve, reject) => {
    const req = http.request({
      hostname: "127.0.0.1", port, path, method,
      headers: { Authorization: "Bearer " + token, ...(value ? { "Content-Type": "application/json" } : {}) },
    }, async response => {
      try {
        const chunks = [];
        for await (const chunk of response) chunks.push(chunk);
        const body = Buffer.concat(chunks).toString();
        resolve({ status: response.statusCode, value: body ? JSON.parse(body) : undefined });
      } catch (error) { reject(error); }
    });
    req.on("error", reject);
    req.setTimeout(5000, () => req.destroy(new Error("Search fixture HTTP timeout")));
    req.end(value ? JSON.stringify(value) : undefined);
  });
}

async function stop() {
  assert.equal((await request("/v1/server/stop", "POST")).status, 202);
  assert.deepEqual(await running.exit, [0, null]);
  running = undefined;
}

async function toggle(webSearch) {
  const res = await request(settings, "PATCH", { webSearch });
  assert.equal(res.status, 200);
  return res.value.webSearch;
}

async function runSearch(name = "web-search") {
  const created = await request("/v1/conversations", "POST", { cwd: data });
  assert.equal(created.status, 201);
  const path = "/v1/conversations/" + created.value.id;
  assert.equal((await request(path + "/messages", "POST", {
    content: "fixture:" + name, mode: "prompt", requestId: "one-search",
  })).status, 202);
  for (let i = 0; i < 200; i++) {
    const page = await request(path + "/entries");
    const result = page.value.entries.find(entry => entry.toolName === name.replace("-", "_"));
    if (result) return result;
    await sleep(25);
  }
  throw new Error("Search result was not recorded");
}

const attempts = async () => (await readFile(effects, "utf8")).trim().split("\n");

try {
  await start();
  const initial = await request(settings);
  assert.equal(initial.status, 200);
  assert.equal(initial.value.webSearch.enabled, true);
  assert.equal(initial.value.webFetch.enabled, true);
  const fetch = await runSearch("web-fetch");
  assert.equal(fetch.toolSucceeded, false);
  assert.match(fetch.text, /blocked_destination/);
  assert.equal(fetch.toolTarget, "http://127.0.0.1/");
  assert.equal((await request(settings, "PATCH", { webFetch: { enabled: false } })).value.webFetch.enabled, false);
  assert.equal((await runSearch("web-fetch")).toolSucceeded, false);
  assert.equal((await request(settings, "PATCH", { webFetch: { enabled: true } })).value.webFetch.enabled, true);
  assert.ok(initial.value.webSearch.providers.every(p => p.status === "ready"));
  assert.ok(!JSON.stringify(initial.value).includes("fixture-brave"));
  assert.deepEqual(JSON.parse((await runSearch()).text), {
    provider: "exa", results: [{ title: "Exa result", url: "https://example.org/exa", snippet: "Source excerpt" }],
  });
  const cooling = (await request(settings)).value.webSearch.providers[0];
  assert.equal(cooling.status, "rate_limited");
  assert.ok(Date.parse(cooling.retryAt) > Date.now());
  assert.equal((await toggle({ provider: { id: "exa", enabled: false } })).providers[1].status, "disabled");
  assert.equal(JSON.parse((await runSearch()).text).provider, "firecrawl");
  assert.deepEqual(await attempts(), ["api.search.brave.com", "api.exa.ai", "api.firecrawl.dev"]);
  assert.equal((await toggle({ enabled: false })).enabled, false);
  assert.equal((await runSearch()).toolSucceeded, false);
  assert.equal((await attempts()).length, 3);
  await stop();

  await start({ outage: false });
  const saved = (await request(settings)).value.webSearch;
  assert.equal(saved.enabled, false);
  assert.equal(saved.providers[1].enabled, false);
  await toggle({ enabled: true });
  assert.deepEqual(JSON.parse((await runSearch()).text), { provider: "brave", results: [] });
  assert.equal((await attempts()).length, 4);
  await stop();

  await start({ storageRights: false });
  assert.equal((await request(settings)).value.webSearch.providers[0].status, "storage_rights_required");
  assert.equal(JSON.parse((await runSearch()).text).provider, "firecrawl");
  assert.equal((await attempts()).at(-1), "api.firecrawl.dev");
  assert.equal((await attempts()).length, 5);
  await stop();
  await start({ outage: "both" });
  await toggle({ provider: { id: "exa", enabled: true } });
  assert.equal(JSON.parse((await runSearch()).text).provider, "firecrawl");
  assert.deepEqual((await attempts()).slice(5), ["api.search.brave.com", "api.exa.ai", "api.firecrawl.dev"]);
  assert.equal((await request(settings)).value.webSearch.providers[1].status, "unavailable");
  await stop();

  const original = globalThis.fetch;
  const canceled = new AbortController();
  let launched = 0;
  globalThis.fetch = async (_target, { signal }) => {
    launched++;
    queueMicrotask(() => canceled.abort());
    await new Promise((_, reject) => {
      signal.addEventListener("abort", () => reject(signal.reason), { once: true });
    });
  };
  try {
    const search = new WebSearch(() => ({ webSearch: { enabled: true, providers: { brave: true, exa: true, firecrawl: true } } }), {
      EXA_API_KEY: "fixture-exa", FIRECRAWL_API_KEY: "fixture-firecrawl",
    });
    await assert.rejects(search.tool().execute({ query }, {}, { abortSignal: canceled.signal }), { name: "AbortError" });
    assert.equal(launched, 1);
  } finally {
    globalThis.fetch = original;
  }
  // Exa's public MCP and Firecrawl's search endpoint work without credentials.
  const savedFetch = globalThis.fetch;
  try {
    const prefs = (id) => () => ({ webSearch: { enabled: true, providers: { brave: false, exa: id === "exa", firecrawl: id === "firecrawl" } } });
    for (const id of ["exa", "firecrawl"]) {
      globalThis.fetch = async (url, options) => {
        if (id === "exa") {
          assert.equal(url, "https://mcp.exa.ai/mcp");
          assert.equal(options.headers["x-api-key"], undefined);
          assert.equal(JSON.parse(options.body).params.name, "web_search_exa");
          const data = { result: { content: [{ type: "text", text: "Title: Exa result\nURL: https://example.org/exa\nHighlights:\nSource excerpt" }] } };
          return new Response("event: message\ndata: " + JSON.stringify(data) + "\n\n", { headers: { "Content-Type": "text/event-stream" } });
        }
        assert.equal(url, "https://api.firecrawl.dev/v2/search");
        assert.equal(options.headers.Authorization, undefined);
        return Response.json({ success: true, data: { web: [{ title: "Firecrawl result", url: "https://example.org/firecrawl", description: "Source snippet" }] } });
      };
      const search = new WebSearch(prefs(id), {});
      assert.equal(search.status().webSearch.providers.find(p => p.id === id).status, "ready");
      assert.equal(search.available(), true);
      const result = await search.tool().execute({ query }, {}, {});
      const value = JSON.parse(result.output[0].text);
      assert.equal(value.provider, id);
      assert.equal(value.results[0].url, "https://example.org/" + id);
    }
  } finally { globalThis.fetch = savedFetch; }
  console.log("Search settings, fallback, keyless providers, cooldown, persistence, and cancellation passed");
} finally {
  if (running) {
    running.child.kill("SIGKILL");
    await running.exit;
  }
  await rm(data, { recursive: true, force: true });
}
