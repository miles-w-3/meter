import { randomUUID } from "node:crypto";
import { DatabaseSync } from "node:sqlite";
import type { z } from "zod";
import { layout } from "../contracts/schemas.ts";

type Layout = z.infer<typeof layout>;

export class LayoutConflict extends Error {}
export class InvalidLayout extends Error {}

function conversationIDs(value: Layout) {
  const conversations = new Set<string>();
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
      if (node.conversationId || node.empty) {
        if (split.some((part) => part !== undefined) || (node.conversationId && (node.empty || node.cwd))) {
          throw new InvalidLayout("Invalid pane fields");
        }
        if (node.conversationId) {
          if (conversations.has(node.conversationId) && tab.projectId) throw new InvalidLayout("Conversation is already open");
          conversations.add(node.conversationId);
        }
        return;
      }
      if (node.cwd || node.name || node.widthShare !== undefined || !node.axis || node.ratio === undefined || !node.first || !node.second) throw new InvalidLayout("Split is incomplete");
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
      CREATE TABLE IF NOT EXISTS projects (
        id TEXT PRIMARY KEY, cwd TEXT NOT NULL UNIQUE, name TEXT NOT NULL
      ) STRICT;
      CREATE TABLE IF NOT EXISTS conversation_projects (
        conversation_id TEXT PRIMARY KEY, project_id TEXT NOT NULL REFERENCES projects(id)
      ) STRICT;
      CREATE TABLE IF NOT EXISTS layouts (
        id TEXT PRIMARY KEY, revision INTEGER NOT NULL, body TEXT NOT NULL
      ) STRICT;
    `);
  }

  projects() {
    return this.db.prepare("SELECT id, cwd, name FROM projects ORDER BY name, cwd").all();
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
    if (this.get(id).revision !== current.revision) throw new LayoutConflict("Workspace changed. Reload layout before editing.");
    const active = current.tabs.find((tab) => tab.id === current.activeTab);
    if (active?.projectId && active.projectId !== current.activeProject) throw new InvalidLayout("Active tab belongs to another project");
    const projects = new Set(current.tabs.map((tab) => tab.projectId));
    projects.add(current.activeProject);
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
