import { BACKGROUND_CONTEXT as context } from "@earendil-works/chord/context";
import type { ConversationId, ConversationRecord, Cursor, Harness } from "@earendil-works/pi-durable";

/** History ancestry is separate from task ownership. A child conversation that
 * does not fork history must not be mistaken for a branch of the transcript. */
export function historyTree(records: readonly ConversationRecord[], selected: ConversationId) {
  const byId = new Map(records.map((record) => [record.id, record]));
  let current = byId.get(selected);
  if (!current) return undefined;
  const visited = new Set<ConversationId>();
  while (current.parent) {
    if (visited.has(current.id)) throw new Error("Conversation ancestry contains a cycle");
    visited.add(current.id);
    const parent = byId.get(current.parent.conversationId);
    if (!parent) throw new Error("Conversation ancestry is incomplete");
    current = parent;
  }
  const root = current.id;
  const children = new Map<ConversationId, ConversationRecord[]>();
  for (const record of records) {
    if (!record.parent) continue;
    const siblings = children.get(record.parent.conversationId) ?? [];
    siblings.push(record);
    children.set(record.parent.conversationId, siblings);
  }
  const branches: { conversationId: string; parentConversationId?: string; at?: string }[] = [];
  const pending = [current];
  const seen = new Set<ConversationId>();
  while (pending.length) {
    const record = pending.pop()!;
    if (seen.has(record.id)) throw new Error("Conversation ancestry contains a cycle");
    seen.add(record.id);
    branches.push({ conversationId: String(record.id), ...(record.parent ? {
      parentConversationId: String(record.parent.conversationId), at: String(record.parent.at),
    } : {}) });
    const next = children.get(record.id) ?? [];
    for (let i = next.length - 1; i >= 0; i--) pending.push(next[i]);
  }
  return { rootConversationId: String(root), branches };
}

/** Scan on the session line so all pages describe the same committed tree. */
export async function discoverHistoryTree(harness: Harness, selected: ConversationId) {
  return harness.commit(async (tx) => {
    const records: ConversationRecord[] = [];
    let cursor: Cursor | undefined;
    do {
      const page = await tx.scanConversations({ order: "ascending" }, 256, cursor);
      records.push(...page.items);
      cursor = page.next;
    } while (cursor);
    return historyTree(records, selected);
  }, context);
}
