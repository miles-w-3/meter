import { withCancel, type Context } from "@earendil-works/chord/context";
import { defineTool } from "@earendil-works/pi-durable";
import type { ExecutionEnv } from "@earendil-works/pi-durable/env";
import { z } from "zod";
import { basename, relative, resolve, sep } from "node:path";
import { minimatch } from "minimatch";

const common = {
  path: z.string().min(1).describe("File or directory to search, relative to the workspace or absolute; defaults to the workspace").optional(),
  limit: z.number().int().min(1).max(1000).describe("Maximum matches or paths; defaults to 100").optional(),
};
const grepParameters = z.strictObject({
  pattern: z.string().min(1).describe("Regular expression or literal text to search for"), ...common,
  glob: z.string().min(1).describe("Filter paths with a glob; basename patterns match at any depth").optional(),
  ignoreCase: z.boolean().describe("Use case-insensitive matching").optional(),
  literal: z.boolean().describe("Treat pattern as literal text rather than a regex").optional(),
  context: z.number().int().min(0).max(20).describe("Lines of context before and after matches").optional(),
});
const findParameters = z.strictObject({ pattern: z.string().min(1).describe("File glob; basename patterns match at any depth, paths are relative to the search root"), ...common });
type GrepArgs = z.infer<typeof grepParameters>;
type FindArgs = z.infer<typeof findParameters>;
const jsonSchema = (schema: typeof grepParameters | typeof findParameters) => {
  const result = z.toJSONSchema(schema); delete result.$schema; return result;
};
const MAX_BYTES = 50 * 1024;
// A single JSON event can contain a very long source line. Bound the parser too.
const MAX_EVENT_BYTES = 1024 * 1024;

function rgText(value: unknown): string {
  if (!value || typeof value !== "object") return "";
  const field = value as { text?: unknown; bytes?: unknown };
  if (typeof field.text === "string") return field.text;
  return typeof field.bytes === "string" ? Buffer.from(field.bytes, "base64").toString("utf8") : "";
}

/** Execute through the workspace environment, never through a shell. */
export async function searchCode(kind: "grep" | "find", input: unknown, env: ExecutionEnv, context: Context): Promise<string> {
  const args: GrepArgs | FindArgs = kind === "grep" ? grepParameters.parse(input) : findParameters.parse(input);
  const limit = args.limit ?? 100;
  const command = [process.env.METER_RG_PATH || "rg", "--no-config", "--hidden", "--glob", "!.git"];
  if (kind === "grep") {
    const grep = args as GrepArgs;
    command.push("--json", "--line-number", "--color=never");
    if (grep.ignoreCase) command.push("--ignore-case");
    if (grep.literal) command.push("--fixed-strings");
    if (grep.context) command.push("--context", String(grep.context));
    command.push("--", grep.pattern, grep.path || ".");
  } else {
    command.push("--files", "--null", "--", args.path || ".");
  }
  // ripgrep's -g overrides .gitignore, so filter globs after traversal instead.
  // Patterns without a slash match basenames at any depth.
  const glob = kind === "find" ? args.pattern : (args as GrepArgs).glob;
  const searchRoot = resolve(env.cwd, args.path || ".");
  const matches = (path: string) => {
    if (!glob) return true;
    const pattern = glob.replace(/^\.\//, "");
    const rel = relative(searchRoot, resolve(env.cwd, path)).split(sep).join("/");
    return minimatch(pattern.includes("/") ? rel : basename(path), pattern, { dot: true, nonegate: true, nocomment: true });
  };
  const child = withCancel(context);
  const results: string[] = [];
  let bytes = 0, count = 0, pending = "", stderr = "", truncated = false;
  const stop = () => { truncated = true; child.cancel(); };
  const append = (line: string) => {
    const size = Buffer.byteLength(line) + 1;
    if (bytes + size > MAX_BYTES) { stop(); return; }
    results.push(line); bytes += size;
  };
  const accept = (record: string) => {
    if (truncated || !record) return;
    if (kind === "find") {
      if (!matches(record)) return;
      if (count >= limit) { stop(); return; }
      count++; append(record.replace(/^\.\//, "")); return;
    }
    let event: { type?: string; data?: { path?: unknown; lines?: unknown; line_number?: number } };
    try { event = JSON.parse(record); } catch { return; }
    if (event.type !== "match" && event.type !== "context") return;
    if (!matches(rgText(event.data?.path))) return;
    if (event.type === "match") {
      if (count >= limit) { stop(); return; }
      count++;
    }
    const data = event.data;
    if (!data || typeof data.line_number !== "number") return;
    const path = rgText(data.path).replace(/^\.\//, "");
    const text = rgText(data.lines).replace(/\r?\n$/, "");
    const short = text.length > 500 ? text.slice(0, 500) + "… [line truncated]" : text;
    const separator = event.type === "match" ? ":" : "-";
    append(`${path}${separator}${data.line_number}${separator} ${short}`);
  };
  const delimiter = kind === "find" ? "\0" : "\n";
  const result = await env.exec(command, {
    cwd: env.cwd, timeout: 30,
    onOutput(text, _context, info) {
      if (truncated) return;
      if (info.stream === "stderr") { stderr = (stderr + text).slice(0, 8192); return; }
      pending += text;
      let index: number;
      while (!truncated && (index = pending.indexOf(delimiter)) !== -1) {
        const record = pending.slice(0, index); pending = pending.slice(index + 1); accept(record);
      }
      if (Buffer.byteLength(pending) > MAX_EVENT_BYTES) stop();
    },
  }, child.context);
  if (context.abortSignal?.aborted) throw new Error("Search aborted");
  if (!result.ok && !truncated) {
    if (result.error.code === "spawn_error") throw new Error(`Unable to start ripgrep (${command[0]}). Install rg or configure METER_RG_PATH: ${result.error.message}`);
    throw result.error;
  }
  if (result.ok && result.value.exitCode !== 0 && result.value.exitCode !== 1) {
    throw new Error(stderr.trim() || `ripgrep exited with code ${result.value.exitCode}`);
  }
  if (!truncated && pending) accept(pending);
  return (results.length ? results.join("\n") : "No matches found") + (truncated ? "\n[Search truncated: result or output limit reached]" : "");
}

export function codeSearchTools() {
  return ([
    { name: "grep", description: "Search file contents using ripgrep. Respects ignore files; includes hidden files except .git. Returns paths and line numbers, with bounded output.", parameters: jsonSchema(grepParameters) },
    { name: "find", description: "Find files matching a glob using ripgrep. Respects ignore files; includes hidden files except .git. Returns paths, with bounded output.", parameters: jsonSchema(findParameters) },
  ] as const).map(({ name, description, parameters }) => defineTool({
    name, description, parameters,
    outputLimits: { maxBytes: MAX_BYTES + 1024 },
    async execute(args, api, context) {
      if (!api.env) throw new Error("Search requires a workspace execution environment");
      return { output: [{ type: "text" as const, text: await searchCode(name, args, api.env, context) }] };
    },
  }));
}
