"use client";

import { useEffect, useState } from "react";

import { type AgentDefinition, listMCPServers, PROFILES } from "@/lib/api";
import ui from "../ui/ui.module.css";
import styles from "./recruitment.module.css";

// The tools most agents are given; others can be typed.
const COMMON_TOOLS = ["Read", "Edit", "Write", "Bash", "Grep", "Glob", "WebFetch", "WebSearch"];
const MODELS = ["sonnet", "opus", "haiku"];

interface Props {
  value: AgentDefinition;
  onChange: (value: AgentDefinition) => void;
  // Templates also have a description and a version.
  template?: boolean;
}

// DefinitionFields edits the fields of an agent or template file: role,
// expertise, model, permission profile and rules, tools, MCP servers, skills and the
// instructions (docs/agents.md).
export function DefinitionFields({ value, onChange, template = false }: Props) {
  const [servers, setServers] = useState<string[]>([]);
  const set = (patch: Partial<AgentDefinition>) => onChange({ ...value, ...patch });
  const setPermissions = (patch: NonNullable<AgentDefinition["permissions"]>) => set({ permissions: { ...value.permissions, ...patch } });

  useEffect(() => {
    let cancelled = false;
    listMCPServers()
      .then((s) => !cancelled && setServers(s.map((x) => x.name)))
      .catch(() => {
        // Without the registry, MCP servers simply cannot be picked.
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const tools = value.tools ?? [];
  const extraTools = tools.filter((t) => !COMMON_TOOLS.includes(t));
  const toggle = (list: string[] | undefined, item: string) => (list ?? []).includes(item) ? (list ?? []).filter((x) => x !== item) : [...(list ?? []), item];

  return (
    <div className={styles.fields}>
      {template && (
        <>
          <label className={`${ui.label} ${styles.wide}`}>
            DESCRIPTION, SHOWN IN THE CATALOG
            <input id="def-description" className={ui.input} value={value.description ?? ""} onChange={(e) => set({ description: e.target.value })} />
          </label>
          <label className={ui.label}>
            VERSION
            <input
              id="def-version"
              type="number"
              min={1}
              className={ui.input}
              value={value.version ?? 1}
              onChange={(e) => set({ version: Number(e.target.value) })}
            />
          </label>
        </>
      )}
      <label className={ui.label}>
        ROLE
        <input id="def-role" className={ui.input} placeholder="One line, shown to colleagues" value={value.role ?? ""} onChange={(e) => set({ role: e.target.value })} />
      </label>
      <label className={ui.label}>
        MODEL
        <input id="def-model" className={ui.input} list="def-models" placeholder="Claude Code's default" value={value.model ?? ""} onChange={(e) => set({ model: e.target.value })} />
        <datalist id="def-models">
          {MODELS.map((m) => (
            <option key={m} value={m} />
          ))}
        </datalist>
      </label>
      <label className={`${ui.label} ${styles.wide}`}>
        EXPERTISE
        <input
          id="def-expertise"
          className={ui.input}
          placeholder="What colleagues should ask about, and what this agent decides"
          value={value.expertise ?? ""}
          onChange={(e) => set({ expertise: e.target.value })}
        />
        <span className={styles.hint}>Public: shown next to the name in every colleague&apos;s organization chart, so that questions reach the right agent.</span>
      </label>
      <div className={`${ui.label} ${styles.wide}`}>
        PERMISSIONS
        <div className={ui.seg} role="group" aria-label="Permission profile">
          {PROFILES.map((p) => (
            <button key={p} type="button" aria-pressed={(value.permissions?.profile ?? "default") === p} onClick={() => setPermissions({ profile: p })}>
              {p}
            </button>
          ))}
        </div>
        <span className={styles.hint}>
          default: every sensitive action asks you · allowlist: edits, commits and tests pass, the rest asks · auto: Claude Code decides (not with Haiku)
        </span>
      </div>
      <div className={`${ui.label} ${styles.wide}`}>
        TOOLS WITHOUT ASKING
        <div className={styles.chips}>
          {[...COMMON_TOOLS, ...extraTools].map((t) => (
            <button key={t} type="button" className={`${ui.chip} ${tools.includes(t) ? ui.chipOn : ""}`} aria-pressed={tools.includes(t)} onClick={() => set({ tools: toggle(tools, t) })}>
              {t}
            </button>
          ))}
        </div>
      </div>
      <div className={`${ui.label} ${styles.wide}`}>
        MCP SERVERS (FROM .NOBODY/MCP.YAML)
        <div className={styles.chips}>
          {servers.length === 0 && <span className={styles.hint}>No server declared in this project.</span>}
          {servers.map((s) => {
            const on = (value.mcp ?? []).includes(s);
            return (
              <button key={s} type="button" className={`${ui.chip} ${on ? ui.chipOn : ""}`} aria-pressed={on} onClick={() => set({ mcp: toggle(value.mcp, s) })}>
                {s}
              </button>
            );
          })}
        </div>
      </div>
      <label className={ui.label}>
        ALLOW RULES, ONE PER LINE
        <textarea
          id="def-allow"
          className={ui.textarea}
          placeholder="Bash(go test *)"
          value={(value.permissions?.allow ?? []).join("\n")}
          onChange={(e) => setPermissions({ allow: e.target.value.split("\n") })}
        />
      </label>
      <label className={ui.label}>
        DENY RULES, ONE PER LINE
        <textarea
          id="def-deny"
          className={ui.textarea}
          placeholder="Read(./.env)"
          value={(value.permissions?.deny ?? []).join("\n")}
          onChange={(e) => setPermissions({ deny: e.target.value.split("\n") })}
        />
      </label>
      <label className={`${ui.label} ${styles.wide}`}>
        INSTRUCTIONS
        <textarea id="def-prompt" className={`${ui.textarea} ${styles.prompt}`} value={value.prompt ?? ""} onChange={(e) => set({ prompt: e.target.value })} />
      </label>
    </div>
  );
}
