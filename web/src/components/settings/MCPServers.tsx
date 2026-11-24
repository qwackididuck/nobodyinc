"use client";

import { type FormEvent, useEffect, useState } from "react";

import {
  addMCPServer,
  getMCPServer,
  listMCPServers,
  type MCPScope,
  type MCPServer,
  type MCPServerDetail,
  type MCPServerInput,
  removeMCPServer,
  updateMCPServer,
} from "@/lib/api";
import { useLive } from "@/lib/live";
import { lookOf } from "@/lib/look";
import { isDirector } from "../chat/agents";
import { Sprite } from "../office/sprite";
import ui from "../ui/ui.module.css";
import styles from "./settings.module.css";

// Where a server goes, as the human chooses it when adding one (ADR-0010).
const SCOPES: { scope: MCPScope; label: string; help: string }[] = [
  { scope: "project", label: "The whole project", help: ".mcp.json, committed: everyone on the project gets it, in Claude Code too." },
  { scope: "local", label: "Me, on this project", help: "Your Claude Code configuration, for this project only. Not committed." },
  { scope: "user", label: "Me, on all my projects", help: "Your Claude Code configuration, for every project. Not committed." },
];

const SCOPE_LABELS: Record<MCPScope, string> = { project: "project", local: "me, this project", user: "me, everywhere" };

// A value refers to an environment variable: ${NAME} or ${NAME:-default}.
const VARIABLE = /\$\{[A-Za-z_][A-Za-z0-9_]*(:-[^}]*)?\}/;

