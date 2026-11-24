import { mkdtempSync, mkdirSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { spawnSync } from "node:child_process";

const dir = mkdtempSync(join(tmpdir(), "meter-bun-oauth-"));
try {
  const agent = join(dir, "agent");
  mkdirSync(agent, { mode: 0o700 });
  writeFileSync(join(agent, "auth.json"), JSON.stringify({
    "openai-codex": { type: "oauth", access: "test-codex-token", refresh: "test-refresh", expires: Date.now() + 3600000, accountId: "test-account" },
  }), { mode: 0o600 });
  const binary = join(dir, "oauth-smoke");
  const build = spawnSync(process.env.BUN_EXE || "bun", ["build", "scripts/bun-oauth-smoke.ts", "--compile", "--minify-syntax", "--minify-whitespace", "--no-compile-autoload-dotenv", "--no-compile-autoload-bunfig", "--outfile", binary], { encoding: "utf8" });
  if (build.status !== 0) throw new Error(`Compile failed: ${build.stderr || build.stdout}`);
  const run = spawnSync(binary, [], { encoding: "utf8", timeout: 30000, env: {
    ...process.env, PI_CODING_AGENT_DIR: agent, PI_MODEL: "gpt-5.2", TEST_DB: join(dir, "test.sqlite"), OPENAI_API_KEY: "",
  } });
  if (run.status !== 0) throw new Error(`Compiled OAuth test failed: ${run.stderr || run.stdout}`);
  console.log(run.stdout.trim());
} finally {
  rmSync(dir, { recursive: true, force: true });
}
