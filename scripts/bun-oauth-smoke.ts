// Compiled by bun-oauth-test.mjs: exercise the same registration and model
// runtime as the release backend with disposable, non-network credentials.
import "../server/oauth-setup.ts";
import { BACKGROUND_CONTEXT } from "@earendil-works/chord/context";
import { openHarness } from "../server/durable.ts";

const db = process.env.TEST_DB;
if (!db) throw new Error("TEST_DB is required");
const opened = await openHarness(db, "openai-codex");
try {
  const result = await opened.models.getAuth("openai-codex");
  if (result?.auth.apiKey !== "test-codex-token") throw new Error("Codex OAuth did not resolve the fixture credential");
  console.log("Compiled Codex OAuth credential resolution passed");
} finally {
  await opened.harness.close(BACKGROUND_CONTEXT);
  await opened.closeClaude();
}
