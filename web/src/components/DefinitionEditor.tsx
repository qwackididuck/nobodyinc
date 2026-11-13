"use client";

import { type AgentDefinition, PROFILES, type PermissionProfile } from "@/lib/api";
import styles from "./ControlRoom.module.css";

interface Props {
  value: AgentDefinition;
  onChange: (value: AgentDefinition) => void;
  // Templates also have a description and a version.
  template?: boolean;
}

// DefinitionEditor edits the fields of an agent or template file
// (docs/agents.md). Lists are typed as text: tools, MCP servers and skills
// separated by commas, permission rules one per line (they may hold commas).
export function DefinitionEditor({ value, onChange, template = false }: Props) {
  const set = (patch: Partial<AgentDefinition>) => onChange({ ...value, ...patch });
  const setPermissions = (patch: NonNullable<AgentDefinition["permissions"]>) =>
    set({ permissions: { ...value.permissions, ...patch } });

  return (
    <div className={styles.composer}>
      {template && (
        <div className={styles.row}>
          <input
            className={styles.grow}
            placeholder="Description shown in the catalog"
            value={value.description ?? ""}
            onChange={(e) => set({ description: e.target.value })}
          />
          <label className={styles.checkbox}>
            version
            <input
              type="number"
              min={1}
              className={styles.narrow}
              value={value.version ?? 1}
              onChange={(e) => set({ version: Number(e.target.value) })}
            />
          </label>
        </div>
      )}
      <div className={styles.row}>
        <input className={styles.grow} placeholder="Role, e.g. Go developer" value={value.role ?? ""} onChange={(e) => set({ role: e.target.value })} />
        <input placeholder="Model (sonnet, opus, haiku…)" value={value.model ?? ""} onChange={(e) => set({ model: e.target.value })} />
        <select
          aria-label="Permission profile"
          value={value.permissions?.profile ?? "default"}
          onChange={(e) => setPermissions({ profile: e.target.value as PermissionProfile })}
        >
          {PROFILES.map((p) => (
            <option key={p} value={p}>
              {p}
            </option>
          ))}
        </select>
      </div>
      <div className={styles.row}>
        <input
          className={styles.grow}
          placeholder="Tools allowed without asking: Read, Grep, …"
          value={joinList(value.tools)}
          onChange={(e) => set({ tools: splitList(e.target.value) })}
        />
        <input placeholder="MCP servers (from .nobody/mcp.yaml)" value={joinList(value.mcp)} onChange={(e) => set({ mcp: splitList(e.target.value) })} />
        <input placeholder="Skills" value={joinList(value.skills)} onChange={(e) => set({ skills: splitList(e.target.value) })} />
      </div>
      <div className={styles.row}>
        <textarea
          className={styles.grow}
          rows={3}
          placeholder={"Allow rules, one per line\nBash(go test *)"}
          value={joinLines(value.permissions?.allow)}
          onChange={(e) => setPermissions({ allow: splitLines(e.target.value) })}
        />
        <textarea
          className={styles.grow}
          rows={3}
          placeholder={"Deny rules, one per line\nRead(./.env)"}
          value={joinLines(value.permissions?.deny)}
          onChange={(e) => setPermissions({ deny: splitLines(e.target.value) })}
        />
      </div>
      <textarea rows={8} placeholder="Instructions (the agent's prompt)" value={value.prompt ?? ""} onChange={(e) => set({ prompt: e.target.value })} />
    </div>
  );
}

// emptyDefinition is the starting point of an agent written from scratch.
export function emptyDefinition(): AgentDefinition {
  return { permissions: { profile: "default" } };
}

const joinList = (items?: string[]) => (items ?? []).join(", ");
const joinLines = (items?: string[]) => (items ?? []).join("\n");

// While typing, a trailing comma or newline must survive, so empty entries
// are only dropped when the definition is sent (see cleanDefinition).
const splitList = (text: string) => text.split(",").map((s) => s.trimStart());
const splitLines = (text: string) => text.split("\n");

// cleanDefinition trims list entries and drops the empty ones before sending.
export function cleanDefinition(d: AgentDefinition): AgentDefinition {
  const clean = (items?: string[]) => (items ?? []).map((s) => s.trim()).filter(Boolean);
  return {
    ...d,
    tools: clean(d.tools),
    mcp: clean(d.mcp),
    skills: clean(d.skills),
    permissions: { ...d.permissions, allow: clean(d.permissions?.allow), deny: clean(d.permissions?.deny) },
  };
}
