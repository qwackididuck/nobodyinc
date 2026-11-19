"use client";

import { type FormEvent, useState } from "react";

import { importSubagents, type PermissionProfile, PROFILES, type Subagent } from "@/lib/api";
import { useLive } from "@/lib/live";
import { Modal } from "../ui/Modal";
import ui from "../ui/ui.module.css";
import styles from "./staff.module.css";

// pendingSubagents are the project's subagents offered to the human: readable,
// neither imported nor dismissed (ADR-0018).
export function pendingSubagents(all: Subagent[]): Subagent[] {
  return all.filter((s) => s.status === "pending" && !s.error);
}

// What a row's selector says: nothing chosen yet (the subagent stays
// pending), not imported (dismissed), an existing team, or a new team,
// declared on the row named after "new:".
type Choice = "" | "skip" | `team:${string}` | `new:${string}`;

interface Row {
  choice: Choice;
  // The agent's name.
  name: string;
  profile: PermissionProfile;
  // The new team this row declares, when its choice is new:<its subagent>.
  teamID: string;
  teamGoal: string;
}

interface Props {
  onClose: () => void;
}

// SubagentImport is the window that turns the project's Claude Code
// subagents (.claude/agents) into agents (ADR-0018): one row per pending
// subagent, with the team it joins (an existing one, a new one declared on
// the row, or none: not imported). "Create" applies every choice at once,
// all or nothing; the new agents start right away. "Later" writes nothing.
export function SubagentImport({ onClose }: Props) {
  const { subagents, teams, reload } = useLive();
  const pending = pendingSubagents(subagents);
  const broken = subagents.filter((s) => s.error);

  const [rows, setRows] = useState<Record<string, Row>>(() =>
    Object.fromEntries(
      pending.map((s) => [
        s.name,
        { choice: "", name: s.agent_name ?? "", profile: (s.agent?.permissions?.profile as PermissionProfile) || "default", teamID: "", teamGoal: "" },
      ]),
    ),
  );
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const row = (name: string): Row => rows[name] ?? { choice: "", name: "", profile: "default", teamID: "", teamGoal: "" };
  const change = (name: string, patch: Partial<Row>) => setRows((r) => ({ ...r, [name]: { ...row(name), ...patch } }));

  // The new teams, declared by the rows that chose "+ New team…".
  const newTeams = pending.filter((s) => row(s.name).choice === `new:${s.name}`).map((s) => ({ owner: s.name, id: row(s.name).teamID.trim(), goal: row(s.name).teamGoal.trim() }));
  const teamOf = (choice: Choice): string => {
    if (choice.startsWith("team:")) return choice.slice("team:".length);
    if (choice.startsWith("new:")) return newTeams.find((t) => `new:${t.owner}` === choice)?.id ?? "";
    return "";
  };

  const imported = pending.filter((s) => row(s.name).choice.startsWith("team:") || row(s.name).choice.startsWith("new:"));
  const dismissed = pending.filter((s) => row(s.name).choice === "skip");
  const nothing = imported.length === 0 && dismissed.length === 0;

  async function submit(e: FormEvent) {
    e.preventDefault();
    e.stopPropagation();
    setBusy(true);
    try {
      await importSubagents({
        teams: newTeams.map((t) => ({ id: t.id, goal: t.goal })),
        agents: imported.map((s) => ({ subagent: s.name, team: teamOf(row(s.name).choice), name: row(s.name).name.trim(), profile: row(s.name).profile })),
        ignore: dismissed.map((s) => s.name),
      });
      reload("subagents");
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal label="Subagents found in .claude/agents" size="wide" onClose={onClose}>
      <form className={styles.importer} onSubmit={(e) => void submit(e)}>
        <h2 className={ui.title}>Subagents found in .claude/agents ({pending.length})</h2>
        <p className={ui.sub}>
          Make them agents of your teams: each becomes a copy in .nobody/, and keeps working as a Claude Code subagent. Fix their role and expertise in Staff
          afterwards. A subagent left on &quot;Choose…&quot; is offered again later.
        </p>

        <div className={styles.importRows}>
          {pending.map((s) => (
            <ImportRow
              key={s.name}
              subagent={s}
              row={row(s.name)}
              teams={teams.map((t) => t.id)}
              newTeams={newTeams.filter((t) => t.owner !== s.name)}
              onChange={(patch) => change(s.name, patch)}
            />
          ))}
        </div>

        {broken.length > 0 && (
          <details className={styles.importBroken}>
            <summary className={styles.hint}>{broken.length} file(s) cannot be imported</summary>
            {broken.map((s) => (
              <p key={s.file} className={styles.hint}>
                <code>{s.file}</code>: {s.error}
              </p>
            ))}
          </details>
        )}

        {error && <p className={ui.error}>{error}</p>}

        <div className={styles.importActions}>
          <button type="button" className={ui.link} onClick={onClose}>
            Later
          </button>
          <button type="submit" className={ui.btnPrimary} disabled={busy || nothing}>
            {busy ? "Creating…" : summary(newTeams.length, imported.length, dismissed.length)}
          </button>
        </div>
      </form>
    </Modal>
  );
}

// summary is the Create button's label: what it will do, new agents first,
// as each starts and costs its onboarding turn.
function summary(teams: number, agents: number, dismissed: number): string {
  const parts: string[] = [];
  if (teams) parts.push(`${teams} team${teams > 1 ? "s" : ""}`);
  if (agents) parts.push(`${agents} agent${agents > 1 ? "s" : ""}`);
  const create = parts.length ? `Create ${parts.join(", ")}` : "";
  const dismiss = dismissed ? `dismiss ${dismissed}` : "";
  return [create, dismiss].filter(Boolean).join(", ") || "Create";
}

interface RowProps {
  subagent: Subagent;
  row: Row;
  // The existing teams, and the new teams declared on other rows.
  teams: string[];
  newTeams: { owner: string; id: string }[];
  onChange: (patch: Partial<Row>) => void;
}

// ImportRow is one subagent: what it is, the team it joins, and, unfolded,
// its agent's name, profile, and what is not carried over.
function ImportRow({ subagent, row, teams, newTeams, onChange }: RowProps) {
  const joining = row.choice !== "" && row.choice !== "skip";
  const ownsTeam = row.choice === `new:${subagent.name}`;
  const lost = [subagent.dropped_tools && `tools: ${subagent.dropped_tools}`, ...(subagent.ignored_keys ?? [])].filter(Boolean);

  return (
    <div className={styles.importRow} data-choice={row.choice === "skip" ? "skip" : joining ? "join" : "none"}>
      <div className={styles.importWho}>
        <strong>{subagent.name}</strong>
        {subagent.agent?.model && <span className={styles.from}>{subagent.agent.model}</span>}
        <span className={styles.importDesc} title={subagent.description}>
          {subagent.description}
        </span>
      </div>
      <select className={ui.select} value={row.choice} onChange={(e) => onChange({ choice: e.target.value as Choice })} aria-label={`Team for ${subagent.name}`}>
        <option value="">Choose…</option>
        {teams.map((t) => (
          <option key={t} value={`team:${t}`}>
            {t}
          </option>
        ))}
        {newTeams
          .filter((t) => t.id)
          .map((t) => (
            <option key={t.owner} value={`new:${t.owner}`}>
              {t.id} (new)
            </option>
          ))}
        <option value={`new:${subagent.name}`}>+ New team…</option>
        <option value="skip">Don&apos;t import</option>
      </select>

      {ownsTeam && (
        <div className={styles.importTeam}>
          <label className={ui.label}>
            TEAM ID
            <input className={ui.input} value={row.teamID} onChange={(e) => onChange({ teamID: e.target.value })} placeholder="e.g. backend" pattern="[a-z][a-z0-9\-]*" required />
          </label>
          <label className={ui.label}>
            GOAL
            <input className={ui.input} value={row.teamGoal} onChange={(e) => onChange({ teamGoal: e.target.value })} placeholder="What the team is for" />
          </label>
        </div>
      )}

      {joining && (
        <div className={styles.importTeam}>
          <label className={ui.label}>
            AGENT NAME
            <input className={ui.input} value={row.name} onChange={(e) => onChange({ name: e.target.value })} pattern="[a-z][a-z0-9\-]*" required />
          </label>
          <label className={ui.label}>
            PROFILE
            <select className={ui.select} value={row.profile} onChange={(e) => onChange({ profile: e.target.value as PermissionProfile })}>
              {PROFILES.map((p) => (
                <option key={p} value={p}>
                  {p}
                </option>
              ))}
            </select>
          </label>
        </div>
      )}

      {joining && lost.length > 0 && (
        <p className={styles.importLost}>
          Not carried over: {lost.join(" · ")}.{" "}
          {subagent.dropped_tools && "As a subagent its tools were a restriction; an agent's tools are allowed without asking: set them in Staff if needed."}
        </p>
      )}
    </div>
  );
}
