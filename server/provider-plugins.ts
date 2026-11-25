import type { Provider } from "@earendil-works/pi-ai";

/** How long a provider keeps session processes, and how many (contracts `providerSessions`). */
export interface ProviderSessions {
  keepaliveMinutes: number;
  toolWaitMinutes: number;
  maxParked: number;
}

/** Server-owned integration metadata, separate from pi-ai's provider interface. */
export interface ProviderPlugin {
  provider: Provider;
  needsConversationCwd?: boolean;
  enabled?(): boolean;
  externalAuth?: {
    instruction: string;
    status(signal?: AbortSignal): Promise<{ status: "configured" | "unconfigured" | "unknown"; source?: string }>;
    refresh(signal?: AbortSignal): Promise<void>;
  };
  catalogSource?(): "builtin" | "cached" | "account";
  /** Session process limits; `set` applies them to running processes. */
  sessions?: { get(): ProviderSessions; set(value: ProviderSessions): Promise<void> };
  close(): Promise<void>;
}
export class ProviderPlugins {
  private readonly plugins = new Map<string, ProviderPlugin>();
  register(plugin: ProviderPlugin) {
    if (this.plugins.has(plugin.provider.id)) throw new Error(`Duplicate provider plugin: ${plugin.provider.id}`);
    this.plugins.set(plugin.provider.id, plugin);
  }
  get(id: string) { return this.plugins.get(id); }
}
