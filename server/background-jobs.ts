import { spawn, type ChildProcess } from "node:child_process";
import { StringDecoder } from "node:string_decoder";
import { randomUUID } from "node:crypto";

/** In-memory, daemon-owned process supervisor. No Pi or GUI dependencies. */
export type BackgroundMode = "exit" | "monitor";
export type BackgroundStatus = "running" | "exited" | "stopped" | "timed_out" | "failed";
export interface BackgroundJob {
  id: string;
  conversationId: string;
  label: string;
  command: string;
  cwd: string;
  mode: BackgroundMode;
  status: BackgroundStatus;
  pid?: number;
  startedAt: number;
  endedAt?: number;
  exitCode?: number | null;
  signal?: NodeJS.Signals | null;
  events: number;
  droppedEvents: number;
  output: string;
}
export type BackgroundNotice =
  | { type: "output"; job: BackgroundJob; lines: string[]; dropped: number }
  | { type: "exit"; job: BackgroundJob };
interface RecordState {
  job: BackgroundJob;
  child: ChildProcess;
  partial: string;
  decoder: StringDecoder;
  pending: string[];
  dropped: number;
  flushTimer?: NodeJS.Timeout;
  timeout?: NodeJS.Timeout;
  killTimer?: NodeJS.Timeout;
  done: Promise<void>;
  finish: () => void;
}

const MAX_OUTPUT = 64 * 1024;
const MAX_LINE = 4096;
const MAX_PENDING = 50;
const BATCH_MS = 250;
const MAX_JOBS = 8;

export class BackgroundJobs {
  private readonly records = new Map<string, RecordState>();
  private disposed = false;
  private readonly notify: (notice: BackgroundNotice) => void;
  constructor(notify: (notice: BackgroundNotice) => void = () => {}) { this.notify = notify; }

  list(conversationId: string): BackgroundJob[] {
    return [...this.records.values()].filter(r => r.job.conversationId === conversationId).map(r => ({ ...r.job }));
  }

  start(input: { conversationId: string; label: string; command: string; cwd: string; mode?: BackgroundMode; timeoutMs?: number }): BackgroundJob {
    if (this.disposed) throw new Error("Background jobs are shut down");
    if (!input.conversationId || !input.command.trim() || !input.cwd) throw new Error("conversationId, command and cwd are required");
    if ([...this.records.values()].filter(r => r.job.status === "running").length >= MAX_JOBS) throw new Error("Too many background jobs");
    const mode = input.mode ?? "exit";
    if (mode !== "exit" && mode !== "monitor") throw new Error("Invalid background mode");
    const timeoutMs = input.timeoutMs ?? (mode === "monitor" ? 300_000 : undefined);
    if (timeoutMs !== undefined && (!Number.isFinite(timeoutMs) || timeoutMs < 1000 || timeoutMs > 1_800_000)) throw new Error("Timeout must be between 1 second and 30 minutes");
    const job: BackgroundJob = { id: randomUUID(), conversationId: input.conversationId, label: input.label.trim() || input.command.trim().slice(0, 60), command: input.command, cwd: input.cwd, mode, status: "running", startedAt: Date.now(), events: 0, droppedEvents: 0, output: "" };
    const child = spawn("/bin/bash", ["-c", input.command], { cwd: input.cwd, detached: true, stdio: ["ignore", "pipe", "pipe"] });
    job.pid = child.pid;
    let finish!: () => void;
    const done = new Promise<void>(resolve => { finish = resolve; });
    const record: RecordState = { job, child, partial: "", decoder: new StringDecoder("utf8"), pending: [], dropped: 0, done, finish };
    this.records.set(job.id, record);
    child.stdout?.on("data", (chunk: Buffer) => this.stdout(record, record.decoder.write(chunk)));
    child.stderr?.on("data", (chunk: Buffer) => this.appendOutput(record, chunk.toString("utf8")));
    child.once("error", error => { this.appendOutput(record, `\n${error.message}\n`); if (job.status === "running") job.status = "failed"; });
    child.once("close", (code, signal) => {
      clearTimeout(record.timeout);
      this.stdout(record, record.decoder.end());
      if (record.partial) { this.line(record, record.partial); record.partial = ""; }
      this.flush(record);
      if (job.status === "running") job.status = "exited";
      job.endedAt = Date.now(); job.exitCode = code; job.signal = signal;
      record.finish();
      if (!this.disposed) this.emit({ type: "exit", job: { ...job } });
    });
    if (timeoutMs !== undefined) {
      record.timeout = setTimeout(() => { void this.stop(input.conversationId, job.id, "timed_out"); }, timeoutMs);
      record.timeout.unref();
    }
    return { ...job };
  }

  async stop(conversationId: string, id: string, reason: "stopped" | "timed_out" = "stopped"): Promise<BackgroundJob> {
    const record = this.records.get(id);
    if (!record || record.job.conversationId !== conversationId) throw new Error("Unknown background job");
    if (record.job.status === "running") {
      record.job.status = reason;
      this.kill(record.child.pid, "SIGTERM");
      // Continue escalation even if the shell exits: a descendant may ignore TERM.
      record.killTimer = setTimeout(() => this.kill(record.child.pid, "SIGKILL"), 5000);
      record.killTimer.unref();
    }
    await record.done;
    return { ...record.job };
  }

  async dispose(): Promise<void> {
    this.disposed = true;
    await Promise.all([...this.records.values()].filter(r => r.job.status === "running").map(r => this.stop(r.job.conversationId, r.job.id)));
  }

  private kill(pid: number | undefined, signal: NodeJS.Signals): void {
    if (!pid) return;
    try { process.kill(-pid, signal); } catch { /* already gone */ }
  }
  private emit(notice: BackgroundNotice): void { try { this.notify(notice); } catch { /* consumer failure must not affect supervision */ } }
  private appendOutput(r: RecordState, text: string): void { r.job.output = (r.job.output + text).slice(-MAX_OUTPUT); }
  private stdout(r: RecordState, text: string): void {
    if (!text) return;
    this.appendOutput(r, text);
    if (r.job.mode !== "monitor") return;
    r.partial += text;
    let newline: number;
    while ((newline = r.partial.indexOf("\n")) >= 0) {
      const line = r.partial.slice(0, newline).replace(/\r$/, "");
      r.partial = r.partial.slice(newline + 1);
      this.line(r, line);
    }
    // An unterminated line must not consume unbounded memory.
    if (r.partial.length > MAX_LINE) r.partial = r.partial.slice(0, MAX_LINE) + "…";
  }
  private line(r: RecordState, line: string): void {
    if (r.job.mode !== "monitor") return;
    r.job.events++;
    if (r.pending.length < MAX_PENDING) r.pending.push(line.slice(0, MAX_LINE));
    else { r.dropped++; r.job.droppedEvents++; }
    if (!r.flushTimer) { r.flushTimer = setTimeout(() => this.flush(r), BATCH_MS); r.flushTimer.unref(); }
  }
  private flush(r: RecordState): void {
    clearTimeout(r.flushTimer); r.flushTimer = undefined;
    if (!r.pending.length && !r.dropped) return;
    const lines = r.pending.splice(0); const dropped = r.dropped; r.dropped = 0;
    if (!this.disposed) this.emit({ type: "output", job: { ...r.job }, lines, dropped });
  }
}
