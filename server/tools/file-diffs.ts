import { execFile } from "node:child_process";
import { readdir, lstat, open } from "node:fs/promises";
import { constants, type Stats } from "node:fs";
import { join, relative } from "node:path";

// Snapshots of the files a shell command may change, confined to cwd and
// bounded. Inside a Git work tree only dirty or untracked paths are read; the
// before side of a clean path is its index blob. Gitignored files are not
// tracked: they are mostly build output. Git is only ever read: optional index
// refreshes are disabled. Outside Git every file is read, as before.
const excluded = new Set([".git", "node_modules", "vendor", ".venv", "venv", "dist", "build", "target"]);
const maxFiles = 10_000, maxBytes = 20_000_000, maxFile = 128_000, maxDiffs = 30, maxDiffChars = 400_000;
const concurrency = 8, gitTimeout = 10_000, maxListing = 64_000_000;
// A file modified this recently may still change without a visible mtime
// change on coarse filesystems, so its stat is not trusted for reuse.
const racyMs = 3_000;

export type FileSnapshot = {
  files: Map<string, string>;
  skipped: Set<string>;
  incomplete: boolean;
  // Absence from `files` is not evidence that a path does not exist.
  truncated: boolean;
  git?: GitState;
  // Set on Git after-snapshots: the before side of every candidate path.
  baseline?: FileSnapshot;
};
export type ComputedDiff = { path: string; before: string; after: string };

type Read = { text: string; stat: string } | "missing" | "skipped";
type Budget = { bytes: number };
// `reads` holds the dirty paths (relative to root) and their contents at the
// time of the snapshot; `index` is the raw `git ls-files -s` listing under root.
type GitState = { top: string; prefix: string; index: string; reads: Map<string, Read> };

// Pass the before-snapshot as `base` when taking the after-snapshot. In a Git
// work tree the after-snapshot needs it to know which paths to compare;
// without it only paths dirty in both snapshots are compared.
export async function snapshot(root: string, base?: FileSnapshot): Promise<FileSnapshot> {
  if (base?.git) return gitAfter(root, base.git);
  if (base) return walkSnapshot(root);
  return await gitSnapshot(root) ?? walkSnapshot(root);
}

export function compare(before: FileSnapshot, after: FileSnapshot) {
  const diffs: ComputedDiff[] = [];
  // A Git before-snapshot alone only holds dirty paths, so the after-snapshot
  // must have been taken against it to supply the before side.
  const old = after.baseline ?? before;
  let incomplete = before.incomplete || after.incomplete || old.incomplete || (before.git !== undefined && !after.baseline), total = 0;
  // Missing files are ambiguous when either scan is incomplete; do not invent
  // additions or deletions in that case.
  for (const path of [...new Set([...old.files.keys(), ...after.files.keys()])].sort()) {
    const prior = old.files.get(path), next = after.files.get(path);
    if (old.skipped.has(path) || after.skipped.has(path)) continue;
    if ((prior === undefined || next === undefined) && (old.truncated || after.truncated)) continue;
    if (prior === next) continue;
    const size = (prior?.length ?? 0) + (next?.length ?? 0);
    if (diffs.length >= maxDiffs || total + size > maxDiffChars) { incomplete = true; continue; }
    diffs.push({ path, before: prior ?? "", after: next ?? "" });
    total += size;
  }
  return { diffs, incomplete };
}

// Without Git there is no record of the before contents, so every file must be
// read up front; a stat-only scan could detect but never show a change. This
// stays bounded as before, but reads files concurrently instead of serially.
async function walkSnapshot(root: string): Promise<FileSnapshot> {
  const reads = new Map<string, Read>(), paths: string[] = [];
  let incomplete = false, truncated = false, count = 0;
  async function walk(dir: string) {
    let entries;
    try { entries = await readdir(dir, { withFileTypes: true }); }
    catch { incomplete = true; truncated = true; return; }
    entries.sort((a, b) => a.name.localeCompare(b.name));
    for (const entry of entries) {
      if (++count > maxFiles) { incomplete = true; truncated = true; return; }
      const path = join(dir, entry.name);
      if (entry.isDirectory()) {
        if (!excluded.has(entry.name)) await walk(path);
      } else if (entry.isSymbolicLink()) reads.set(relative(root, path), "skipped");
      else if (entry.isFile()) paths.push(path);
    }
  }
  await walk(root);
  const budget = { bytes: 0 };
  await pool(paths, async path => { reads.set(relative(root, path), await readText(path, budget)); });
  return fromReads(reads, incomplete, truncated);
}

