"use client";

import { type FormEvent, useState } from "react";

import { recruitDirector } from "@/lib/api";
import { useLive } from "@/lib/live";
import styles from "./chat.module.css";

// The built-in template of the director (internal/catalog/builtin/god.md).
const DIRECTOR_TEMPLATE = "god";

// HireDirector is shown in place of the director's chat while the project
// has none: the director talks with the human, turns requests into features
// and tickets, and leads the teams (docs/god.md). Its address is its name.
export function HireDirector({ onHired }: { onHired: (address: string) => void }) {
  const { templates, reload } = useLive();
  const [name, setName] = useState("god");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const template = templates.find((t) => t.name === DIRECTOR_TEMPLATE);

  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    try {
      const agent = await recruitDirector({ name: name.trim(), template: DIRECTOR_TEMPLATE });
      reload("agents");
      onHired(agent.address);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className={styles.body}>
      <form className={styles.card} data-kind="proposal" onSubmit={submit}>
        <span className={styles.kicker}>No director yet</span>
        <strong className={styles.cardTitle}>Recruit the director</strong>
        <p className={styles.cardText}>
          {template?.description ??
            "The director talks with you, turns your requests into features and tickets, and leads the teams. There is one per project."}
        </p>
        <label className={styles.kicker} htmlFor="director-name">
          Name (also its address)
        </label>
        <input id="director-name" className={styles.input} value={name} onChange={(e) => setName(e.target.value)} pattern="[a-z][a-z0-9-]*" required />
        <div className={styles.actions}>
          <button type="submit" className={styles.btnPrimary} disabled={busy || !name.trim()}>
            Recruit
          </button>
        </div>
        {error && <p className={styles.error}>{error}</p>}
      </form>
    </div>
  );
}
