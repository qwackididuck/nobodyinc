"use client";

import { type FormEvent, useEffect, useState } from "react";

import { type AgentDefinition, editDefinition, getDefinition, recruitAgent, recruitDirector, saveTemplate, type Template } from "@/lib/api";
import { useLive } from "@/lib/live";
import { isDirector } from "../chat/agents";
import ui from "../ui/ui.module.css";
import { cleanDefinition, emptyDefinition, NAME_PATTERN, toDefinition } from "./definition";
import { DefinitionFields } from "./DefinitionFields";
import styles from "./recruitment.module.css";

function useAction() {
  const [busy, setBusy] = useState(false);
  const [note, setNote] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  async function run(action: () => Promise<unknown>, done: string): Promise<boolean> {
    setBusy(true);
    try {
      await action();
      setError(null);
      setNote(done);
      return true;
    } catch (e) {
      setNote(null);
      setError(e instanceof Error ? e.message : String(e));
      return false;
    } finally {
      setBusy(false);
    }
  }

  return { busy, note, error, run };
}

// EditAgent rewrites a recruited agent's file. The agent restarts once its
// turn is over (or now): with a new session when its role or instructions
// change, resuming its session (and memory) otherwise.
export function EditAgent({ address, onDone }: { address: string; onDone: () => void }) {
  const [definition, setDefinition] = useState<AgentDefinition | null>(null);
  const [now, setNow] = useState(false);
  const { busy, note, error, run } = useAction();

  useEffect(() => {
    let cancelled = false;
    getDefinition(address)
      .then((d) => !cancelled && setDefinition(d))
      .catch(() => !cancelled && setDefinition(emptyDefinition()));
    return () => {
      cancelled = true;
    };
  }, [address]);

  if (!definition) return <p className={ui.sub}>Loading…</p>;

  const when = now ? "now" : "once its turn is over";
  const save = (fresh: boolean) =>
    void run(() => editDefinition(address, cleanDefinition(definition), { fresh, now }), `Saved: ${address} restarts ${when}${fresh ? " in a new session" : ""}.`);

  return (
    <div className={styles.editor}>
      <DefinitionFields value={definition} onChange={setDefinition} />
      <label className={styles.nowToggle}>
        <button type="button" role="switch" className={ui.switch} aria-checked={now} aria-label="Apply at once" onClick={() => setNow(!now)} />
        Apply at once, without waiting for the end of the current turn
      </label>
      <div className={styles.recruitActions}>
        <button type="button" className={ui.btnPrimary} disabled={busy} onClick={() => save(false)}>
          Save and restart
        </button>
        <button type="button" className={ui.btn} disabled={busy} onClick={() => save(true)}>
          Save, new session
        </button>
        <button type="button" className={ui.link} onClick={onDone}>
          Back to the staff
        </button>
      </div>
      {note && <p className={ui.note}>{note}</p>}
      {error && <p className={ui.error}>{error}</p>}
    </div>
  );
}

// NewAgent writes an agent from scratch into a team (or as the director
// while there is none) and starts it, or saves it as a template instead.
export function NewAgent() {
  const { teams, agents } = useLive();
  const hasDirector = agents.some(isDirector);
  const [name, setName] = useState("");
  const [team, setTeam] = useState(teams[0]?.id ?? "");
  const [definition, setDefinition] = useState<AgentDefinition>(emptyDefinition());
  const { busy, note, error, run } = useAction();

  function create(e: FormEvent) {
    e.preventDefault();
    const body = { name: name.trim(), definition: cleanDefinition(definition) };
    void run(
      () => (team === "" ? recruitDirector(body) : recruitAgent(team, body)),
      `${body.name} is created. Watch the office: they walk in through the door.`,
    );
  }

  return (
    <form className={styles.editor} onSubmit={create}>
      <div className={styles.fields}>
        <label className={ui.label}>
          NAME
          <input id="new-name" className={ui.input} placeholder="e.g. kim" value={name} onChange={(e) => setName(e.target.value)} pattern={NAME_PATTERN} required />
        </label>
        <label className={ui.label}>
          TEAM
          <select id="new-team" className={ui.select} value={team} onChange={(e) => setTeam(e.target.value)}>
            {teams.map((t) => (
              <option key={t.id} value={t.id}>
                {t.id}
              </option>
            ))}
            {!hasDirector && <option value="">Director (no team)</option>}
          </select>
        </label>
      </div>
      <DefinitionFields value={definition} onChange={setDefinition} />
      <div className={styles.recruitActions}>
        <button type="submit" className={ui.btnPrimary} disabled={busy || !name.trim()}>
          Create and start
        </button>
        <button
          type="button"
          className={ui.btn}
          disabled={busy || !name.trim()}
          onClick={() => void run(() => saveTemplate(name.trim(), cleanDefinition(definition)), `Saved to ~/.nobody/catalog/${name.trim()}.md.`)}
        >
          Save as a template instead
        </button>
      </div>
      {note && <p className={ui.note}>{note}</p>}
      {error && <p className={ui.error}>{error}</p>}
    </form>
  );
}

// TemplateEditor saves a customized copy of a template in the user's
// catalog. Keeping a built-in template's name replaces it for this user.
export function TemplateEditor({ template, onDone }: { template: Template; onDone: () => void }) {
  const [name, setName] = useState(template.name);
  const [definition, setDefinition] = useState<AgentDefinition>(toDefinition(template));
  const { busy, note, error, run } = useAction();

  return (
    <div className={styles.editor}>
      <h3 className={styles.editorTitle}>Customize {template.name}</h3>
      <label className={ui.label}>
        TEMPLATE NAME
        <input id="template-name" className={ui.input} value={name} onChange={(e) => setName(e.target.value)} pattern={NAME_PATTERN} required />
        {name === template.name && template.builtin && <span className={styles.hint}>Same name: your copy replaces the built-in template for you.</span>}
      </label>
      <DefinitionFields value={definition} onChange={setDefinition} template />
      <div className={styles.recruitActions}>
        <button
          type="button"
          className={ui.btnPrimary}
          disabled={busy || !name.trim()}
          onClick={() => void run(() => saveTemplate(name.trim(), cleanDefinition(definition)), `Saved to ~/.nobody/catalog/${name.trim()}.md.`)}
        >
          Save template
        </button>
        <button type="button" className={ui.link} onClick={onDone}>
          Back
        </button>
      </div>
      {note && <p className={ui.note}>{note}</p>}
      {error && <p className={ui.error}>{error}</p>}
    </div>
  );
}
