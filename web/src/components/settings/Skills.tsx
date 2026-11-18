"use client";

import { type FormEvent, useEffect, useState } from "react";

import {
  copySkillToProject,
  deleteSkill,
  getSkill,
  listSkills,
  refreshBuiltinSkills,
  saveSkill,
  type Skill,
  SKILL_NAME_PATTERN,
  type SkillDetail,
  type SkillList,
  type SkillScope,
} from "@/lib/api";
import ui from "../ui/ui.module.css";
import styles from "./settings.module.css";

// Where a new skill goes, as the human chooses it (ADR-0011).
const SCOPES: { scope: SkillScope; label: string; help: string }[] = [
  { scope: "project", label: "The whole project", help: ".claude/skills, to commit: everyone on the project gets it, in Claude Code too." },
  { scope: "personal", label: "Me, on all my projects", help: "Your personal skills: every project's agents get it, and your own Claude Code. Not committed." },
];

const PERSONAL_WARNING = "This skill is yours on all your projects: a change applies to every project, and to your own Claude Code.";

// Skills lists the skills every agent gets: the project's, the user's
// personal ones and Claude Code's built-in ones, with what deserves a
// warning (hooks, not committed, shadowed). It creates, edits and deletes
// project and personal skills, and copies a personal skill into the project.
// Every agent restarts once idle after a change.
export function Skills() {
  const [list, setList] = useState<SkillList | null>(null);
  const [adding, setAdding] = useState(false);
  // The skill being edited, once its detail is loaded.
  const [editing, setEditing] = useState<SkillDetail | null>(null);
  const [refreshing, setRefreshing] = useState(false);
  const [note, setNote] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    listSkills()
      .then((l) => !cancelled && setList(l))
      .catch((e: unknown) => !cancelled && setError(message(e)));
    return () => {
      cancelled = true;
    };
  }, []);

  function changed(next: SkillList, text: string) {
    setList(next);
    setAdding(false);
    setEditing(null);
    setError(null);
    setNote(text);
  }

  async function run(action: () => Promise<void>) {
    try {
      await action();
    } catch (e) {
      setError(message(e));
    }
  }

  const edit = (s: Skill) =>
    run(async () => {
      setEditing(await getSkill(s.scope as SkillScope, s.name));
      setAdding(false);
      setError(null);
    });

  const remove = (s: Skill) =>
    run(async () => {
      await deleteSkill(s.scope as SkillScope, s.name);
      changed(await listSkills(), `${s.name} deleted: every agent restarts once its turn is over.`);
    });

  const copy = (s: Skill) =>
    run(async () => {
      changed(
        await copySkillToProject(s.name),
        `${s.name} copied into the project's .claude/skills: this project's agents now get the copy. Commit it so that agents working on a feature get it too.`,
      );
    });

  const refresh = () =>
    run(async () => {
      setRefreshing(true);
      try {
        setList(await refreshBuiltinSkills());
      } finally {
        setRefreshing(false);
      }
    });

  const skills = list?.skills ?? [];
  const group = (scope: Skill["scope"]) => skills.filter((s) => s.scope === scope);
  const card = (s: Skill) =>
    editing?.scope === s.scope && editing.name === s.name ? (
      <SkillForm key={`${s.scope}/${s.name}`} editing={editing} onSaved={changed} onCancel={() => setEditing(null)} />
    ) : (
      <SkillCard key={`${s.scope}/${s.name}`} skill={s} onEdit={() => void edit(s)} onRemove={() => void remove(s)} onCopy={() => void copy(s)} />
    );

  return (
    <section className={styles.section}>
      <h2 className={ui.title}>Skills</h2>
      <p className={ui.sub}>
        The project&apos;s skills, your personal skills and Claude Code&apos;s built-in ones. Every agent gets them all, except those its file excludes; an agent
        can also preload some, to always have them in mind.
      </p>
      {note && <p className={ui.note}>{note}</p>}
      {error && <p className={ui.error}>{error}</p>}

      {adding ? (
        <SkillForm onSaved={changed} onCancel={() => setAdding(false)} />
      ) : (
        <div>
          <button
            type="button"
            className={ui.btn}
            onClick={() => {
              setAdding(true);
              setEditing(null);
            }}
          >
            + New skill
          </button>
        </div>
      )}

      <h3 className={styles.skillGroup}>Project</h3>
      <div className={styles.servers}>
        {group("project").map(card)}
        {list && group("project").length === 0 && <p className={ui.sub}>No skill in the project&apos;s .claude/skills.</p>}
      </div>

      <h3 className={styles.skillGroup}>Personal · all your projects</h3>
      <div className={styles.servers}>
        {group("personal").map(card)}
        {list && group("personal").length === 0 && <p className={ui.sub}>No personal skill in your Claude Code configuration.</p>}
      </div>

      <details className={styles.builtin}>
        <summary className={styles.skillGroup}>Built-in ({group("builtin").length})</summary>
        <p className={ui.sub}>
          Claude Code&apos;s own skills, read from Claude Code without calling the model. They cannot be changed; an agent can exclude them.{" "}
          <button type="button" className={ui.link} disabled={refreshing} onClick={() => void refresh()}>
            {refreshing ? "Refreshing…" : "Refresh"}
          </button>
        </p>
        {list?.builtin_error && <p className={ui.error}>{list.builtin_error}</p>}
        <div className={styles.builtinChips}>
          {group("builtin").map((s) => (
            <span key={s.name} className={ui.chip}>
              {s.name}
            </span>
          ))}
        </div>
      </details>
    </section>
  );
}

