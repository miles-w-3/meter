/**
 * The CLI processes of one runtime: launching them, and parking one per pi
 * session between calls.
 *
 * Every process scope is a child of this service's scope, so disposing the
 * runtime ends every process, parked or borrowed. A parked process is
 * exclusive: `take` removes it, so two calls can never share one.
 *
 * Only one call at a time owns a pi session's place in the pool: `take`
 * hands it a lease, and only the lease's holder may park. A call that runs
 * while another of the same session is in flight gets no lease, and its
 * process is retired when it ends instead of replacing the owner's.
 *
 * At most `maxParked` processes are parked at once (each holds hundreds of
 * megabytes); parking one more retires the longest-parked, preferring one
 * that is not inside a tool call. The limits are read when they apply, so a
 * host may change them; `reconfigure` applies a change to the processes
 * already parked.
 */

import { Clock, Context, Duration, Effect, FiberHandle, HashMap, Layer, Option, Ref, Semaphore } from "effect";
import { ClaudeCli, claudeCliLayer } from "./claude-cli.ts";
import {
  type ClaudeProcessLaunch,
  type IClaudeProcess,
  launchClaudeProcess,
  type RetireCause,
} from "./claude-process.ts";
import type { CliRequirementError, CliSpawnError } from "./errors.ts";
import { ProviderConfig, providerConfigLayer } from "./provider-config.ts";

/** One call's claim on its pi session's place in the pool. */
export interface SessionLease {
  readonly piSessionId: string;
  readonly token: number;
}

/** What `take` gives a call. */
export interface TakenSession {
  /** The live process parked for the session, if any. */
  readonly process: Option.Option<IClaudeProcess>;
  /** None while another call of the session holds the lease. */
  readonly lease: Option.Option<SessionLease>;
}

/** Defines the pool of CLI processes. */
export interface IClaudeProcessPool {
  /** Launch a process that lives until it is retired or the runtime is disposed. */
  readonly launch: (
    launch: ClaudeProcessLaunch,
  ) => Effect.Effect<IClaudeProcess, CliRequirementError | CliSpawnError>;
  /**
   * Remove and return the live process parked for a pi session, if any, and
   * the session's lease unless another call holds it. The caller must end
   * the lease (`park` or `endLease`).
   */
  readonly take: (piSessionId: string) => Effect.Effect<TakenSession>;
  /**
   * Keep a process for its pi session's next call, and retire it if nobody
   * comes back within `idle`. Ends the lease. A process already parked there
   * is retired; so is this one if the lease is no longer the session's.
   */
  readonly park: (
    lease: SessionLease,
    process: IClaudeProcess,
    idle: Duration.Duration,
  ) => Effect.Effect<void>;
  /** Give up a lease without parking. Idempotent. */
  readonly endLease: (lease: SessionLease) => Effect.Effect<void>;
  /**
   * Retire a process without waiting for it: retiring can take seconds, and
   * pi must not wait that long for an error or an abort.
   */
  readonly retireInBackground: (process: IClaudeProcess, cause: RetireCause) => Effect.Effect<void>;
  /** Retire every parked process and wait for them to be gone. */
  readonly retireAll: (cause: RetireCause) => Effect.Effect<void>;
  /** Retire the process parked for one pi session, if any, and wait for it. */
  readonly retireSession: (piSessionId: string, cause: RetireCause) => Effect.Effect<void>;
  /**
   * Apply the current limits to the processes already parked: retire the
   * longest-parked over the cap, and time each one's idle lifetime again
   * from when it was parked.
   */
  readonly reconfigure: () => Effect.Effect<void>;
}

/** Provides the pool of CLI processes. */
export class ClaudeProcessPool extends Context.Service<ClaudeProcessPool, IClaudeProcessPool>()(
  "pi-claude-cli/ClaudeProcessPool",
) {}

interface ParkedProcess {
  readonly process: IClaudeProcess;
  readonly parkedAtMillis: number;
}

/**
 * Kill every live CLI process when pi exits without the runtime having been
 * disposed. Effect finalizers cannot run in an `exit` handler, so this last
 * resort is synchronous.
 */
const killOnProcessExit = (livePids: Set<number>) =>
  Effect.acquireRelease(
    Effect.sync(() => {
      const kill = () => {
        for (const pid of livePids) {
          try {
            process.kill(pid, "SIGKILL");
          } catch {
            // Already gone.
          }
        }
      };
      process.on("exit", kill);
      return kill;
    }),
    (kill) => Effect.sync(() => process.removeListener("exit", kill)),
  );

