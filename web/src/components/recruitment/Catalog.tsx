"use client";

import { type FormEvent, useState } from "react";

import { deleteTemplate, recruitAgent, recruitDirector, type Template } from "@/lib/api";
import { useLive } from "@/lib/live";
import { lookOf } from "@/lib/look";
import { isDirector } from "../chat/agents";
import { Sprite } from "../office/sprite";
import ui from "../ui/ui.module.css";
import { NAME_PATTERN } from "./definition";
import { TemplateEditor } from "./Editors";
import styles from "./recruitment.module.css";

// The recruiting target that makes the recruit the director (no team).
const DIRECTOR = "__director__";

// templateLook is the character shown for a template: the director's suit
// for the director's template, a look of its own otherwise.
export function templateLook(name: string) {
  return lookOf(`template:${name}`, name === "god");
}

// Catalog shows the templates as a character select screen: built-in ones
// and the user's own (~/.nobody/catalog, a same name replaces a built-in
// one), and the chosen template's sheet with the recruiting form.
export function Catalog() {
  const { templates } = useLive();
  const [picked, setPicked] = useState<string | null>(null);
  const chosen = templates.find((t) => t.name === picked) ?? templates.find((t) => t.name === "dev") ?? templates[0];

  const groups: { label: string; items: Template[] }[] = [
    { label: "Built in", items: templates.filter((t) => t.builtin) },
    { label: "Your templates · ~/.nobody/catalog", items: templates.filter((t) => !t.builtin) },
  ];

  return (
    <div className={styles.select}>
      <div className={styles.roster}>
        {groups.map(
          (g) =>
            g.items.length > 0 && (
              <div key={g.label} className={styles.group}>
                <p className={styles.groupLabel}>{g.label}</p>
                <div className={styles.tiles}>
                  {g.items.map((t) => (
                    <button key={t.name} type="button" className={styles.tile} aria-pressed={chosen?.name === t.name} onClick={() => setPicked(t.name)}>
                      <span className={styles.portrait}>
                        <Sprite look={templateLook(t.name)} />
                      </span>
                      <span className={styles.tileName}>{t.name}</span>
                      <span className={styles.tileRole}>{t.role}</span>
                      <span className={styles.badge} data-yours={!t.builtin}>
                        {t.builtin ? "Built in" : t.overrides_builtin ? "Yours, replaces built-in" : "Yours"}
                        {t.version ? ` · v${t.version}` : ""}
                      </span>
                    </button>
                  ))}
                </div>
              </div>
            ),
        )}
        {templates.length === 0 && <p className={ui.sub}>No template in the catalog.</p>}
      </div>
      {chosen && <Sheet key={chosen.name} template={chosen} />}
    </div>
  );
}

// Sheet is the chosen template: its character, what it is given, its
// instructions, and how to recruit it, customize it or delete it.
function Sheet({ template }: { template: Template }) {
  const { teams, agents } = useLive();
  const hasDirector = agents.some(isDirector);
  const targets = [...teams.map((t) => ({ id: t.id, label: t.id })), ...(hasDirector ? [] : [{ id: DIRECTOR, label: "Director (no team)" }])];
  const [name, setName] = useState("");
  // The team named like the template when there is one (dev → dev), the
  // director for the director's template while the project has none.
  const sameName = teams.find((t) => t.id === template.name)?.id;
  const [team, setTeam] = useState(template.name === "god" && !hasDirector ? DIRECTOR : (sameName ?? teams[0]?.id ?? ""));
  const [customizing, setCustomizing] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [note, setNote] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function act(action: () => Promise<unknown>, done: string) {
    setBusy(true);
    try {
      await action();
      setError(null);
      setNote(done);
    } catch (e) {
      setNote(null);
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }

  function recruit(e: FormEvent) {
    e.preventDefault();
    const n = name.trim();
    const body = { name: n, template: template.name };
    void act(
      () => (team === DIRECTOR ? recruitDirector(body) : recruitAgent(team, body)),
      team === DIRECTOR ? `${n} is your director. Watch the office: they walk in through the door.` : `${n} joins ${team}. Watch the office: they walk in through the door.`,
    ).then(() => setName(""));
  }

  if (customizing) {
    return (
      <aside className={styles.sheet}>
        <TemplateEditor template={template} onDone={() => setCustomizing(false)} />
      </aside>
    );
  }

  return (
    <aside className={styles.sheet} aria-label={`Template ${template.name}`}>
      <div className={styles.stage}>
        <Sprite look={templateLook(template.name)} />
      </div>
      <div className={styles.sheetName}>
        <strong>{template.name}</strong>
        <span>{template.role}</span>
      </div>
      {template.description && <p className={styles.description}>{template.description}</p>}
      <dl className={styles.attrs}>
        <dt>MODEL</dt>
        <dd>
          <span className={ui.chip}>{template.model || "default"}</span>
        </dd>
        <dt>PERMISSIONS</dt>
        <dd>
          <span className={ui.chip}>{template.permissions?.profile ?? "default"}</span>
        </dd>
        <dt>TOOLS</dt>
        <dd>
          {(template.tools ?? []).map((t) => (
            <span key={t} className={ui.chip}>
              {t}
            </span>
          ))}
          {!template.tools?.length && <span className={ui.chip}>none</span>}
        </dd>
        <dt>MCP</dt>
        <dd>
          {(template.mcp ?? []).map((m) => (
            <span key={m} className={ui.chip}>
              {m}
            </span>
          ))}
          {!template.mcp?.length && <span className={ui.chip}>none</span>}
        </dd>
      </dl>
      {template.prompt && <pre className={styles.instructions}>{template.prompt}</pre>}
      <form className={styles.recruit} onSubmit={recruit}>
        <label className={ui.label}>
          NAME
          <input id="recruit-name" className={ui.input} placeholder="e.g. frank" value={name} onChange={(e) => setName(e.target.value)} pattern={NAME_PATTERN} required />
        </label>
        <label className={ui.label}>
          TEAM
          <select id="recruit-team" className={ui.select} value={team} onChange={(e) => setTeam(e.target.value)}>
            {targets.map((t) => (
              <option key={t.id} value={t.id}>
                {t.label}
              </option>
            ))}
          </select>
        </label>
        <div className={styles.recruitActions}>
          <button type="submit" className={`${ui.btnPrimary} ${ui.big}`} disabled={busy || !name.trim() || !team}>
            Recruit ▶
          </button>
          <button type="button" className={ui.btn} onClick={() => setCustomizing(true)}>
            Customize as my template
          </button>
          {!template.builtin &&
            (deleting ? (
              <>
                <button type="button" className={ui.btnDanger} disabled={busy} onClick={() => void act(() => deleteTemplate(template.name), `${template.name} deleted.`)}>
                  {template.overrides_builtin ? "Delete, bring back the built-in" : "Delete for good"}
                </button>
                <button type="button" className={ui.link} onClick={() => setDeleting(false)}>
                  Cancel
                </button>
              </>
            ) : (
              <button type="button" className={ui.link} onClick={() => setDeleting(true)}>
                Delete this template
              </button>
            ))}
        </div>
        {note && <p className={ui.note}>{note}</p>}
        {error && <p className={ui.error}>{error}</p>}
      </form>
    </aside>
  );
}