// SkillCard is one project or personal skill: its description, what
// deserves a warning, and what can be done with it.
function SkillCard({ skill, onEdit, onRemove, onCopy }: { skill: Skill; onEdit: () => void; onRemove: () => void; onCopy: () => void }) {
  const personal = skill.scope === "personal";

  return (
    <article className={styles.server}>
      <div className={styles.serverName}>
        {skill.name}
        <span className={styles.serverScope} data-scope={skill.scope}>
          {personal ? "personal, all your projects" : "project"}
        </span>
        {skill.has_hooks && (
          <span className={styles.skillWarn} title="Invoking it asks for permission: you approve it each time, then its hooks run.">
            declares hooks
          </span>
        )}
        {skill.committed === false && <span className={styles.skillWarn}>not committed</span>}
        {skill.shadowed && <span className={styles.serverType}>hidden by the project&apos;s skill</span>}
        <button type="button" className={`${ui.link} ${styles.serverEdit}`} onClick={onEdit}>
          Edit
        </button>
        {personal && !skill.shadowed && (
          <button type="button" className={ui.link} onClick={onCopy} title="Copy it into the project's .claude/skills, to edit it for this project only">
            Copy to project
          </button>
        )}
        <RemoveSkill skill={skill} onRemove={onRemove} />
      </div>
      <p className={ui.sub}>{skill.description}</p>
      {skill.committed === false && <p className={styles.scopeHelp}>Agents working on a feature only see committed skills: commit it for them to get it.</p>}
      {skill.shadowed && <p className={styles.scopeHelp}>The project has a skill of the same name: this project&apos;s agents get the project&apos;s.</p>}
    </article>
  );
}

// RemoveSkill asks for a confirmation before deleting a skill's directory.
function RemoveSkill({ skill, onRemove }: { skill: Skill; onRemove: () => void }) {
  const [confirming, setConfirming] = useState(false);

  if (!confirming) {
    return (
      <button type="button" className={`${ui.link} ${styles.serverRemove}`} onClick={() => setConfirming(true)}>
        Delete
      </button>
    );
  }

  return (
    <span className={styles.serverRemove}>
      {skill.scope === "personal" ? "Delete from your personal skills, for every project?" : "Delete its directory from .claude/skills?"}{" "}
      <button type="button" className={ui.btnDanger} onClick={onRemove}>
        Delete {skill.name}
      </button>{" "}
      <button type="button" className={ui.link} onClick={() => setConfirming(false)}>
        Cancel
      </button>
    </span>
  );
}

