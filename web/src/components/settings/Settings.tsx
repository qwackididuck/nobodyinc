"use client";

import { type ReactNode, useEffect, useState } from "react";

import { type ConfigProtection, getProject, listMCPServers, type MCPServer, setConfigProtection, setProjectPrompt } from "@/lib/api";
import { type Animations, saveAppearance, useAppearance } from "@/lib/appearance";
import { useLive } from "@/lib/live";
import { lookOf } from "@/lib/look";
import { isDirector } from "../chat/agents";
import { Sprite } from "../office/sprite";
import ui from "../ui/ui.module.css";
import styles from "./settings.module.css";

const SECTIONS = [
  { id: "project", label: "Project and daemon" },
  { id: "mcp", label: "MCP servers" },
  { id: "appearance", label: "Appearance" },
] as const;

type Section = (typeof SECTIONS)[number]["id"];

// Settings is a game-like options menu: the project prompt every agent
// reads, how the daemon runs (read-only, except what to do when a delivery
// changes .nobody/), the project's MCP servers (read-only), and how the
// office looks on this computer.
export function Settings() {
  const [section, setSection] = useState<Section>("project");

  return (
    <div className={ui.scroll}>
      <div className={styles.settings}>
        <nav className={styles.nav} aria-label="Settings sections">
          {SECTIONS.map((s) => (
            <button key={s.id} type="button" aria-current={section === s.id} onClick={() => setSection(s.id)}>
              {s.label}
            </button>
          ))}
        </nav>
        {section === "project" && <ProjectSection />}
        {section === "mcp" && <MCPSection />}
        {section === "appearance" && <AppearanceSection />}
      </div>
    </div>
  );
}

function Option({ label, help, children }: { label: string; help?: string; children: ReactNode }) {
  return (
    <div className={styles.option}>
      <div className={styles.optionLabel}>
        <strong>{label}</strong>
        {help && <span>{help}</span>}
      </div>
      <div className={styles.optionValue}>{children}</div>
    </div>
  );
}

function Segment<T extends string>({ label, values, current, onPick }: { label: string; values: { value: T; label: string }[]; current: T; onPick: (v: T) => void }) {
  return (
    <div className={ui.seg} role="group" aria-label={label}>
      {values.map((v) => (
        <button key={v.value} type="button" aria-pressed={current === v.value} onClick={() => onPick(v.value)}>
          {v.label}
        </button>
      ))}
    </div>
  );
}

function ProjectSection() {
  const { settings, reload } = useLive();
  const [error, setError] = useState<string | null>(null);

  if (!settings) return <p className={ui.sub}>Loading…</p>;

  async function protect(value: ConfigProtection) {
    try {
      await setConfigProtection(value);
      setError(null);
      reload("settings");
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  }

  return (
    <section className={styles.section}>
      <h2 className={ui.title}>Project and daemon</h2>
      <p className={ui.sub}>What this office runs on. Except the first two options, these are set when the daemon starts.</p>
      <ProjectPrompt />
      <Option
        label="When an agent's commits change .nobody/"
        help="Agents cannot edit their own configuration; a shell command could. Block refuses the delivery until the agent undoes it; Warn delivers and warns you. Saved in ~/.nobody/settings.yaml, applies at once."
      >
        <Segment
          label="Configuration protection"
          values={[
            { value: "block", label: "Block" },
            { value: "warn", label: "Warn" },
          ]}
          current={settings.config_protection}
          onPick={(v) => void protect(v)}
        />
      </Option>
      {error && <p className={ui.error}>{error}</p>}
      <Option label="Project folder" help="The folder holding .nobody/. Agents work here, or in their team's worktree while a feature is active.">
        <code>{settings.project_dir}</code>
      </Option>
      <Option label="Data on this machine" help="Database and worktrees of this project. Never committed, never shared.">
        <code>{settings.state_dir}</code>
      </Option>
      <Option label="Daemon address" help="NOBODY_ADDR. Loopback only, unless you change it.">
        <code>{settings.address}</code>
      </Option>
      <Option label="Allowed origins" help="Pages allowed to change things through the API (-origins).">
        <span className={styles.chips}>
          {settings.origins.map((o) => (
            <span key={o} className={ui.chip}>
              {o}
            </span>
          ))}
        </span>
      </Option>
      <Option label="Start every agent with the daemon" help="-start">
        <span className={ui.chip}>{settings.start_agents ? "Yes" : "No"}</span>
      </Option>
      <Option label="Claude Code executable" help="-claude">
        <code>{settings.claude_path}</code>
      </Option>
    </section>
  );
}

// ProjectPrompt edits .nobody/project.md: what every agent of the project
// should know. Saving restarts every agent with a new session once idle.
function ProjectPrompt() {
  const [saved, setSaved] = useState<string | null>(null);
  const [text, setText] = useState("");
  const [busy, setBusy] = useState(false);
  const [note, setNote] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    getProject()
      .then((p) => {
        if (cancelled) return;
        setSaved(p.prompt);
        setText(p.prompt);
      })
      .catch((e: unknown) => !cancelled && setError(e instanceof Error ? e.message : String(e)));
    return () => {
      cancelled = true;
    };
  }, []);

  async function save() {
    setBusy(true);
    try {
      const p = await setProjectPrompt(text);
      setSaved(p.prompt);
      setText(p.prompt);
      setError(null);
      setNote("Saved. Every agent restarts with a new session once its turn is over.");
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className={styles.block}>
      <div className={styles.optionLabel}>
        <strong>Project prompt</strong>
        <span>
          What every agent should know about this project: the stack, the conventions, where things are. It comes after your own CLAUDE.md and before each
          team&apos;s instructions. Saved in .nobody/project.md.
        </span>
      </div>
      <textarea
        id="project-prompt"
        className={`${ui.textarea} ${styles.projectPrompt}`}
        placeholder={"e.g. A Go CLI with a Taskfile. Run `task check` before closing a ticket. Conventional commits."}
        value={text}
        disabled={saved === null}
        onChange={(e) => {
          setText(e.target.value);
          setNote(null);
        }}
      />
      <div className={styles.blockActions}>
        <button type="button" className={ui.btnPrimary} disabled={busy || saved === null || text === saved} onClick={() => void save()}>
          Save
        </button>
        {saved !== null && text !== saved && (
          <button type="button" className={ui.link} onClick={() => setText(saved)}>
            Undo changes
          </button>
        )}
        {note && <span className={ui.note}>{note}</span>}
      </div>
      {error && <p className={ui.error}>{error}</p>}
    </div>
  );
}

