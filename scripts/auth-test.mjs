import { strict as assert } from "node:assert";
import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { spawnSync } from "node:child_process";

const dir = mkdtempSync(join(tmpdir(), "meter-auth-test-"));
try {
  const run = (provider, extra = {}) => spawnSync(process.execPath, ["--input-type=module", "-e", `
    import { openHarness } from './server/durable.ts';
    const { models, model } = await openHarness(process.env.TEST_DB, process.env.TEST_PROVIDER);
    const auth = await models.getAuth(model.provider);
    console.log(JSON.stringify({ provider: model.provider, source: auth?.source, key: auth?.auth.apiKey }));
  `], {
    encoding: "utf8",
    env: { ...process.env, PI_CODING_AGENT_DIR: dir, PI_MODEL: "gpt-5.2", OPENAI_API_KEY: "", TEST_DB: join(dir, `${provider}.sqlite`), TEST_PROVIDER: provider, ...extra },
  });
  const missing = run("openai");
  assert.notEqual(missing.status, 0);
  assert.match(missing.stderr, /No openai credentials found/);
  const apiKey = run("openai", { OPENAI_API_KEY: "test-api-key" });
  assert.equal(apiKey.status, 0, apiKey.stderr);
  assert.equal(JSON.parse(apiKey.stdout.trim()).key, "test-api-key");
  writeFileSync(join(dir, "auth.json"), JSON.stringify({
    openai: { type: "oauth", access: "test-access", refresh: "test-refresh", expires: Date.now() + 3600000 },
    "openai-codex": { type: "oauth", access: "test-codex", refresh: "test-refresh", expires: Date.now() + 3600000, accountId: "test-account" },
  }), { mode: 0o600 });
  for (const [provider, key] of [["openai", "test-access"], ["openai-codex", "test-codex"]]) {
    const result = run(provider);
    assert.equal(result.status, 0, result.stderr);
    const auth = JSON.parse(result.stdout.trim());
    assert.equal(auth.provider, provider);
    assert.equal(auth.key, key);
  }
  console.log("Pi credentials, OpenAI API keys, and OAuth providers passed");
} finally {
  rmSync(dir, { recursive: true, force: true });
}