/** Constructs the pool; its scope is the parent of every process scope. */
export const makeClaudeProcessPool = Effect.gen(function* () {
  const cli = yield* ClaudeCli;
  const { poolLimits } = yield* ProviderConfig;
  const runtimeScope = yield* Effect.scope;
  const parked = yield* Ref.make(HashMap.empty<string, ParkedProcess>());
  /** The lease token of each pi session with a call in flight. */
  const leases = yield* Ref.make(HashMap.empty<string, number>());
  const leaseSequence = yield* Ref.make(0);
  // Serialize ownership changes, timer expiry, and reconfiguration.
  const locked = Semaphore.makeUnsafe(1).withPermits(1);
  const livePids = new Set<number>();
  yield* killOnProcessExit(livePids);

  const retireInBackground = (process: IClaudeProcess, cause: RetireCause) =>
    Effect.asVoid(Effect.forkIn(process.retire(cause), runtimeScope));

  /** Drop the session's entry, but only while it still holds this process. */
  const forget = (piSessionId: string, process: IClaudeProcess) =>
    Ref.update(parked, (entries) =>
      Option.exists(HashMap.get(entries, piSessionId), (entry) => entry.process === process)
        ? HashMap.remove(entries, piSessionId)
        : entries,
    );

  /** End the lease if it is still the session's; true when it was. */
  const endLease = (lease: SessionLease) =>
    Ref.modify(leases, (entries) =>
      Option.exists(HashMap.get(entries, lease.piSessionId), (token) => token === lease.token)
        ? [true, HashMap.remove(entries, lease.piSessionId)]
        : [false, entries],
    );

  /** Retire the longest-parked process while more than `maxParked` are parked. */
  const evictOverCap = (keep: string | undefined) =>
    Effect.gen(function* () {
      while (HashMap.size(yield* Ref.get(parked)) > poolLimits().maxParked) {
        const candidates = [];
        for (const [piSessionId, entry] of yield* Ref.get(parked)) {
          if (piSessionId === keep) continue;
          candidates.push({ piSessionId, entry, inToolCall: yield* entry.process.isTurnActive });
        }
        candidates.sort(
          (a, b) =>
            Number(a.inToolCall) - Number(b.inToolCall) ||
            a.entry.parkedAtMillis - b.entry.parkedAtMillis,
        );
        const victim = candidates[0];
        if (victim === undefined) return;
        yield* forget(victim.piSessionId, victim.entry.process);
        yield* FiberHandle.clear(victim.entry.process.idleTimer);
        yield* Effect.annotateCurrentSpan({ evictedPid: victim.entry.process.pid });
        yield* retireInBackground(victim.entry.process, "evicted");
      }
    });

  /**
   * Retire a parked process once it has been idle for `idle`. The timer lives
   * in the process scope, so retiring the process removes it; the retire it
   * triggers must therefore run outside that scope.
   */
  const armIdleTimer = (piSessionId: string, process: IClaudeProcess, idle: Duration.Duration) =>
    Effect.sleep(idle).pipe(
      Effect.andThen(locked(Effect.gen(function* () {
        const current = yield* Ref.get(parked);
        if (Option.exists(HashMap.get(current, piSessionId), (entry) => entry.process === process)) {
          yield* forget(piSessionId, process);
          yield* retireInBackground(process, "idle");
        }
      }))),
      FiberHandle.run(process.idleTimer),
    );

  return ClaudeProcessPool.of({
    launch: (launch) =>
      Effect.gen(function* () {
        const process = yield* launchClaudeProcess(launch, runtimeScope);
        livePids.add(process.pid);
        yield* process.awaitRetired.pipe(
          Effect.andThen(Effect.sync(() => livePids.delete(process.pid))),
          Effect.forkIn(runtimeScope),
        );
        return process;
      }).pipe(Effect.provideService(ClaudeCli, cli)),

    take: (piSessionId: string) => locked(Effect.gen(function* () {
      const token = yield* Ref.updateAndGet(leaseSequence, (n) => n + 1);
      const leased = yield* Ref.modify(leases, (entries) =>
        HashMap.has(entries, piSessionId)
          ? [false, entries]
          : [true, HashMap.set(entries, piSessionId, token)],
      );
      const lease = leased
        ? Option.some<SessionLease>({ piSessionId, token })
        : Option.none<SessionLease>();
      yield* Effect.annotateCurrentSpan({ leased });
      // A call without the lease leaves the owner's process alone.
      const entry = leased
        ? yield* Ref.modify(parked, (entries) => [
            HashMap.get(entries, piSessionId),
            HashMap.remove(entries, piSessionId),
          ])
        : Option.none<ParkedProcess>();
      if (Option.isNone(entry)) {
        yield* Effect.annotateCurrentSpan({ hit: false });
        return { process: Option.none(), lease } satisfies TakenSession;
      }
      const { process, parkedAtMillis } = entry.value;
      yield* FiberHandle.clear(process.idleTimer);
      const alive = yield* process.isAlive;
      yield* Effect.annotateCurrentSpan({
        hit: alive,
        pid: process.pid,
        idleMs: (yield* Clock.currentTimeMillis) - parkedAtMillis,
      });
      return {
        process: alive ? Option.some(process) : Option.none(),
        lease,
      } satisfies TakenSession;
    })),

    park: (lease: SessionLease, process: IClaudeProcess, idle: Duration.Duration) => locked(Effect.gen(function* () {
      const { piSessionId } = lease;
      yield* Effect.annotateCurrentSpan({
        pid: process.pid,
        idleMs: Duration.toMillis(idle),
      });
      // A lease already ended is no claim on the session: never displace
      // whoever holds it now.
      if (!(yield* endLease(lease))) {
        yield* Effect.annotateCurrentSpan({ leased: false });
        return yield* retireInBackground(process, "concurrent");
      }
      const parkedAtMillis = yield* Clock.currentTimeMillis;
      const previous = yield* Ref.modify(parked, (entries) => [
        HashMap.get(entries, piSessionId),
        HashMap.set(entries, piSessionId, { process, parkedAtMillis }),
      ]);
      if (Option.isSome(previous) && previous.value.process !== process)
        yield* retireInBackground(previous.value.process, "replaced");
      yield* armIdleTimer(piSessionId, process, idle);
      yield* evictOverCap(piSessionId);
    })),

    endLease: (lease) => Effect.asVoid(endLease(lease)),

    retireInBackground,

    retireAll: (cause: RetireCause) => locked(Effect.gen(function* () {
      const entries = yield* Ref.getAndSet(parked, HashMap.empty());
      yield* Effect.annotateCurrentSpan({
        cause,
        count: HashMap.size(entries),
      });
      yield* Effect.forEach(HashMap.toValues(entries), (entry) => entry.process.retire(cause), {
        concurrency: "unbounded",
        discard: true,
      });
    })),

    retireSession: (piSessionId: string, cause: RetireCause) => locked(Effect.gen(function* () {
      const entry = yield* Ref.modify(parked, (entries) => [
        HashMap.get(entries, piSessionId),
        HashMap.remove(entries, piSessionId),
      ]);
      yield* Effect.annotateCurrentSpan({ cause, hit: Option.isSome(entry) });
      if (Option.isSome(entry)) yield* entry.value.process.retire(cause);
    })),

    reconfigure: () => locked(Effect.gen(function* () {
      const limits = poolLimits();
      yield* Effect.annotateCurrentSpan({
        maxParked: limits.maxParked,
        keepaliveMs: Option.match(limits.keepalive, { onNone: () => 0, onSome: Duration.toMillis }),
        handoffWaitMs: Duration.toMillis(limits.handoffWait),
      });
      yield* evictOverCap(undefined);
      const now = yield* Clock.currentTimeMillis;
      for (const [piSessionId, entry] of yield* Ref.get(parked)) {
        // A process parked inside a tool call waits for pi's results.
        const idle = (yield* entry.process.isTurnActive)
          ? Option.some(limits.handoffWait)
          : limits.keepalive;
        if (Option.isNone(idle)) {
          yield* forget(piSessionId, entry.process);
          yield* FiberHandle.clear(entry.process.idleTimer);
          yield* retireInBackground(entry.process, "idle");
          continue;
        }
        const remaining = Math.max(0, Duration.toMillis(idle.value) - (now - entry.parkedAtMillis));
        yield* FiberHandle.clear(entry.process.idleTimer);
        yield* armIdleTimer(piSessionId, entry.process, Duration.millis(remaining));
      }
    })),
  });
});

/** Provides the pool without selecting how the CLI is launched. */
export const claudeProcessPoolLayerWithoutDependencies = Layer.effect(
  ClaudeProcessPool,
  makeClaudeProcessPool,
);

/** Provides the pool over the real Claude CLI. */
export const claudeProcessPoolLayer = claudeProcessPoolLayerWithoutDependencies.pipe(
  Layer.provide([claudeCliLayer, providerConfigLayer]),
);
