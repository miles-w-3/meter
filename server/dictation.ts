import { spawn, type ChildProcessByStdio } from "node:child_process";
import { constants, accessSync } from "node:fs";
import { join } from "node:path";
import type { Readable, Writable } from "node:stream";
import { WebSocket, type RawData } from "ws";

const maxAudioFrame = 4096;
const maxAudioBytes = 16_000 * 2 * 120;
const maxPendingBytes = 32_000;
const maxReplyBytes = 256 * 1024;

type Worker = ChildProcessByStdio<Writable, Readable, null>;
type Session = { socket: WebSocket; phase: "starting" | "recording" | "finishing"; bytes: number; timeout: NodeJS.Timeout };

export function dictation(directory: string, data: string) {
  const executable = join(directory, "meter-dictation");
  const candidates = [join(data, "parakeet-eou-q8.gguf"), join(directory, "parakeet-eou-q8.gguf")];
  function accessible(path: string, mode: number) {
    try {
      accessSync(path, mode);
      return true;
    } catch {
      return false;
    }
  }
  function modelPath() {
    return candidates.find(path => accessible(path, constants.R_OK));
  }

  let worker: Worker | undefined;
  let session: Session | undefined;
  let idle: NodeJS.Timeout | undefined;

  function stopWorker() {
    clearTimeout(idle);
    const child = worker;
    worker = undefined;
    if (!child) return;
    child.stdin.destroy();
    child.kill("SIGKILL");
  }

  function release() {
    if (session) clearTimeout(session.timeout);
    session = undefined;
  }

  function fail(message: string) {
    const socket = session?.socket;
    release();
    stopWorker();
    if (socket?.readyState === WebSocket.OPEN) {
      socket.send(JSON.stringify({ type: "error", message }));
      socket.close(1011);
    }
  }

  function write(kind: string, payload: Buffer = Buffer.alloc(0)) {
    if (!worker) return;
    const header = Buffer.alloc(5);
    header.write(kind, 0);
    header.writeUInt32BE(payload.length, 1);
    worker.stdin.write(Buffer.concat([header, payload]));
  }

  function receive(child: Worker, kind: string, text: string) {
    const current = session;
    if (worker !== child || !current) return;
    if (current.socket.readyState !== WebSocket.OPEN) {
      release();
      stopWorker();
      return;
    }
    switch (kind) {
      case "R":
        write("S");
        break;
      case "S":
        current.phase = "recording";
        clearTimeout(current.timeout);
        current.timeout = setTimeout(() => fail("Dictation session expired"), 125_000).unref();
        current.socket.send(JSON.stringify({ type: "ready" }));
        break;
      case "P":
      case "F": {
        if (current.socket.bufferedAmount > maxReplyBytes) return fail("Dictation client is too slow");
        current.socket.send(JSON.stringify({ type: kind === "P" ? "partial" : "final", text }));
        if (kind === "F") {
          release();
          idle = setTimeout(stopWorker, 60_000).unref();
          current.socket.close();
        }
        break;
      }
      default:
        fail("Invalid dictation worker response");
    }
  }

  function startWorker(model: string) {
    const child = spawn(executable, [model], {
      stdio: ["pipe", "pipe", "inherit"],
      // Only local installation paths are passed to the worker.
      env: { PATH: "/usr/bin:/bin", LC_ALL: "C" },
    });
    worker = child;
    let buffer = Buffer.alloc(0);
    const died = () => {
      if (worker === child) fail("Dictation worker stopped");
    };
    child.on("error", died);
    child.on("exit", died);
    child.stdin.on("error", died);
    child.stdout.on("data", (chunk: Buffer) => {
      if (worker !== child) return;
      buffer = Buffer.concat([buffer, chunk]);
      while (buffer.length >= 5) {
        const size = buffer.readUInt32BE(1);
        if (size > maxReplyBytes) return fail("Invalid dictation worker response");
        if (buffer.length < 5 + size) return;
        const kind = String.fromCharCode(buffer[0]);
        const text = buffer.toString("utf8", 5, 5 + size);
        buffer = buffer.subarray(5 + size);
        receive(child, kind, text);
      }
    });
  }

  function attach(socket: WebSocket) {
    const model = modelPath();
    const available = model && accessible(executable, constants.X_OK);
    if (!available || session) {
      socket.send(JSON.stringify({ type: "error", message: available ? "Dictation is busy" : "Dictation files are missing" }));
      socket.close(1008);
      return;
    }
    clearTimeout(idle);
    const current: Session = {
      socket, phase: "starting", bytes: 0,
      timeout: setTimeout(() => fail("Dictation model did not start"), 20_000).unref(),
    };
    session = current;
    socket.on("error", () => socket.terminate());
    socket.on("close", () => {
      if (session === current) {
        release();
        stopWorker();
      }
    });
    socket.on("message", (data: RawData, binary) => {
      if (session !== current) return;
      if (!Buffer.isBuffer(data) || current.phase !== "recording") return fail("Unexpected dictation frame");
      if (!binary) {
        if (data.toString() !== "finish") return fail("Unknown dictation command");
        current.phase = "finishing";
        clearTimeout(current.timeout);
        current.timeout = setTimeout(() => fail("Dictation did not finish"), 10_000).unref();
        write("F");
        return;
      }
      current.bytes += data.length;
      if (!data.length || data.length % 2 || data.length > maxAudioFrame || current.bytes > maxAudioBytes) {
        return fail("Invalid or oversized dictation audio");
      }
      if ((worker?.stdin.writableLength ?? 0) > maxPendingBytes) return fail("Dictation audio is arriving too quickly");
      write("A", data);
    });
    if (worker) write("S");
    else startWorker(model);
  }

  return {
    attach, maxAudioFrame,
    get status() {
      if (!accessible(executable, constants.X_OK)) return "worker_missing";
      return modelPath() ? "ready" : "model_missing";
    },
    close() {
      session?.socket.terminate();
      release();
      stopWorker();
    },
  };
}
