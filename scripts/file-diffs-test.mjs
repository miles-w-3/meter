import assert from "node:assert/strict";
import { mkdtemp, mkdir, writeFile, rm, symlink } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { snapshot, compare } from "../server/tools/file-diffs.ts";
import { Workspaces } from "../server/workspaces.ts";

const root = await mkdtemp(join(tmpdir(), "meter-diffs-"));
try {
  await mkdir(join(root, ".git"));
  await writeFile(join(root, "before.txt"), "before\n");
  await writeFile(join(root, "deleted.txt"), "delete\n");
  const a = await snapshot(root);
  await writeFile(join(root, "before.txt"), "after\n");
  await rm(join(root, "deleted.txt"));
  await writeFile(join(root, "new.txt"), "new\n");
  const changes = compare(a, await snapshot(root));
  assert.deepEqual(changes.diffs, [
    { path: "before.txt", before: "before\n", after: "after\n" },
    { path: "deleted.txt", before: "delete\n", after: "" },
    { path: "new.txt", before: "", after: "new\n" },
  ]);
  assert.equal(changes.incomplete, false);
  await writeFile(join(root, "before.txt"), Buffer.from([0, 1]));
  await symlink("new.txt", join(root, "link"));
  const partial = compare(a, await snapshot(root));
  assert.equal(partial.incomplete, true);
  assert.ok(!partial.diffs.some(d => d.path === "before.txt"), "binary transition is not a false deletion");

  const store = new Workspaces(join(root, "app.sqlite"));
  assert.equal(store.toolSettings().computedDiffs, true);
  store.updateToolSettings({ computedDiffs: false, diffLayout: "unified", diffInline: "characters" });
  assert.equal(store.toolSettings().computedDiffs, false);
  assert.equal(store.toolSettings().diffLayout, "unified");
  assert.equal(store.toolSettings().diffInline, "characters");
  store.db.close();
  const reopened = new Workspaces(join(root, "app.sqlite"));
  assert.equal(reopened.toolSettings().computedDiffs, false);
  assert.equal(reopened.toolSettings().diffLayout, "unified");
  reopened.db.close();
  console.log("File snapshots, exclusions, and persisted diff settings passed");
} finally { await rm(root, { recursive: true, force: true }); }
