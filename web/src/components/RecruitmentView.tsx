"use client";

import { type FormEvent, useState } from "react";

import {
  type Agent,
  type AgentDefinition,
  type Template,
  deleteTemplate,
  listTeams,
  listTemplates,
  recruitAgent,
  saveTemplate,
} from "@/lib/api";
import { usePolling } from "@/lib/usePolling";
import { AgentEditor } from "./AgentEditor";
import styles from "./ControlRoom.module.css";
import { DefinitionEditor, cleanDefinition, emptyDefinition } from "./DefinitionEditor";

// The catalog and teams change rarely: a slow refresh is enough.
const REFRESH_MS = 5000;

interface Props {
  // The agent selected in the team list, edited below the catalog.
  selected: Agent | null;
  onChanged: () => void;
}

// RecruitmentView is the "Recruitment" tab: the catalog of templates (recruit
// one into a team, or customize it as your own template), creating an agent
// from scratch, and editing the selected agent. Deliberately plain: the real
// design pass comes later.
export function RecruitmentView({ selected, onChanged }: Props) {
  const templates = usePolling(listTemplates, REFRESH_MS);
  const teams = usePolling(listTeams, REFRESH_MS);
  const [chosen, setChosen] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);

  const teamIDs = (teams.data ?? []).map((t) => t.id);
  const template = (templates.data ?? []).find((t) => t.name === chosen) ?? null;

  return (
    <div className={styles.tickets}>
      {selected && <AgentEditor key={selected.address} agent={selected} onChanged={onChanged} />}

      <h3 className={styles.panelTitle}>
        Catalog
        <button type="button" className={styles.linkButton} onClick={() => setCreating(!creating)}>
          {creating ? "Close" : "New agent from scratch"}
        </button>
      </h3>
      {templates.error && <p className={styles.error}>{templates.error}</p>}
      {creating && <CreateAgent teams={teamIDs} onCreated={() => { setCreating(false); onChanged(); }} />}

      <table className={styles.ticketTable}>
        <thead>
          <tr>
            <th>Template</th>
            <th>Role</th>
            <th>Description</th>
            <th>Profile</th>
            <th>Source</th>
          </tr>
        </thead>
        <tbody>
          {(templates.data ?? []).map((t) => (
            <tr key={t.name} className={t.name === chosen ? styles.ticketRowSelected : styles.ticketRow} onClick={() => setChosen(t.name)}>
              <td>
                {t.name}
                {t.version ? <span className={styles.muted}> v{t.version}</span> : null}
              </td>
              <td>{t.role}</td>
              <td>{t.description}</td>
              <td>{t.permissions?.profile ?? "default"}</td>
              <td>{t.builtin ? "built-in" : t.overrides_builtin ? "mine (replaces built-in)" : "mine"}</td>
            </tr>
          ))}
        </tbody>
      </table>
      <p className={styles.muted}>Your own templates are the .md files in ~/.nobody/catalog; one named like a built-in template replaces it.</p>

      {template && (
        <TemplateDetail
          key={template.name}
          template={template}
          teams={teamIDs}
          onRecruited={onChanged}
          onSaved={() => void templates.refresh()}
          onDeleted={() => {
            setChosen(null);
            void templates.refresh();
          }}
        />
      )}
    </div>
  );
}

