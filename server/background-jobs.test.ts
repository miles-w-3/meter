import { strict as assert } from "node:assert";
import { test } from "node:test";
import { BackgroundJobs, type BackgroundNotice } from "./background-jobs.ts";

function waitFor(predicate: () => boolean, ms = 3000): Promise<void> {
  return new Promise((resolve, reject) => {
    const deadline = Date.now() + ms;
    const check = () => predicate() ? resolve() : Date.now() > deadline ? reject(new Error("Timed out")) : setTimeout(check, 10);
    check();
  });
}

test("monitor emits batched stdout events before exit and scopes jobs by conversation", async () => {
  const notices: BackgroundNotice[] = [];
  const jobs = new BackgroundJobs(n => notices.push(n));
  try {
    const job = jobs.start({ conversationId: "one", label: "watch", command: "printf 'one\\ntwo\\n'; sleep 5", cwd: process.cwd(), mode: "monitor" });
    await waitFor(() => notices.some(n => n.type === "output"));
    const events = notices.find(n => n.type === "output");
    assert.equal(events?.type, "output");
    if (events?.type === "output") assert.deepEqual(events.lines, ["one", "two"]);
    assert.equal(jobs.list("one")[0]?.status, "running");
    assert.deepEqual(jobs.list("two"), []);
    await assert.rejects(jobs.stop("two", job.id), /Unknown/);
    assert.equal((await jobs.stop("one", job.id)).status, "stopped");
  } finally { await jobs.dispose(); }
});

test("exit jobs deliver completion rather than streaming events", async () => {
  const notices: BackgroundNotice[] = [];
  const jobs = new BackgroundJobs(n => notices.push(n));
  try {
    jobs.start({ conversationId: "one", label: "build", command: "echo out; echo err >&2; exit 3", cwd: process.cwd() });
    await waitFor(() => notices.some(n => n.type === "exit"));
    assert.equal(notices.length, 1);
    assert.equal(notices[0]?.job.exitCode, 3);
    assert.match(notices[0]!.job.output, /out/);
    assert.match(notices[0]!.job.output, /err/);
  } finally { await jobs.dispose(); }
});

test("event bursts are bounded and report dropped events", async () => {
  const notices: BackgroundNotice[] = [];
  const jobs = new BackgroundJobs(n => notices.push(n));
  try {
    jobs.start({ conversationId: "one", label: "burst", command: "for i in {1..100}; do echo event; done", cwd: process.cwd(), mode: "monitor" });
    await waitFor(() => notices.some(n => n.type === "exit"));
    const events = notices.find(n => n.type === "output");
    assert.equal(events?.type, "output");
    if (events?.type === "output") { assert.equal(events.lines.length, 50); assert.equal(events.dropped, 50); }
  } finally { await jobs.dispose(); }
});

test("stderr is retained but does not produce monitor events", async () => {
  const notices: BackgroundNotice[] = [];
  const jobs = new BackgroundJobs(n => notices.push(n));
  try {
    jobs.start({ conversationId: "one", label: "streams", command: "echo out; echo err >&2", cwd: process.cwd(), mode: "monitor" });
    await waitFor(() => notices.some(n => n.type === "exit"));
    const event = notices.find(n => n.type === "output");
    assert.equal(event?.type, "output");
    if (event?.type === "output") assert.deepEqual(event.lines, ["out"]);
    assert.match(notices.find(n => n.type === "exit")!.job.output, /err/);
  } finally { await jobs.dispose(); }
});

test("deadline stops a command and reports timed_out", async () => {
  const notices: BackgroundNotice[] = [];
  const jobs = new BackgroundJobs(n => notices.push(n));
  try {
    const job = jobs.start({ conversationId: "one", label: "deadline", command: "sleep 30", cwd: process.cwd(), timeoutMs: 1000 });
    assert.equal(job.deadlineAt, job.startedAt + 1000);
    await waitFor(() => notices.some(n => n.type === "exit"));
    assert.equal(notices.find(n => n.type === "exit")!.job.status, "timed_out");
  } finally { await jobs.dispose(); }
});