// MCPServers lists the servers every agent gets, from Claude Code's three
// scopes, without any value in clear; adds one to the scope the human
// chooses, edits or removes one. Every agent restarts once idle after a
// change.
export function MCPServers() {
  const [servers, setServers] = useState<MCPServer[] | null>(null);
  const [adding, setAdding] = useState(false);
  // The server being edited, once its detail is loaded.
  const [editing, setEditing] = useState<MCPServerDetail | null>(null);
  const [note, setNote] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  useEffect(() => {
    let cancelled = false;
    listMCPServers()
      .then((s) => !cancelled && setServers(s))
      .catch((e: unknown) => !cancelled && setError(e instanceof Error ? e.message : String(e)));
    return () => {
      cancelled = true;
    };
  }, []);

  function saved(list: MCPServer[], name: string) {
    const verb = editing ? "changed" : "added";
    setServers(list);
    setAdding(false);
    setEditing(null);
    setError(null);
    const missing = list.find((s) => s.name === name)?.env.filter((v) => !v.set).map((v) => v.name) ?? [];
    setNote(
      missing.length > 0
        ? `${name} ${verb}. Set ${missing.join(", ")} in the Environment section, or in the environment you start the daemon from.`
        : `${name} ${verb}: every agent restarts once its turn is over to get it.`,
    );
  }

  async function edit(s: MCPServer) {
    try {
      setEditing(await getMCPServer(s.name));
      setAdding(false);
      setError(null);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  }

  async function remove(s: MCPServer) {
    try {
      await removeMCPServer(s.name, s.source);
      setServers((list) => list?.filter((x) => x.name !== s.name) ?? null);
      setError(null);
      setNote(`${s.name} removed: every agent restarts once its turn is over.`);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  }

  return (
    <section className={styles.section}>
      <h2 className={ui.title}>MCP servers</h2>
      <p className={ui.sub}>
        The servers of Claude Code&apos;s three scopes: the project&apos;s .mcp.json, yours on this project, and yours on all your projects. Every agent gets
        them all, except those its file excludes.
      </p>
      {note && <p className={ui.note}>{note}</p>}
      {error && <p className={ui.error}>{error}</p>}
      <div className={styles.servers}>
        {servers?.map((s) =>
          editing?.name === s.name ? (
            <ServerForm key={s.name} editing={editing} onSaved={saved} onCancel={() => setEditing(null)} />
          ) : (
            <ServerCard key={s.name} server={s} onEdit={() => void edit(s)} onRemove={() => void remove(s)} />
          ),
        )}
        <article className={`${styles.server} ${styles.locked}`}>
          <div className={styles.serverName}>
            nobody <span className={styles.serverType}>built in</span>
          </div>
          <p className={ui.sub}>The office: mailboxes, tickets, and the director&apos;s tools. Every agent gets it.</p>
        </article>
        {servers?.length === 0 && <p className={ui.sub}>No MCP server in the project or in your Claude Code configuration.</p>}
      </div>
      {adding ? (
        <ServerForm onSaved={saved} onCancel={() => setAdding(false)} />
      ) : (
        <div>
          <button type="button" className={ui.btn} onClick={() => {
              setAdding(true);
              setEditing(null);
            }}>
            + Add server
          </button>
        </div>
      )}
      <p className={styles.foot}>Secrets are never written to these files: they stay in environment variables, written as ${"{NAME}"}, and are set in the Environment section or outside Nobody Inc.</p>
    </section>
  );
}

// ServerCard is one server of the list: where it comes from, what it runs or
// reaches (without what may hold a secret), its variables, and who gets it.
function ServerCard({ server, onEdit, onRemove }: { server: MCPServer; onEdit: () => void; onRemove: () => void }) {
  const { agents } = useLive();
  const byAddress = new Map(agents.map((a) => [a.address, a]));

  return (
    <article className={styles.server}>
      <div className={styles.serverName}>
        {server.name} <span className={styles.serverType}>{server.type}</span>
        <span className={styles.serverScope} data-scope={server.source}>
          {SCOPE_LABELS[server.source]}
        </span>
        <button type="button" className={`${ui.link} ${styles.serverEdit}`} onClick={onEdit}>
          Edit
        </button>
        <RemoveServer server={server} onRemove={onRemove} />
      </div>
      <code className={styles.serverTarget}>{server.target}</code>
      {server.env.length > 0 && (
        <div className={styles.env}>
          {server.env.map((v) => (
            <span key={v.name} className={v.set ? styles.envSet : styles.envMissing}>
              {v.set ? "✓" : "✗"} {v.name} {v.set ? "set" : "missing from the project's variables and the daemon's environment"}
            </span>
          ))}
        </div>
      )}
      <div className={styles.users}>
        {server.excluded_by.length === 0 ? (
          "Every agent gets it"
        ) : (
          <>
            Every agent but
            {server.excluded_by.map((u) => {
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
  );
}

// RemoveServer asks for a confirmation before removing a server.
function RemoveServer({ server, onRemove }: { server: MCPServer; onRemove: () => void }) {
  const [confirming, setConfirming] = useState(false);

  if (!confirming) {
    return (
      <button type="button" className={`${ui.link} ${styles.serverRemove}`} onClick={() => setConfirming(true)}>
        Remove
      </button>
    );
  }

  return (
    <span className={styles.serverRemove}>
      Remove from {server.source === "project" ? ".mcp.json, for everyone" : "your configuration"}?{" "}
      <button type="button" className={ui.btnDanger} onClick={onRemove}>
        Remove {server.name}
      </button>{" "}
      <button type="button" className={ui.link} onClick={() => setConfirming(false)}>
        Cancel
      </button>
    </span>
  );
}

// A row of env or headers: a name and a value referring to a variable;
// clear marks a value written in clear in the configuration, never sent.
type Pair = { key: string; value: string; clear?: boolean };

// ServerForm adds a server (its transport, what it runs or reaches, its
// variables, and the scope it goes to), or edits one: then its name and
// scope stay, its arguments and URL are hidden until revealed, and a value
// written in clear must be replaced by a variable.
function ServerForm({ editing, onSaved, onCancel }: { editing?: MCPServerDetail; onSaved: (list: MCPServer[], name: string) => void; onCancel: () => void }) {
  const initial = editing?.server;
  const [name, setName] = useState(editing?.name ?? "");
  const [type, setType] = useState<MCPServerInput["server"]["type"]>(initial?.type ?? "stdio");
  const [command, setCommand] = useState(initial?.command ?? "");
  const [args, setArgs] = useState((initial?.args ?? []).join("\n"));
  const [url, setURL] = useState(initial?.url ?? "");
  const [pairs, setPairs] = useState<Pair[]>(() => initialPairs(editing));
  const [scope, setScope] = useState<MCPScope | null>(editing?.source ?? null);
  const [revealed, setRevealed] = useState(!editing);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const stdio = type === "stdio";
  const secret = pairs.find((p) => p.key.trim() && !VARIABLE.test(p.value));

  async function submit(e: FormEvent) {
    e.preventDefault();
    if (!scope) return;
    const values = Object.fromEntries(pairs.filter((p) => p.key.trim()).map((p) => [p.key.trim(), p.value.trim()]));
    const server: MCPServerInput["server"] = stdio
      ? { type, command: command.trim(), args: args.split("\n").map((a) => a.trim()).filter(Boolean), env: values }
      : { type, url: url.trim(), headers: values };
    setBusy(true);
    try {
      const list = editing ? await updateMCPServer(editing.name, editing.source, server) : await addMCPServer({ name: name.trim(), scope, server });
      onSaved(list, name.trim());
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
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
            <span className={styles.serverScope} data-scope={editing.source}>
              {SCOPE_LABELS[editing.source]}
            </span>
          </>
        ) : (
          "New MCP server"
        )}
      </div>
      {!editing && (
        <label className={ui.label}>
          NAME
          <input className={ui.input} value={name} onChange={(e) => setName(e.target.value)} placeholder="e.g. gitlab" pattern="[A-Za-z0-9_\-]+" required />
        </label>
      )}
      <div className={ui.label}>
        TYPE
        <div className={ui.seg} role="group" aria-label="Type">
          {(["stdio", "http", "sse"] as const).map((t) => (
            <button key={t} type="button" aria-pressed={type === t} onClick={() => setType(t)}>
              {t}
            </button>
          ))}
        </div>
      </div>
      {stdio && (
        <label className={ui.label}>
          COMMAND
          <input className={ui.input} value={command} onChange={(e) => setCommand(e.target.value)} placeholder="e.g. npx" required />
        </label>
      )}
      {revealed ? (
        stdio ? (
          <label className={ui.label}>
            ARGUMENTS, ONE PER LINE
            <textarea className={ui.textarea} value={args} onChange={(e) => setArgs(e.target.value)} placeholder={"-y\n@modelcontextprotocol/server-gitlab"} />
          </label>
        ) : (
          <label className={ui.label}>
            URL
            <input className={ui.input} value={url} onChange={(e) => setURL(e.target.value)} placeholder="https://example.com/mcp" required />
          </label>
        )
      ) : (
        <div className={ui.label}>
          {stdio ? "ARGUMENTS" : "URL"}
          <div className={styles.hidden}>
            <span>•••••••• hidden: {stdio ? "arguments" : "a URL"} may hold a secret</span>
            <button type="button" className={ui.link} onClick={() => setRevealed(true)}>
              Reveal
            </button>
          </div>
        </div>
      )}
      <div className={ui.label}>
        {stdio ? "ENVIRONMENT" : "HEADERS"}
        <span className={styles.scopeHelp}>
          Values refer to environment variables, never to a secret: {stdio ? "${GITLAB_TOKEN}" : "Bearer ${API_KEY}"}. Their values come from the project&apos;s
          variables (Settings, Environment) or from the environment you start the daemon from. They are never written to the server&apos;s file.
        </span>
        {pairs.map((p, i) => (
          <div key={i} className={styles.pair}>
            <input
              className={ui.input}
              aria-label="Name"
              value={p.key}
              placeholder={stdio ? "GITLAB_TOKEN" : "Authorization"}
              onChange={(e) => setPairs(pairs.map((x, j) => (j === i ? { ...x, key: e.target.value } : x)))}
            />
            <input
              className={ui.input}
              aria-label="Value"
              value={p.value}
              placeholder={p.clear ? "written in clear: replace it with ${VARIABLE}" : stdio ? "${GITLAB_TOKEN}" : "Bearer ${API_KEY}"}
              onChange={(e) => setPairs(pairs.map((x, j) => (j === i ? { ...x, value: e.target.value } : x)))}
            />
            <button type="button" className={ui.link} onClick={() => setPairs(pairs.filter((_, j) => j !== i))}>
              Remove
            </button>
          </div>
        ))}
        <div>
          <button type="button" className={ui.link} onClick={() => setPairs([...pairs, { key: "", value: "" }])}>
            + Add {stdio ? "a variable" : "a header"}
          </button>
        </div>
        {secret && (
          <span className={ui.error}>
            {secret.key}: {secret.clear ? "this value is written in clear in your configuration; " : ""}refer to an environment variable, e.g. ${"{"}
            {secret.key.toUpperCase().replace(/[^A-Z0-9_]/g, "_")}
            {"}"}. Secrets are never written.
          </span>
        )}
      </div>
      {!editing && (
        <fieldset className={styles.scopes}>
          <legend className={ui.label}>FOR WHOM</legend>
          {SCOPES.map((s) => (
            <label key={s.scope} className={styles.scope}>
              <input type="radio" name="mcp-scope" checked={scope === s.scope} onChange={() => setScope(s.scope)} />
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
        <button type="submit" className={ui.btnPrimary} disabled={busy || !scope || !name.trim() || Boolean(secret)}>
          {editing ? "Save" : "Add server"}
        </button>
        <button type="button" className={ui.link} onClick={onCancel}>
          Cancel
        </button>
      </div>
    </form>
  );
}

// initialPairs are the env or headers rows of the server being edited.
function initialPairs(editing?: MCPServerDetail): Pair[] {
  if (!editing) return [];
  const field = (editing.server.type ?? "stdio") === "stdio" ? "env" : "headers";
  const values = editing.server[field] ?? {};
  return Object.entries(values).map(([key, value]) => ({ key, value, clear: editing.clear_values.includes(`${field}.${key}`) }));
}
