// Reconfiguration must not rearm an idle timer on a borrowed process.
import assert from "node:assert/strict";
import { Deferred, Duration, Effect, Fiber, FiberHandle, Option } from "effect";
import { makeClaudeProcessPool } from "../server/claude-code/vendor/src/claude-process-pool.ts";
import { ClaudeCli } from "../server/claude-code/vendor/src/claude-cli.ts";
import { ProviderConfig } from "../server/claude-code/vendor/src/provider-config.ts";

await Effect.runPromise(Effect.scoped(Effect.gen(function* () {
 const entered = yield* Deferred.make();
 const proceed = yield* Deferred.make();
 const idleTimer = yield* FiberHandle.make();
 let retired = false, taken = false;
 const process = {
  pid: 12345, idleTimer, isAlive: Effect.succeed(true),
  isTurnActive: Effect.gen(function* () { yield* Deferred.succeed(entered, undefined); yield* Deferred.await(proceed); return false; }),
  retire: () => Effect.sync(() => { retired = true; }),
 };
 const pool = yield* makeClaudeProcessPool.pipe(
  Effect.provideService(ClaudeCli, {}),
  Effect.provideService(ProviderConfig, {poolLimits:()=>({maxParked:1,keepalive:Option.some(Duration.millis(200)),handoffWait:Duration.millis(200)})}),
 );
 const initial = yield* pool.take("session");
 yield* pool.park(Option.getOrThrow(initial.lease), process, Duration.seconds(10));
 const reconfigure = yield* Effect.forkScoped(pool.reconfigure());
 yield* Deferred.await(entered);
 const take = yield* Effect.forkScoped(pool.take("session").pipe(Effect.tap(() => Effect.sync(() => { taken = true; }))));
 yield* Effect.sleep(Duration.millis(20));
 assert.equal(taken, false, "a borrower must wait for reconfiguration ownership changes");
 yield* Deferred.succeed(proceed, undefined);
 yield* Fiber.join(reconfigure);
 const borrowed = yield* Fiber.join(take);
 assert.equal(Option.isSome(borrowed.process), true);
 yield* Effect.sleep(Duration.millis(250));
 assert.equal(retired, false, "a reconfigured idle timer must not retire a borrowed process");
})));
console.log("Claude pool: reconfigure and take are serialized");

// An idle timer may expire while reconfiguration owns the pool lock. Clearing
// that timer must not deadlock waiting for a fiber which is waiting for the lock.
await Effect.runPromise(Effect.timeout(Effect.scoped(Effect.gen(function* () {
 const entered = yield* Deferred.make();
 const proceed = yield* Deferred.make();
 const idleTimer = yield* FiberHandle.make();
 const process = {
  pid: 12346, idleTimer, isAlive: Effect.succeed(true),
  isTurnActive: Effect.gen(function* () { yield* Deferred.succeed(entered, undefined); yield* Deferred.await(proceed); return false; }),
  retire: () => Effect.void,
 };
 const pool = yield* makeClaudeProcessPool.pipe(
  Effect.provideService(ClaudeCli, {}),
  Effect.provideService(ProviderConfig, {poolLimits:()=>({maxParked:1,keepalive:Option.some(Duration.seconds(10)),handoffWait:Duration.seconds(10)})}),
 );
 const initial = yield* pool.take("session");
 yield* pool.park(Option.getOrThrow(initial.lease), process, Duration.millis(20));
 const reconfigure = yield* Effect.forkScoped(pool.reconfigure());
 yield* Deferred.await(entered);
 yield* Effect.sleep(Duration.millis(60));
 yield* Deferred.succeed(proceed, undefined);
 yield* Fiber.join(reconfigure);
})), Duration.seconds(3)));
console.log("Claude pool: expiring timer does not deadlock reconfiguration");