async function gitSnapshot(root: string): Promise<FileSnapshot | undefined> {
  let top: string, prefix: string, state: Awaited<ReturnType<typeof gitStatus>>;
  try {
    // Fails outside a work tree, inside .git, and when Git is not installed.
    [top, prefix] = (await git(root, ["rev-parse", "--show-toplevel", "--show-prefix"])).toString().split("\n");
    if (!top || prefix === undefined) return undefined;
    state = await gitStatus(root, prefix);
  } catch { return undefined; }
  const paths = [...state.dirty].sort(), reads = new Map<string, Read>(), budget = { bytes: 0 };
  await pool(paths, async (path, i) => { reads.set(path, i < maxFiles ? await readText(join(root, path), budget) : "skipped"); });
  // Clean paths are not read, so `files` alone is truncated by design.
  return { ...fromReads(reads, state.incomplete, true), git: { top, prefix, index: state.index, reads } };
}

// Candidates are paths dirty before or after, plus paths whose index entry
// changed (for example a commit or checkout made by the command).
async function gitAfter(root: string, state: GitState): Promise<FileSnapshot> {
  let now: Awaited<ReturnType<typeof gitStatus>>;
  try { now = await gitStatus(root, state.prefix); }
  catch { return { files: new Map(), skipped: new Set(), incomplete: true, truncated: true }; }
  const candidates = new Set([...state.reads.keys(), ...now.dirty]);
  const oldIndex = indexEntries(state.index);
  if (now.index !== state.index) {
    const newIndex = indexEntries(now.index);
    for (const path of new Set([...oldIndex.keys(), ...newIndex.keys()])) {
      const local = path.slice(state.prefix.length);
      if (oldIndex.get(path) !== newIndex.get(path) && path.startsWith(state.prefix) && included(local)) candidates.add(local);
    }
  }
  const before = new Map<string, Read>(), after = new Map<string, Read>(), budget = { bytes: 0 };
  let changed = 0;
  await pool([...candidates].sort(), async (path, i) => {
    const old = state.reads.get(path);
    // Past the diff cap the result is incomplete anyway; stop reading.
    if (old === "skipped" || i >= maxFiles || changed >= maxDiffs) { before.set(path, "skipped"); after.set(path, "skipped"); return; }
    const next = await readText(join(root, path), budget, old);
    const prior = old ?? (next === "skipped" ? "skipped" : await readBlob(state.top, state.prefix + path, oldIndex.get(state.prefix + path), budget));
    before.set(path, prior);
    after.set(path, next);
    if (prior !== "skipped" && next !== "skipped" && content(prior) !== content(next)) changed++;
  });
  return { ...fromReads(after, now.incomplete, false), baseline: fromReads(before, false, false) };
}

// Paths are relative to the top-level in porcelain and --full-name output
// regardless of cwd; `-- .` limits both to root.
async function gitStatus(root: string, prefix: string) {
  const [status, index] = await Promise.all([
    git(root, ["status", "--porcelain=v1", "-z", "--untracked-files=all", "--", "."]),
    git(root, ["ls-files", "-s", "-z", "--full-name", "--", "."]),
  ]);
  const fields = status.toString().split("\0"), dirty = new Set<string>();
  let incomplete = false;
  for (let i = 0; i < fields.length; i++) {
    const field = fields[i];
    if (field.length < 4) continue;
    const names = [field.slice(3)];
    // Renames and copies are followed by their source path.
    if (/[RC]/.test(field.slice(0, 2))) names.push(fields[++i] ?? "");
    for (const name of names) {
      const path = name.slice(prefix.length);
      if (!path || !name.startsWith(prefix) || !included(path)) continue;
      // A nested repository is listed as a directory; its files are not seen.
      if (path.endsWith("/")) incomplete = true;
      else dirty.add(path);
    }
  }
  return { dirty, incomplete, index: index.toString() };
}