// TemplateDetail recruits the chosen template into a team, or saves a
// customized copy as the user's own template.
function TemplateDetail({
  template,
  teams,
  onRecruited,
  onSaved,
  onDeleted,
}: {
  template: Template;
  teams: string[];
  onRecruited: () => void;
  onSaved: () => void;
  onDeleted: () => void;
}) {
  const [name, setName] = useState("");
  const [team, setTeam] = useState("");
  const [customizing, setCustomizing] = useState(false);
  const [copyName, setCopyName] = useState(template.name);
  const [definition, setDefinition] = useState<AgentDefinition>(toDefinition(template));
  const [notice, setNotice] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  async function act(action: () => Promise<unknown>, done: string, after: () => void) {
    try {
      await action();
      setError(null);
      setNotice(done);
      after();
    } catch (e) {
      setNotice(null);
      setError(e instanceof Error ? e.message : String(e));
    }
  }

  const targetTeam = team || teams[0] || "";

  function recruit(event: FormEvent) {
    event.preventDefault();
    void act(() => recruitAgent(targetTeam, { name, template: template.name }), `${name}@${targetTeam} recruited.`, () => {
      setName("");
      onRecruited();
    });
  }

  return (
    <article className={styles.ticketDetail}>
      <h3 className={styles.subject}>
        {template.name} <span className={styles.muted}>— {template.role}</span>
      </h3>
      {template.prompt && <p className={styles.body}>{template.prompt}</p>}

      <form className={styles.row} onSubmit={recruit}>
        <input placeholder="Agent name, e.g. dev2" value={name} onChange={(e) => setName(e.target.value)} />
        <span className={styles.muted}>in</span>
        <select aria-label="Team" value={targetTeam} onChange={(e) => setTeam(e.target.value)}>
          {teams.map((t) => (
            <option key={t} value={t}>
              {t}
            </option>
          ))}
        </select>
        <button type="submit" className={styles.primaryButton} disabled={!name || !targetTeam}>
          Recruit
        </button>
        <button type="button" className={styles.linkButton} onClick={() => setCustomizing(!customizing)}>
          {customizing ? "Close" : "Customize as my template"}
        </button>
        {!template.builtin && (
          <button
            type="button"
            className={styles.denyButton}
            onClick={() => {
              const what = template.overrides_builtin ? "The built-in template comes back." : "It is removed from ~/.nobody/catalog.";
              if (window.confirm(`Delete your template ${template.name}? ${what} Agents recruited from it are not changed.`)) {
                void act(() => deleteTemplate(template.name), `${template.name} deleted.`, onDeleted);
              }
            }}
          >
            {template.overrides_builtin ? "Delete (restore built-in)" : "Delete template"}
          </button>
        )}
      </form>

      {customizing && (
        <>
          <DefinitionEditor value={definition} onChange={setDefinition} template />
          <div className={styles.row}>
            <input placeholder="Template name" value={copyName} onChange={(e) => setCopyName(e.target.value)} />
            <button
              type="button"
              className={styles.primaryButton}
              disabled={!copyName}
              onClick={() =>
                act(() => saveTemplate(copyName, cleanDefinition(definition)), `Saved to ~/.nobody/catalog/${copyName}.md.`, onSaved)
              }
            >
              Save template
            </button>
            {copyName === template.name && template.builtin && <span className={styles.muted}>replaces the built-in one for you</span>}
          </div>
        </>
      )}

      {notice && <p className={styles.muted}>{notice}</p>}
      {error && <p className={styles.error}>{error}</p>}
    </article>
  );
}

// CreateAgent writes an agent from scratch into a team.
function CreateAgent({ teams, onCreated }: { teams: string[]; onCreated: () => void }) {
  const [name, setName] = useState("");
  const [team, setTeam] = useState("");
  const [definition, setDefinition] = useState<AgentDefinition>(emptyDefinition());
  const [error, setError] = useState<string | null>(null);

  const targetTeam = team || teams[0] || "";

  async function create() {
    try {
      await recruitAgent(targetTeam, { name, definition: cleanDefinition(definition) });
      setError(null);
      onCreated();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  }

  return (
    <article className={styles.ticketDetail}>
      <div className={styles.row}>
        <input placeholder="Agent name" value={name} onChange={(e) => setName(e.target.value)} />
        <span className={styles.muted}>in</span>
        <select aria-label="Team" value={targetTeam} onChange={(e) => setTeam(e.target.value)}>
          {teams.map((t) => (
            <option key={t} value={t}>
              {t}
            </option>
          ))}
        </select>
      </div>
      <DefinitionEditor value={definition} onChange={setDefinition} />
      <div className={styles.row}>
        <button type="button" className={styles.primaryButton} disabled={!name || !targetTeam} onClick={() => void create()}>
          Create and start
        </button>
        {error && <span className={styles.error}>{error}</span>}
      </div>
    </article>
  );
}

// toDefinition keeps the file fields of a template (drops name and builtin).
function toDefinition(t: Template): AgentDefinition {
  // eslint-disable-next-line @typescript-eslint/no-unused-vars -- name and builtin are not part of the file
  const { name, builtin, ...definition } = t;
  return definition;
}
