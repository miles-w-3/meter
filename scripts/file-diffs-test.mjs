import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdtemp, mkdir, writeFile, rm, symlink } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { snapshot, compare } from "../server/tools/file-diffs.ts";
import { Workspaces } from "../server/workspaces.ts";

const root = await mkdtemp(join(tmpdir(), "meter-diffs-"));
try {
  // Not a Git work tree (an empty .git is not a repository): every file is read.
  await mkdir(join(root, ".git"));
  await writeFile(join(root, "before.txt"), "before\n");
  await writeFile(join(root, "deleted.txt"), "delete\n");
  const a = await snapshot(root);
  await writeFile(join(root, "before.txt"), "after\n");
  await rm(join(root, "deleted.txt"));
  await writeFile(join(root, "new.txt"), "new\n");
  const changes = compare(a, await snapshot(root, a));
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
} finally { await rm(root, { recursive: true, force: true }); }

function hasGit() {
  try { execFileSync("git", ["--version"], { stdio: "ignore" }); return true; } catch { return false; }
}
const git = (cwd, ...args) => execFileSync("git", [
  "-c", "user.name=Meter", "-c", "user.email=meter@example.invalid", "-c", "commit.gpgsign=false", "-c", "core.autocrlf=false", ...args,
], { cwd, stdio: "pipe" });

if (!hasGit()) console.log("Skipping Git file snapshot tests: git is not available");
else {
  const repo = await mkdtemp(join(tmpdir(), "meter-diffs-git-"));
  try {
    // The conversation cwd is a subdirectory, so paths must be made relative to it.
    const cwd = join(repo, "sub");
    await mkdir(cwd);
    git(repo, "init", "-q");
    await writeFile(join(repo, ".gitignore"), "*.log\n");
    await writeFile(join(repo, "outside.txt"), "outside\n");
    const tracked = { "clean.txt": "clean\n", "dirty.txt": "dirty v0\n", "gone.txt": "gone\n", "reverted.txt": "base\n", "untouched.txt": "untouched v0\n", "committed.txt": "one\n" };
    for (const [name, text] of Object.entries(tracked)) await writeFile(join(cwd, name), text);
    git(repo, "add", "-A");
    git(repo, "commit", "-q", "-m", "fixture");
    await writeFile(join(cwd, "dirty.txt"), "dirty v1\n");
    await writeFile(join(cwd, "reverted.txt"), "local\n");
    await writeFile(join(cwd, "untouched.txt"), "untouched v1\n");
    await writeFile(join(cwd, "ignored.log"), "ignored\n");

    const before = await snapshot(cwd);
    assert.ok(before.git, "a Git work tree uses Git status");
    assert.deepEqual([...before.files.keys()].sort(), ["dirty.txt", "reverted.txt", "untouched.txt"], "only dirty files are read");
    // The command: edit a clean file, edit a dirty file again, add, delete,
    // revert, and touch ignored and out-of-cwd files.
    await writeFile(join(cwd, "clean.txt"), "clean changed\n");
    await writeFile(join(cwd, "dirty.txt"), "dirty v2\n");
    await writeFile(join(cwd, "new.txt"), "new\n");
    await rm(join(cwd, "gone.txt"));
    git(cwd, "checkout", "--", "reverted.txt");
    await writeFile(join(cwd, "ignored.log"), "ignored again\n");
    await writeFile(join(repo, "outside.txt"), "outside changed\n");
    const changes = compare(before, await snapshot(cwd, before));
    assert.deepEqual(changes.diffs, [
      { path: "clean.txt", before: "clean\n", after: "clean changed\n" },
      { path: "dirty.txt", before: "dirty v1\n", after: "dirty v2\n" },
      { path: "gone.txt", before: "gone\n", after: "" },
      { path: "new.txt", before: "", after: "new\n" },
      { path: "reverted.txt", before: "local\n", after: "base\n" },
    ]);
    assert.equal(changes.incomplete, false);

    // A command that commits leaves every path clean; the index change still
    // surfaces the edit, and already-dirty files that were only staged do not.
    const staged = await snapshot(cwd);
    await writeFile(join(cwd, "committed.txt"), "two\n");
    git(cwd, "add", "-A", ".");
    git(cwd, "commit", "-q", "-m", "command");
    const committed = compare(staged, await snapshot(cwd, staged));
    assert.deepEqual(committed.diffs, [{ path: "committed.txt", before: "one\n", after: "two\n" }]);
    assert.equal(committed.incomplete, false);
    // Without the before-snapshot as base, Git results are marked incomplete.
    assert.equal(compare(staged, await snapshot(cwd)).incomplete, true);
  } finally { await rm(repo, { recursive: true, force: true }); }
}
console.log("File snapshots, Git dirty-path snapshots, exclusions, and persisted diff settings passed");