// Maps a top-level-relative path to "<mode> <object> <stage>".
function indexEntries(listing: string) {
  const entries = new Map<string, string>();
  for (const line of listing.split("\0")) {
    const tab = line.indexOf("\t");
    if (tab < 0) continue;
    const path = line.slice(tab + 1);
    entries.set(path, entries.has(path) ? "conflict" : line.slice(0, tab));
  }
  return entries;
}

// --filters applies the work tree's smudge and end-of-line conversion, so the
// blob matches what a clean checkout of the path contains. Filters that run
// external programs (for example Git LFS) are bounded by the timeout and size.
async function readBlob(top: string, path: string, entry: string | undefined, budget: Budget): Promise<Read> {
  if (entry === undefined) return "missing";
  const [mode, object, stage] = entry.split(" ");
  // Symlinks, submodules and merge conflicts have no comparable text.
  if (!mode.startsWith("100") || !object || stage !== "0") return "skipped";
  try {
    const text = decode(await git(top, ["cat-file", "--filters", "--path=" + path, object], maxFile), budget);
    return text === undefined ? "skipped" : { text, stat: "" };
  } catch { return "skipped"; }
}

// Reads a regular, non-symlink UTF-8 file within the caps. A known read is
// reused without reading when the file's stat is unchanged.
async function readText(path: string, budget: Budget, known?: Read): Promise<Read> {
  try {
    const info = await lstat(path);
    if (!info.isFile()) return "skipped";
    const stat = statKey(info);
    if (typeof known === "object" && known.stat !== "" && known.stat === stat) return known;
    if (info.size > maxFile || budget.bytes + info.size > maxBytes) return "skipped";
    const handle = await open(path, constants.O_RDONLY | (constants.O_NOFOLLOW ?? 0));
    let data: Buffer;
    try {
      const opened = await handle.stat();
      if (!opened.isFile() || opened.size > maxFile) return "skipped";
      const buffer = Buffer.allocUnsafe(maxFile + 1);
      const { bytesRead } = await handle.read(buffer, 0, buffer.length, 0);
      data = buffer.subarray(0, bytesRead);
    } finally { await handle.close(); }
    const text = decode(data, budget);
    return text === undefined ? "skipped" : { text, stat: info.mtimeMs < Date.now() - racyMs ? stat : "" };
  } catch (error) {
    const code = (error as { code?: unknown }).code;
    return code === "ENOENT" || code === "ENOTDIR" ? "missing" : "skipped";
  }
}

const decoder = new TextDecoder("utf-8", { fatal: true });
function decode(data: Buffer, budget: Budget) {
  if (data.length > maxFile || budget.bytes + data.length > maxBytes || data.includes(0)) return undefined;
  try {
    const text = decoder.decode(data);
    budget.bytes += data.length;
    return text;
  } catch { return undefined; }
}

// Matches the walk, which does not descend into excluded directories.
const included = (path: string) => !path.split("/").slice(0, -1).some(part => excluded.has(part));
const statKey = (info: Stats) => `${info.ino}:${info.size}:${info.mtimeMs}:${info.ctimeMs}`;
const content = (read: Read) => typeof read === "object" ? read.text : "";

function fromReads(reads: Map<string, Read>, incomplete: boolean, truncated: boolean): FileSnapshot {
  const files = new Map<string, string>(), skipped = new Set<string>();
  for (const [path, read] of reads) {
    if (read === "skipped") { skipped.add(path); incomplete = true; }
    else if (read !== "missing") files.set(path, read.text);
  }
  return { files, skipped, incomplete, truncated };
}

async function pool<T>(items: readonly T[], run: (item: T, index: number) => Promise<void>) {
  let next = 0;
  const worker = async () => {
    while (next < items.length) { const index = next++; await run(items[index], index); }
  };
  await Promise.all(Array.from({ length: Math.min(concurrency, items.length) }, worker));
}

function git(cwd: string, args: string[], maxBuffer = maxListing) {
  return new Promise<Buffer>((resolve, reject) => {
    execFile("git", args, {
      cwd, encoding: "buffer", maxBuffer, timeout: gitTimeout, killSignal: "SIGKILL", windowsHide: true,
      env: { ...process.env, GIT_OPTIONAL_LOCKS: "0" },
    }, (error: Error | null, stdout: Buffer) => error ? reject(error) : resolve(stdout));
  });
}