// SkillForm creates a skill (its name, where it goes, its description and
// instructions) or edits one: then its name and scope stay, and the other
// keys of its header are shown, read-only, as they will be kept.
function SkillForm({ editing, onSaved, onCancel }: { editing?: SkillDetail; onSaved: (list: SkillList, note: string) => void; onCancel: () => void }) {
  const [name, setName] = useState(editing?.name ?? "");
  const [scope, setScope] = useState<SkillScope | null>(editing?.scope ?? null);
  const [description, setDescription] = useState(editing?.description ?? "");
  const [body, setBody] = useState(editing?.body ?? "");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function submit(e: FormEvent) {
    e.preventDefault();
    if (!scope) return;
    const skill = name.trim();
    setBusy(true);
    try {
      const list = await saveSkill(scope, skill, description.trim(), body);
      const commit = scope === "project" ? " Commit it so that agents working on a feature get it too." : "";
      onSaved(list, `${skill} saved: every agent restarts once its turn is over.${commit}`);
    } catch (err) {
      setError(message(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <form className={`${styles.server} ${styles.addServer}`} onSubmit={(e) => void submit(e)}>
      <div className={styles.serverName}>
        {editing ? (
          <>
            Edit {editing.name}
            <span className={styles.serverScope} data-scope={editing.scope}>
              {editing.scope === "personal" ? "personal, all your projects" : "project"}
            </span>
          </>
        ) : (
          "New skill"
        )}
      </div>
      {editing?.scope === "personal" && <p className={ui.note}>{PERSONAL_WARNING}</p>}
      {!editing && (
        <label className={ui.label}>
          NAME
          <input className={ui.input} value={name} onChange={(e) => setName(e.target.value)} placeholder="e.g. release-notes" pattern={SKILL_NAME_PATTERN} required />
        </label>
      )}
      {!editing && <p className={styles.scopeHelp}>Lowercase letters, digits and hyphens: it is also the name of the skill&apos;s directory.</p>}
      <label className={ui.label}>
        DESCRIPTION
        <input
          className={ui.input}
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          placeholder="What it does and when to use it: agents choose skills from this"
          required
        />
      </label>
      <label className={ui.label}>
        INSTRUCTIONS
        <textarea className={`${ui.textarea} ${styles.skillBody}`} value={body} onChange={(e) => setBody(e.target.value)} placeholder="Markdown: what to do when the skill applies." />
      </label>
      {editing?.other_keys && (
        <div className={ui.label}>
          OTHER KEYS, KEPT AS THEY ARE
          <pre className={styles.otherKeys}>{editing.other_keys}</pre>
        </div>
      )}
      {editing?.has_hooks && <p className={styles.scopeHelp}>It declares hooks: invoking it asks for your approval each time, then its hooks run.</p>}
      {!editing && (
        <fieldset className={styles.scopes}>
          <legend className={ui.label}>FOR WHOM</legend>
          {SCOPES.map((s) => (
            <label key={s.scope} className={styles.scope}>
              <input type="radio" name="skill-scope" checked={scope === s.scope} onChange={() => setScope(s.scope)} />
              <span>
                <strong>{s.label}</strong>
                <span className={styles.scopeHelp}>{s.help}</span>
              </span>
            </label>
          ))}
        </fieldset>
      )}
      {error && <p className={ui.error}>{error}</p>}
      <div className={styles.addActions}>
        <button type="submit" className={ui.btnPrimary} disabled={busy || !scope || !name.trim() || !description.trim()}>
          {editing ? "Save" : "Create skill"}
        </button>
        <button type="button" className={ui.link} onClick={onCancel}>
          Cancel
        </button>
      </div>
    </form>
  );
}

function message(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}
