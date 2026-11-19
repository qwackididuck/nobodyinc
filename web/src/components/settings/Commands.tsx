"use client";

import { type FormEvent, useEffect, useState } from "react";

import {
  type Command,
  COMMAND_NAME_PATTERN,
  type CommandDetail,
  type CommandList,
  type CommandScope,
  copyCommandToProject,
  deleteCommand,
  getCommand,
  listCommands,
  saveCommand,
} from "@/lib/api";
import ui from "../ui/ui.module.css";
import styles from "./settings.module.css";

// Where a new command goes, as the human chooses it (ADR-0017).
const SCOPES: { scope: CommandScope; label: string; help: string }[] = [
  { scope: "project", label: "The whole project", help: ".claude/commands, to commit: everyone on the project gets it, in Claude Code too." },
  { scope: "personal", label: "Me, on all my projects", help: "Your personal commands: every project's agents get it, and your own Claude Code. Not committed." },
];

const PERSONAL_WARNING = "This command is yours on all your projects: a change applies to every project, and to your own Claude Code.";

const ALLOWED_TOOLS_HELP = "While it runs, these tools run without asking: they skip the approval queue.";

// Commands lists the commands every agent gets: the project's and the
// user's personal ones, with what deserves a warning (allowed tools, not
// committed, shadowed). It creates, edits and deletes them, and copies a
// personal command into the project. Every agent restarts once idle after a
// change. Built on the same layout as Skills.tsx.
export function Commands() {
  const [list, setList] = useState<CommandList | null>(null);
  const [adding, setAdding] = useState(false);
  // The command being edited, once its detail is loaded.
  const [editing, setEditing] = useState<CommandDetail | null>(null);
  const [note, setNote] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    listCommands()
      .then((l) => !cancelled && setList(l))
      .catch((e: unknown) => !cancelled && setError(message(e)));
    return () => {
      cancelled = true;
    };
  }, []);

  function changed(next: CommandList, text: string) {
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

  const edit = (c: Command) =>
    run(async () => {
      setEditing(await getCommand(c.scope, c.name));
      setAdding(false);
      setError(null);
    });

  const remove = (c: Command) =>
    run(async () => {
      await deleteCommand(c.scope, c.name);
      changed(await listCommands(), `/${c.name} deleted: every agent restarts once its turn is over.`);
    });

  const copy = (c: Command) =>
    run(async () => {
      changed(
        await copyCommandToProject(c.name),
        `/${c.name} copied into the project's .claude/commands: this project's agents now get the copy. Commit it so that agents working on a feature get it too.`,
      );
    });

  const commands = list?.commands ?? [];
  const group = (scope: CommandScope) => commands.filter((c) => c.scope === scope);
  const card = (c: Command) =>
    editing?.scope === c.scope && editing.name === c.name ? (
      <CommandForm key={`${c.scope}/${c.name}`} editing={editing} onSaved={changed} onCancel={() => setEditing(null)} />
    ) : (
      <CommandCard key={`${c.scope}/${c.name}`} command={c} onEdit={() => void edit(c)} onRemove={() => void remove(c)} onCopy={() => void copy(c)} />
    );

  return (
    <section className={styles.section}>
      <h2 className={ui.title}>Commands</h2>
      <p className={ui.sub}>
        The project&apos;s commands and your personal commands, invoked as <code>/name</code>. Every agent gets them all; Claude Code&apos;s built-in commands are
        not listed.
      </p>
      {note && <p className={ui.note}>{note}</p>}
      {error && <p className={ui.error}>{error}</p>}

      {adding ? (
        <CommandForm onSaved={changed} onCancel={() => setAdding(false)} />
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
            + New command
          </button>
        </div>
      )}

      <h3 className={styles.skillGroup}>Project</h3>
      <div className={styles.servers}>
        {group("project").map(card)}
        {list && group("project").length === 0 && <p className={ui.sub}>No command in the project&apos;s .claude/commands.</p>}
      </div>

      <h3 className={styles.skillGroup}>Personal · all your projects</h3>
      <div className={styles.servers}>
        {group("personal").map(card)}
        {list && group("personal").length === 0 && <p className={ui.sub}>No personal command in your Claude Code configuration.</p>}
      </div>
    </section>
  );
}

