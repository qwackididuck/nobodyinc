"use client";

import { type FormEvent, useEffect, useState } from "react";

import { deleteEnv, type EnvVariable, listEnv, setEnv } from "@/lib/api";
import ui from "../ui/ui.module.css";
import styles from "./settings.module.css";

// What people usually set; a click fills the name.
const SUGGESTIONS = ["ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "GITLAB_TOKEN"];

const NAME = /^[A-Za-z_][A-Za-z0-9_]*$/;

// EnvVariables lists the project's environment variables (ADR-0020): what the
// agents' processes get on top of the daemon's environment, e.g. the Claude
// key or the token of an MCP server. A value is never shown, nor returned by
// the daemon: it is typed to add or replace a variable, then forgotten.
// Every agent restarts once idle after a change.
export function EnvVariables() {
  const [variables, setVariables] = useState<EnvVariable[] | null>(null);
  // The variable whose value is being replaced.
  const [replacing, setReplacing] = useState<string | null>(null);
  const [note, setNote] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    listEnv()
      .then((v) => !cancelled && setVariables(v))
      .catch((e: unknown) => !cancelled && setError(e instanceof Error ? e.message : String(e)));
    return () => {
      cancelled = true;
    };
  }, []);

  async function save(name: string, value: string): Promise<boolean> {
    try {
      setVariables(await setEnv(name, value));
      setReplacing(null);
      setError(null);
      setNote(`${name} saved: every agent restarts once its turn is over to get it.`);
      return true;
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
      return false;
    }
  }

  async function remove(name: string) {
    try {
      await deleteEnv(name);
      setVariables((list) => list?.filter((v) => v.name !== name) ?? null);
      setError(null);
      setNote(`${name} removed: every agent restarts once its turn is over.`);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  }

  return (
    <section className={styles.section}>
      <h2 className={ui.title}>Environment</h2>
      <p className={ui.sub}>
        Variables of this project only. The agents get them on top of the daemon&apos;s environment, and they replace a variable of the same name: your own
        shell and your other projects are not affected. Use them for the Claude key, or for the tokens your MCP servers refer to as ${"{NAME}"}.
      </p>
      {note && <p className={ui.note}>{note}</p>}
      {error && <p className={ui.error}>{error}</p>}
      <div className={styles.servers}>
        {variables?.map((v) =>
          replacing === v.name ? (
            <ValueForm key={v.name} name={v.name} onSave={(value) => save(v.name, value)} onCancel={() => setReplacing(null)} />
          ) : (
            <VariableCard key={v.name} variable={v} onReplace={() => setReplacing(v.name)} onRemove={() => void remove(v.name)} />
          ),
        )}
        {variables?.length === 0 && <p className={ui.sub}>No variable yet: the agents get the daemon&apos;s environment as it is.</p>}
      </div>
      <AddForm onSave={save} />
      <p className={styles.foot}>
        Values are kept in a file only you can read, in this project&apos;s folder under ~/.nobody, never in the project, and are never shown again. Names
        Nobody Inc sets itself (NOBODY_*, GIT_AUTHOR_*, GIT_COMMITTER_*) are refused.
      </p>
    </section>
  );
}

// VariableCard is one variable: its name, never its value.
function VariableCard({ variable, onReplace, onRemove }: { variable: EnvVariable; onReplace: () => void; onRemove: () => void }) {
  const [confirming, setConfirming] = useState(false);

  return (
    <article className={styles.server}>
      <div className={styles.serverName}>
        <code>{variable.name}</code>
        <span className={styles.serverType}>••••••••</span>
        <button type="button" className={`${ui.link} ${styles.serverEdit}`} onClick={onReplace}>
          Replace value
        </button>
        {confirming ? (
          <span className={styles.serverRemove}>
            Remove it from this project?{" "}
            <button type="button" className={ui.btnDanger} onClick={onRemove}>
              Remove {variable.name}
            </button>{" "}
            <button type="button" className={ui.link} onClick={() => setConfirming(false)}>
              Cancel
            </button>
          </span>
        ) : (
          <button type="button" className={`${ui.link} ${styles.serverRemove}`} onClick={() => setConfirming(true)}>
            Remove
          </button>
        )}
      </div>
    </article>
  );
}

// ValueForm replaces the value of a variable.
function ValueForm({ name, onSave, onCancel }: { name: string; onSave: (value: string) => Promise<boolean>; onCancel: () => void }) {
  const [value, setValue] = useState("");
  const [busy, setBusy] = useState(false);

  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    try {
      await onSave(value);
    } finally {
      setBusy(false);
    }
  }

  return (
    <form className={`${styles.server} ${styles.addServer}`} onSubmit={(e) => void submit(e)}>
      <div className={styles.serverName}>
        Replace the value of <code>{name}</code>
      </div>
      <ValueInput value={value} onChange={setValue} autoFocus />
      <div className={styles.addActions}>
        <button type="submit" className={ui.btnPrimary} disabled={busy || value === ""}>
          Save
        </button>
        <button type="button" className={ui.link} onClick={onCancel}>
          Cancel
        </button>
      </div>
    </form>
  );
}

// AddForm adds a variable.
function AddForm({ onSave }: { onSave: (name: string, value: string) => Promise<boolean> }) {
  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  const [value, setValue] = useState("");
  const [busy, setBusy] = useState(false);

  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    try {
      if (await onSave(name.trim(), value)) {
        setOpen(false);
        setName("");
        setValue("");
      }
    } finally {
      setBusy(false);
    }
  }

  if (!open) {
    return (
      <div>
        <button type="button" className={ui.btn} onClick={() => setOpen(true)}>
          + Add variable
        </button>
      </div>
    );
  }

  const badName = name.trim() !== "" && !NAME.test(name.trim());

  return (
    <form className={`${styles.server} ${styles.addServer}`} onSubmit={(e) => void submit(e)}>
      <div className={styles.serverName}>New variable</div>
      <label className={ui.label}>
        NAME
        <input
          className={ui.input}
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="e.g. GITLAB_TOKEN"
          autoCapitalize="characters"
          autoComplete="off"
          spellCheck={false}
          required
          autoFocus
        />
        <span className={styles.scopeHelp}>
          {SUGGESTIONS.map((s) => (
            <button key={s} type="button" className={ui.link} onClick={() => setName(s)}>
              {s}
            </button>
          ))}
        </span>
        {badName && <span className={ui.error}>Letters, digits and _, not starting with a digit.</span>}
      </label>
      <ValueInput value={value} onChange={setValue} />
      <div className={styles.addActions}>
        <button type="submit" className={ui.btnPrimary} disabled={busy || name.trim() === "" || badName || value === ""}>
          Add variable
        </button>
        <button type="button" className={ui.link} onClick={() => setOpen(false)}>
          Cancel
        </button>
      </div>
    </form>
  );
}

// ValueInput is where a value is typed: hidden, and kept out of the
// browser's password manager and autofill.
function ValueInput({ value, onChange, autoFocus }: { value: string; onChange: (v: string) => void; autoFocus?: boolean }) {
  return (
    <label className={ui.label}>
      VALUE
      <input
        className={ui.input}
        type="password"
        value={value}
        onChange={(e) => onChange(e.target.value)}
        autoComplete="new-password"
        spellCheck={false}
        autoFocus={autoFocus}
        required
      />
      <span className={styles.scopeHelp}>It is saved, then never shown again.</span>
    </label>
  );
}
