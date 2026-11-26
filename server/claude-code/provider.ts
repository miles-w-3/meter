import { stat } from "node:fs/promises";
import { isAbsolute } from "node:path";
import { createAssistantMessageEventStream, type Api, type Model, type Provider, type SimpleStreamOptions, type TranscriptContext } from "@earendil-works/pi-ai";
import { getBuiltinModels } from "@earendil-works/pi-ai/providers/all";
import { builtinProviderModels, createModelRefresh, providerModelsFromStore } from "./vendor/src/account-models.ts";
import { createCliAuthStatusCache, probeCliAuthStatus, providerAuthCheck } from "./vendor/src/cli-auth.ts";
import { createEventBridge } from "./vendor/src/event-bridge.ts";
import { endStreamWithError } from "./vendor/src/terminal-event.ts";
import type { ProviderRuntime } from "./vendor/src/provider-runtime.ts";
import type { ClaudeCwdBridge } from "./bridge.ts";
import type { ProviderSessions } from "../provider-plugins.ts";

const ID = "pi-claude-cli";
/** Default retention and capacity; prompt-cache lifetimes vary by request. */
export const DEFAULT_CLAUDE_SESSIONS: ProviderSessions = { keepaliveMinutes: 60, toolWaitMinutes: 60, maxParked: 3 };
export function createClaudeProvider(bridge: ClaudeCwdBridge, initiallyEnabled = false, initialSessions: ProviderSessions = DEFAULT_CLAUDE_SESSIONS) {
  let enabled = initiallyEnabled;
  let sessions = { ...initialSessions };
  let epoch = 0;
  let activeTurns = 0;
  const catalogue = getBuiltinModels("anthropic");
  const piModel = (model: ReturnType<typeof builtinProviderModels>[number]): Model<Api> => ({ ...model, api: ID, provider: ID, baseUrl: ID });
  let models = builtinProviderModels(catalogue).map(piModel);
  let source: "builtin" | "cached" | "account" = "builtin";
  const auth = createCliAuthStatusCache(() => probeCliAuthStatus(), { read: async () => undefined, write: async () => {} });
  const refresh = createModelRefresh(catalogue, {
    version: async (signal) => (await import("./vendor/src/model-probe.ts")).probeCliVersion(signal),
    models: async (signal) => (await import("./vendor/src/model-probe.ts")).probeAccountModels(signal),
  });
  let loaded: Promise<ProviderRuntime> | undefined;
  // The pool reads its limits on each use, so a saved change applies without a restart.
  const runtime = () => loaded ??= Promise.all([
    import("./vendor/src/provider-runtime.ts"), import("./vendor/src/provider-config.ts"),
  ]).then(([module, config]) => module.startProviderRuntime(module.providerLayerWith(
    config.providerConfigLayerWith({ poolLimits: () => config.poolLimitsFromMinutes(sessions), dateOverride: () => sessions.dateOverride === true }),
  )));
  const stream = (model: Model<Api>, context: TranscriptContext, options?: SimpleStreamOptions) => {
    const events = createAssistantMessageEventStream();
    void (async () => {
      try {
        if (!enabled) throw new Error("Claude Code provider is disabled");
        // Compaction bypasses the generation hook; do not reuse a stale mapping.
        if (options?.cacheRetention === "none") throw new Error("Claude Code compaction is not supported by the cwd bridge");
        const cwd = bridge.take(options?.sessionId);
        if (!isAbsolute(cwd) || !(await stat(cwd)).isDirectory()) throw new Error("Claude Code conversation cwd is not an accessible absolute directory");
        const activeRuntime = await runtime();
        if (!enabled) throw new Error("Claude Code provider is disabled");
        // A request already running when disabled may finish, but must not leave
        // a newly parked child behind after disable completes.
        const startedAt = epoch;
        void events.result().then(() => {
          if (epoch !== startedAt) void activeRuntime.retireSession(options?.sessionId);
        });
        activeRuntime.startEpisode(events, model, context, { ...options, cwd });
        activeTurns++;
        void events.result().then(() => { activeTurns--; });
      } catch (error) {
        const output = createEventBridge(events, model).getOutput();
        endStreamWithError(events, output, options?.signal?.aborted ? "aborted" : "error", error instanceof Error ? error.message : String(error));
      }
    })();
    return events;
  };
  const provider: Provider = {
    id: ID, name: "Claude Code subscription (experimental)", baseUrl: ID,
    auth: { apiKey: {
      name: "Claude Code login",
      check: async ({ signal }) => {
        if (!enabled) return undefined;
        const status = await auth.status(signal);
        return status.loggedIn === true ? providerAuthCheck(status) : undefined;
      },
      resolve: async () => ({ auth: { apiKey: "unused" }, source: "Claude Code CLI" }),
    }, oauth: {
      name: "Claude subscription (managed by Claude Code)", isSubscription: true,
      login: async () => { throw new Error("Run claude auth login on the backend host"); },
      refresh: async (credential) => credential,
      toAuth: async () => ({ apiKey: "unused" }),
    } },
    getModels: () => enabled ? models : [],
    refreshModels: async (context) => {
      let discovered = false;
      const refreshed = (await refresh({ ...context, publish: async (publication) => {
        if (publication.persist && publication.persist.models.length) discovered = true;
        return context.publish(publication);
      } })).map(piModel);
      if (!context.signal.aborted) await context.publish({ update: () => {
        models = refreshed;
        if (discovered) source = "account";
        else if (providerModelsFromStore(catalogue, context.stored)) source = source === "account" ? "account" : "cached";
        else source = "builtin";
      } });
    },
    stream: (model, context, options) => stream(model, context, options as SimpleStreamOptions),
    streamSimple: stream,
  };
  return { provider, needsConversationCwd: true, catalogSource: () => source,
    enabled: () => enabled,
    sessionStats: async () => ({
      ...(loaded ? await (await loaded).stats() : { liveProcesses: 0, parkedIdle: 0, parkedWaitingForTools: 0 }),
      activeTurns,
    }),
    sessions: {
      get: () => ({ ...sessions, dateOverride: sessions.dateOverride === true }),
      set: async (value: ProviderSessions) => {
        sessions = { ...value };
        if (loaded) await (await loaded).reconfigurePool();
      },
    },
    setEnabled: async (value: boolean) => {
      if (enabled !== value) epoch++;
      enabled = value;
      if (!value && loaded) await (await loaded).retireParkedProcesses();
    },
    externalAuth: {
      instruction: "Install Claude Code and run claude auth login on the backend host, then refresh.",
      status: async (signal?: AbortSignal) => {
        const status = await auth.status(signal);
        if (!status.installed) return { status: "unconfigured" as const, source: "Claude Code not installed" };
        if (status.loggedIn === false) return { status: "unconfigured" as const, source: "Claude Code not logged in" };
        if (status.loggedIn === undefined) return { status: "unknown" as const, source: "Claude Code login status unavailable" };
        return { status: "configured" as const, source: status.subscriptionType ? `Claude Code ${status.subscriptionType}` : "Claude Code login (billing unknown)" };
      },
      refresh: async (signal?: AbortSignal) => { await auth.refresh(signal); },
    },
    close: async () => { if (loaded) await (await loaded).dispose(); } };

}