// CommandCard is one project or personal command: its description, what
// deserves a warning, and what can be done with it.
function CommandCard({ command, onEdit, onRemove, onCopy }: { command: Command; onEdit: () => void; onRemove: () => void; onCopy: () => void }) {
  const personal = command.scope === "personal";

  return (
    <article className={styles.server}>
      <div className={styles.serverName}>
        /{command.name}
        {command.argument_hint && <span className={styles.serverType}>{command.argument_hint}</span>}
        <span className={styles.serverScope} data-scope={command.scope}>
          {personal ? "personal, all your projects" : "project"}
        </span>
        {command.allowed_tools && (
          <span className={styles.skillWarn} title={`${ALLOWED_TOOLS_HELP} ${command.allowed_tools}`}>
            allows tools
          </span>
        )}
        {command.committed === false && <span className={styles.skillWarn}>not committed</span>}
        {command.shadowed && <span className={styles.serverType}>hidden by the project&apos;s command</span>}
        <button type="button" className={`${ui.link} ${styles.serverEdit}`} onClick={onEdit}>
          Edit
        </button>
        {personal && !command.shadowed && (
          <button type="button" className={ui.link} onClick={onCopy} title="Copy it into the project's .claude/commands, to edit it for this project only">
            Copy to project
          </button>
        )}
        <RemoveCommand command={command} onRemove={onRemove} />
      </div>
      {command.description && <p className={ui.sub}>{command.description}</p>}
      {command.allowed_tools && (
        <p className={styles.scopeHelp}>
          {ALLOWED_TOOLS_HELP} <code>{command.allowed_tools}</code>
        </p>
      )}
      {command.committed === false && <p className={styles.scopeHelp}>Agents working on a feature only see committed commands: commit it for them to get it.</p>}
      {command.shadowed && <p className={styles.scopeHelp}>The project has a command of the same name: this project&apos;s agents get the project&apos;s.</p>}
    </article>
  );
}

// RemoveCommand asks for a confirmation before deleting a command's file.
function RemoveCommand({ command, onRemove }: { command: Command; onRemove: () => void }) {
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
      {command.scope === "personal" ? "Delete from your personal commands, for every project?" : "Delete its file from .claude/commands?"}{" "}
      <button type="button" className={ui.btnDanger} onClick={onRemove}>
        Delete /{command.name}
      </button>{" "}
      <button type="button" className={ui.link} onClick={() => setConfirming(false)}>
        Cancel
      </button>
    </span>
  );
}

// CommandForm creates a command (its name, where it goes, its description
// and prompt) or edits one: then its name and scope stay, and the other keys
// of its header are shown, read-only, as they will be kept.
function CommandForm({ editing, onSaved, onCancel }: { editing?: CommandDetail; onSaved: (list: CommandList, note: string) => void; onCancel: () => void }) {
  const [name, setName] = useState(editing?.name ?? "");
  const [scope, setScope] = useState<CommandScope | null>(editing?.scope ?? null);
  const [description, setDescription] = useState(editing?.description ?? "");
  const [body, setBody] = useState(editing?.body ?? "");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function submit(e: FormEvent) {
    e.preventDefault();
    if (!scope) return;
    const command = name.trim();
    setBusy(true);
    try {
      const list = await saveCommand(scope, command, description.trim(), body);
      const commit = scope === "project" ? " Commit it so that agents working on a feature get it too." : "";
      onSaved(list, `/${command} saved: every agent restarts once its turn is over.${commit}`);
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
            Edit /{editing.name}
            <span className={styles.serverScope} data-scope={editing.scope}>
              {editing.scope === "personal" ? "personal, all your projects" : "project"}
            </span>
          </>
        ) : (
          "New command"
        )}
      </div>
      {editing?.scope === "personal" && <p className={ui.note}>{PERSONAL_WARNING}</p>}
      {!editing && (
        <label className={ui.label}>
          NAME
          <input className={ui.input} value={name} onChange={(e) => setName(e.target.value)} placeholder="e.g. release-notes or front:test" pattern={COMMAND_NAME_PATTERN} required />
        </label>
      )}
      {!editing && (
        <p className={styles.scopeHelp}>Lowercase letters, digits and hyphens; &quot;:&quot; puts it in a sub-directory (front:test is front/test.md).</p>
      )}
      <label className={ui.label}>
        DESCRIPTION
        <input className={ui.input} value={description} onChange={(e) => setDescription(e.target.value)} placeholder="What it does (optional: Claude Code shows the prompt's first line)" />
      </label>
      <label className={ui.label}>
        PROMPT
        <textarea
          className={`${ui.textarea} ${styles.skillBody}`}
          value={body}
          onChange={(e) => setBody(e.target.value)}
          placeholder="Markdown: what the command asks. $ARGUMENTS stands for what follows /name."
          required
        />
      </label>
      {editing?.other_keys && (
        <div className={ui.label}>
          OTHER KEYS, KEPT AS THEY ARE
          <pre className={styles.otherKeys}>{editing.other_keys}</pre>
        </div>
      )}
      {editing?.allowed_tools && (
        <p className={styles.scopeHelp}>
          {ALLOWED_TOOLS_HELP} <code>{editing.allowed_tools}</code>
        </p>
      )}
      {!editing && (
        <fieldset className={styles.scopes}>
          <legend className={ui.label}>FOR WHOM</legend>
          {SCOPES.map((s) => (
            <label key={s.scope} className={styles.scope}>
              <input type="radio" name="command-scope" checked={scope === s.scope} onChange={() => setScope(s.scope)} />
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
        <button type="submit" className={ui.btnPrimary} disabled={busy || !scope || !name.trim() || !body.trim()}>
          {editing ? "Save" : "Create command"}
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
