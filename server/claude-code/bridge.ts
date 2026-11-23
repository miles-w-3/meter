import { AgentDoc, ProviderDoc, GenerationTask, defineExtension, hook } from "@earendil-works/pi-durable";
import type { ProviderPlugins } from "../provider-plugins.ts";

/** Disposable request routing state; durable documents remain authoritative. */
export class ClaudeCwdBridge {
  private readonly sessions = new Map<string, { conversation: string; cwd: string }>();
  private readonly conversations = new Map<string, string>();
  private readonly timers = new Map<string, ReturnType<typeof setTimeout>>();

  clear(conversation: string) {
    const previous = this.conversations.get(conversation);
    if (previous) this.sessions.delete(previous);
    this.conversations.delete(conversation);
    const timer = this.timers.get(conversation);
    if (timer) clearTimeout(timer);
    this.timers.delete(conversation);
  }

  set(conversation: string, session: string, cwd: string) {
    const owner = this.sessions.get(session);
    if (owner && owner.conversation !== conversation) throw new Error("Claude Code session belongs to another conversation");
    this.clear(conversation);
    this.sessions.set(session, { conversation, cwd });
    this.conversations.set(conversation, session);
    const timer = setTimeout(() => this.clear(conversation), 5 * 60_000);
    timer.unref?.();
    this.timers.set(conversation, timer);
  }

  take(session: string | undefined): string {
    const entry = session && this.sessions.get(session);
    if (!entry || !session) throw new Error("Claude Code has no trusted conversation working-directory mapping");
    this.clear(entry.conversation);
    return entry.cwd;
  }

  extension(plugins: ProviderPlugins) {
    return defineExtension({ name: "meter-claude-cwd", hooks: [hook(GenerationTask, {
      beforeRequest: async (_request, api, context) => {
        const conversation = String(api.conversationId);
        this.clear(conversation);
        const agent = await api.snapshot(AgentDoc, api.conversationId, context);
        if (!agent?.model || !agent.cwd) return;
        const plugin = plugins.get(agent.model.provider);
        if (!plugin?.needsConversationCwd || plugin.enabled?.() === false) return;
        const provider = await api.snapshot(ProviderDoc, api.conversationId, context);
        if (provider?.sessionId) this.set(conversation, provider.sessionId, agent.cwd);
      },
      afterResponse: (_message, api) => { this.clear(String(api.conversationId)); },
    })] });
  }
}