function MCPSection() {
  const { agents } = useLive();
  const [servers, setServers] = useState<MCPServer[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const byAddress = new Map(agents.map((a) => [a.address, a]));

  useEffect(() => {
    let cancelled = false;
    listMCPServers()
      .then((s) => !cancelled && setServers(s))
      .catch((e: unknown) => !cancelled && setError(e instanceof Error ? e.message : String(e)));
    return () => {
      cancelled = true;
    };
  }, []);

  return (
    <section className={styles.section}>
      <h2 className={ui.title}>MCP servers</h2>
      <p className={ui.sub}>The servers of .nobody/mcp.yaml. Each agent only gets the ones listed in its file. Edit the file to change them.</p>
      {error && <p className={ui.error}>{error}</p>}
      <div className={styles.servers}>
        {servers?.map((s) => (
          <article key={s.name} className={styles.server}>
            <div className={styles.serverName}>
              {s.name} <span className={styles.serverType}>{s.type}</span>
            </div>
            <code className={styles.serverTarget}>{s.target}</code>
            {s.env.length > 0 && (
              <div className={styles.env}>
                {s.env.map((v) => (
                  <span key={v.name} className={v.set ? styles.envSet : styles.envMissing}>
                    {v.set ? "✓" : "✗"} {v.name} {v.set ? "set" : "missing from the daemon's environment"}
                  </span>
                ))}
              </div>
            )}
            <div className={styles.users}>
              {s.used_by.length === 0 ? (
                "Given to no agent yet"
              ) : (
                <>
                  Used by
                  {s.used_by.map((u) => {
                    const a = byAddress.get(u);
                    return (
                      <span key={u} className={styles.user}>
                        <span className={ui.mini}>
                          <Sprite look={lookOf(u, a ? isDirector(a) : false)} />
                        </span>
                        {u}
                      </span>
                    );
                  })}
                </>
              )}
            </div>
          </article>
        ))}
        <article className={`${styles.server} ${styles.locked}`}>
          <div className={styles.serverName}>
            nobody <span className={styles.serverType}>built in</span>
          </div>
          <p className={ui.sub}>The office: mailboxes, tickets, and the director&apos;s tools. Every agent gets it.</p>
        </article>
        {servers?.length === 0 && <p className={ui.sub}>This project declares no MCP server.</p>}
      </div>
      <p className={styles.foot}>.nobody/mcp.yaml is committed with the project: secrets stay in environment variables, written as ${"{NAME}"}.</p>
    </section>
  );
}

function AppearanceSection() {
  const appearance = useAppearance();

  return (
    <section className={styles.section}>
      <h2 className={ui.title}>Appearance</h2>
      <p className={ui.sub}>How the office looks on this computer. When your system asks for less motion, Full behaves like Reduced.</p>
      <Option label="Animations" help="Reduced keeps statuses and paper planes on straight paths, without walking, loops or confetti.">
        <Segment<Animations>
          label="Animations"
          values={[
            { value: "full", label: "Full" },
            { value: "reduced", label: "Reduced" },
            { value: "off", label: "Off" },
          ]}
          current={appearance.animations}
          onPick={(v) => saveAppearance({ ...appearance, animations: v })}
        />
      </Option>
      <Option label="Labels on paper planes" help="Who writes to whom, and the subject.">
        <button
          type="button"
          role="switch"
          className={ui.switch}
          aria-checked={appearance.planeLabels}
          aria-label="Labels on paper planes"
          onClick={() => saveAppearance({ ...appearance, planeLabels: !appearance.planeLabels })}
        />
      </Option>
    </section>
  );
}
