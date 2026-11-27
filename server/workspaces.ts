import { randomUUID } from "node:crypto";
import { DatabaseSync } from "node:sqlite";
import type { z } from "zod";
import { defaultSettings, layout, projectRecord, providerSessions, toolSettings, toolSettingsChange } from "../contracts/schemas.ts";

type Layout = z.infer<typeof layout>;

export class LayoutConflict extends Error {}
export class InvalidLayout extends Error {}

function conversationIDs(value: Layout) {
  const conversations = new Set<string>();
  const liveAttachments = new Set<string>();
  const allIDs = new Set<string>();
  const unique = (id: string) => {
    if (allIDs.has(id)) throw new InvalidLayout("Duplicate tab or pane ID");
    allIDs.add(id);
  };
  if (value.activeTab && !value.tabs.some((tab) => tab.id === value.activeTab)) {
    throw new InvalidLayout("Active tab is invalid");
  }
  for (const tab of value.tabs) {
    unique(tab.id);
    const nodes = new Map(tab.nodes.map((node) => [node.id, node]));
    for (const node of tab.nodes) unique(node.id);
    const visited = new Set<string>();
    function walk(id: string, depth: number) {
      const node = nodes.get(id);
      if (!node || visited.has(id) || depth > 8) throw new InvalidLayout("Invalid split tree");
      visited.add(id);
      const split = [node.axis, node.ratio, node.first, node.second];
      if (node.conversationId || node.empty || node.clientManaged) {
        if (split.some((part) => part !== undefined) ||
            (node.conversationId && (node.empty || node.clientManaged || node.cwd)) ||
            (node.clientManaged && (node.empty || node.treePosition || node.pendingFork || node.resumeEntryId)) ||
            (node.treePosition && !node.conversationId) ||
            (node.pendingFork && (!node.treePosition || (!node.pendingFork.content.trim() && !node.pendingFork.images?.length)))) {
          throw new InvalidLayout("Invalid pane fields");
        }
        if (node.conversationId) {
          if (!node.treePosition) {
            if (liveAttachments.has(node.conversationId) && tab.projectId) throw new InvalidLayout("Conversation is already open");
            liveAttachments.add(node.conversationId);
          }
          conversations.add(node.conversationId);
          if (node.treePosition) conversations.add(node.treePosition.conversationId);
        }
        return;
      }
      if (node.cwd || node.name || node.treePosition || node.pendingFork || node.widthShare !== undefined || !node.axis || node.ratio === undefined || !node.first || !node.second) throw new InvalidLayout("Split is incomplete");
      walk(node.first, depth + 1);
      walk(node.second, depth + 1);
    }
    walk(tab.root, 0);
    if (visited.size !== nodes.size) throw new InvalidLayout("Unreachable pane");
  }
  return conversations;
}

export class Workspaces {
  readonly db: DatabaseSync;

  constructor(file: string) {
    this.db = new DatabaseSync(file);
    this.db.exec(`
      CREATE TABLE IF NOT EXISTS settings (
        id INTEGER PRIMARY KEY CHECK (id = 1), body TEXT NOT NULL
      ) STRICT;
      CREATE TABLE IF NOT EXISTS tool_settings (
        id INTEGER PRIMARY KEY CHECK (id = 1), body TEXT NOT NULL
      ) STRICT;
      CREATE TABLE IF NOT EXISTS provider_settings (
        id TEXT PRIMARY KEY, enabled INTEGER NOT NULL CHECK (enabled IN (0, 1))
      ) STRICT;
      CREATE TABLE IF NOT EXISTS provider_sessions (
        id TEXT PRIMARY KEY, body TEXT NOT NULL
      ) STRICT;
      CREATE TABLE IF NOT EXISTS projects (
        id TEXT PRIMARY KEY, cwd TEXT NOT NULL UNIQUE, name TEXT NOT NULL
      ) STRICT;
      CREATE TABLE IF NOT EXISTS conversation_titles (
        conversation_id TEXT PRIMARY KEY, title TEXT NOT NULL
      ) STRICT;
      CREATE TABLE IF NOT EXISTS conversation_projects (
        conversation_id TEXT PRIMARY KEY, project_id TEXT NOT NULL REFERENCES projects(id)
      ) STRICT;
      CREATE TABLE IF NOT EXISTS layouts (
        id TEXT PRIMARY KEY, revision INTEGER NOT NULL, body TEXT NOT NULL
      ) STRICT;
    `);
  }

  defaults() {
    const row = this.db.prepare("SELECT body FROM settings WHERE id=1").get();
    return row ? defaultSettings.parse(JSON.parse(String(row.body))) : undefined;
  }

  saveDefaults(value: z.infer<typeof defaultSettings>) {
    this.db.prepare("INSERT INTO settings VALUES (1, ?) ON CONFLICT(id) DO UPDATE SET body=excluded.body").run(JSON.stringify(value));
  }

  toolSettings() {
    const row = this.db.prepare("SELECT body FROM tool_settings WHERE id=1").get();
    return toolSettings.parse(row ? JSON.parse(String(row.body)) : {
      webSearch: { enabled: true, providers: { brave: true, exa: true, firecrawl: true } },
    });
  }

  updateToolSettings({ webFetch, webSearch: change, showThinking, computedDiffs, diffLayout, diffInline }: z.infer<typeof toolSettingsChange>) {
    const value = this.toolSettings();
    if (webFetch?.enabled !== undefined) value.webFetch.enabled = webFetch.enabled;
    if (change?.enabled !== undefined) value.webSearch.enabled = change.enabled;
    if (change?.provider) value.webSearch.providers[change.provider.id] = change.provider.enabled;
    if (showThinking !== undefined) value.showThinking = showThinking;
    if (computedDiffs !== undefined) value.computedDiffs = computedDiffs;
    if (diffLayout !== undefined) value.diffLayout = diffLayout;
    if (diffInline !== undefined) value.diffInline = diffInline;
    this.db.prepare("INSERT INTO tool_settings VALUES (1, ?) ON CONFLICT(id) DO UPDATE SET body=excluded.body").run(JSON.stringify(value));
  }

