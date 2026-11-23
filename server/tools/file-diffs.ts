import { readdir, lstat, open } from "node:fs/promises";
import { constants } from "node:fs";
import { join, relative } from "node:path";

// Intentionally bounded and confined to cwd. Symlinks and common generated
// trees are not traversed. Snapshots are per call, never a Git index mutation.
const excluded = new Set([".git", "node_modules", "vendor", ".venv", "venv", "dist", "build", "target"]);
const maxFiles = 10_000, maxBytes = 20_000_000, maxFile = 128_000;
export type FileSnapshot = { files: Map<string, string>; skipped: Set<string>; incomplete: boolean; truncated: boolean };
export type ComputedDiff = { path: string; before: string; after: string };

export async function snapshot(root: string): Promise<FileSnapshot> {
  const files = new Map<string, string>(), skipped = new Set<string>();
  let incomplete = false, truncated = false, count = 0, bytes = 0;
  const decoder = new TextDecoder("utf-8", { fatal: true });
  async function walk(dir: string) {
    let entries;
    try { entries = await readdir(dir, { withFileTypes: true }); }
    catch { incomplete = true; truncated = true; return; }
    entries.sort((a, b) => a.name.localeCompare(b.name));
    for (const entry of entries) {
      if (++count > maxFiles) { incomplete = true; truncated = true; return; }
      const path = join(dir, entry.name);
      if (entry.isDirectory()) {
        if (excluded.has(entry.name)) continue;
        await walk(path);
      } else if (entry.isSymbolicLink()) { incomplete = true; skipped.add(relative(root, path)); }
      else if (entry.isFile()) {
        try {
          const info = await lstat(path);
          if (!info.isFile() || info.size > maxFile || bytes + info.size > maxBytes) { incomplete = true; skipped.add(relative(root, path)); continue; }
          const handle = await open(path, constants.O_RDONLY | (constants.O_NOFOLLOW ?? 0));
          let data: Buffer;
          try {
            const opened = await handle.stat();
            if (!opened.isFile() || opened.size > maxFile) { incomplete = true; skipped.add(relative(root, path)); continue; }
            const buffer = Buffer.allocUnsafe(maxFile + 1);
            const { bytesRead } = await handle.read(buffer, 0, buffer.length, 0);
            data = buffer.subarray(0, bytesRead);
          } finally { await handle.close(); }
          if (data.length > maxFile || bytes + data.length > maxBytes || data.includes(0)) { incomplete = true; skipped.add(relative(root, path)); continue; }
          const text = decoder.decode(data);
          bytes += data.length;
          files.set(relative(root, path), text);
        } catch { incomplete = true; skipped.add(relative(root, path)); }
      }
    }
  }
  await walk(root);
  return { files, skipped, incomplete, truncated };
}

export function compare(before: FileSnapshot, after: FileSnapshot) {
  const diffs: ComputedDiff[] = [];
  let incomplete = before.incomplete || after.incomplete, total = 0;
  // Missing files are ambiguous when either scan is incomplete; do not invent
  // additions or deletions in that case.
  for (const path of new Set([...before.files.keys(), ...after.files.keys()])) {
    const old = before.files.get(path), next = after.files.get(path);
    if (before.skipped.has(path) || after.skipped.has(path)) continue;
    if ((old === undefined || next === undefined) && (before.truncated || after.truncated)) continue;
    if (old === next) continue;
    const size = (old?.length ?? 0) + (next?.length ?? 0);
    if (diffs.length >= 30 || total + size > 400_000) { incomplete = true; continue; }
    diffs.push({ path, before: old ?? "", after: next ?? "" });
    total += size;
  }
  return { diffs, incomplete };
}