  providerEnabled(id: string): boolean {
    const row = this.db.prepare("SELECT enabled FROM provider_settings WHERE id=?").get(id);
    return row?.enabled === 1;
  }

  setProviderEnabled(id: string, enabled: boolean) {
    this.db.prepare("INSERT INTO provider_settings VALUES (?, ?) ON CONFLICT(id) DO UPDATE SET enabled=excluded.enabled").run(id, Number(enabled));
  }

  /** Saved session limits, or undefined for the provider's defaults. */
  providerSessions(id: string): z.infer<typeof providerSessions> | undefined {
    const row = this.db.prepare("SELECT body FROM provider_sessions WHERE id=?").get(id);
    if (!row) return undefined;
    try {
      const parsed = providerSessions.safeParse(JSON.parse(String(row.body)));
      return parsed.success ? parsed.data : undefined;
    } catch {
      return undefined;
    }
  }

  setProviderSessions(id: string, value: z.infer<typeof providerSessions>) {
    this.db.prepare("INSERT INTO provider_sessions VALUES (?, ?) ON CONFLICT(id) DO UPDATE SET body=excluded.body").run(id, JSON.stringify(value));
  }

  projects() {
    const rows = this.db.prepare("SELECT id, cwd, name FROM projects ORDER BY name, cwd").all();
    return projectRecord.array().parse(rows);
  }

  projectExists(id: string) {
    return !!this.db.prepare("SELECT 1 FROM projects WHERE id=?").get(id);
  }

  projectFor(conversation: string, cwd: string): string | undefined {
    const found = this.db.prepare(`
      SELECT project_id AS id FROM conversation_projects WHERE conversation_id=?
      UNION ALL SELECT id FROM projects WHERE cwd=? LIMIT 1
    `).get(conversation, cwd);
    return found ? String(found.id) : undefined;
  }

  conversationTitle(conversation: string): string | undefined {
    const row = this.db.prepare("SELECT title FROM conversation_titles WHERE conversation_id=?").get(conversation);
    return row ? String(row.title) : undefined;
  }

  renameConversation(conversation: string, title: string) {
    this.db.prepare("INSERT INTO conversation_titles VALUES (?, ?) ON CONFLICT(conversation_id) DO UPDATE SET title=excluded.title").run(conversation, title);
  }

  associate(conversation: string, project: string) {
    this.db.prepare("INSERT OR IGNORE INTO conversation_projects VALUES (?, ?)").run(conversation, project);
  }

  register(cwd: string, name: string) {
    return this.db.prepare(`
      INSERT INTO projects VALUES (?, ?, ?) ON CONFLICT(cwd) DO NOTHING
      RETURNING id, cwd, name
    `).get(randomUUID(), cwd, name);
  }

  renameProject(id: string, name: string) {
    return this.db.prepare("UPDATE projects SET name=? WHERE id=? RETURNING id, cwd, name").get(name, id);
  }

  get(id: string): Layout {
    const row = this.db.prepare("SELECT body FROM layouts WHERE id=?").get(id);
    return row ? layout.parse(JSON.parse(String(row.body))) : { revision: 0, tabs: [] };
  }

  async save(id: string, current: Layout, verify: (id: string) => Promise<unknown>): Promise<Layout> {
    const previous = this.get(id);
    if (previous.revision !== current.revision) throw new LayoutConflict("Workspace changed. Reload layout before editing.");
    // Preserve legacy duplicate names, but never allow a save to introduce another.
    const counts = (value: Layout) => {
      const result = new Map<string, number>();
      for (const tab of value.tabs) {
        if (!tab.projectId) continue;
        const key = tab.projectId + "\0" + (tab.name ?? "Main").trim().toLowerCase();
        result.set(key, (result.get(key) ?? 0) + 1);
      }
      return result;
    };
    const before = counts(previous);
    for (const [key, count] of counts(current)) {
      if (count > 1 && count > (before.get(key) ?? 0)) throw new InvalidLayout("Tab names must be unique within a project");
    }
    const active = current.tabs.find((tab) => tab.id === current.activeTab);
    if (active?.projectId && active.projectId !== current.activeProject) throw new InvalidLayout("Active tab belongs to another project");
    const projects = new Set(current.tabs.map((tab) => tab.projectId));
    projects.add(current.activeProject);
    for (const id of current.closedProjects ?? []) projects.add(id);
    if (current.activeProject && current.closedProjects?.includes(current.activeProject)) throw new InvalidLayout("Active project is closed");
    for (const id of projects) {
      if (id && !this.projectExists(id)) throw new InvalidLayout("Unknown project");
    }
    for (const id of conversationIDs(current)) await verify(id);
    const next = { ...current, revision: current.revision + 1 };
    const result = this.db.prepare(`
      INSERT INTO layouts (id, revision, body)
      SELECT ?, ?, ? WHERE ? = 0 OR EXISTS(SELECT 1 FROM layouts WHERE id = ?)
      ON CONFLICT(id) DO UPDATE SET revision=excluded.revision, body=excluded.body WHERE layouts.revision=?
    `).run(id, next.revision, JSON.stringify(next), current.revision, id, current.revision);
    if (!result.changes) throw new LayoutConflict("Workspace changed. Reload layout before editing.");
    return next;
  }
}
