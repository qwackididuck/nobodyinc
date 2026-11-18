"use client";

import { useEffect, useState } from "react";

import { listMCPServers, listMCPTools, type MCPServer, type MCPTool } from "@/lib/api";
import ui from "../ui/ui.module.css";
import styles from "./staff.module.css";

interface Props {
  // The agent's mcp_exclude: servers ("gitlab") and tools ("gitlab/delete_project").
  value: string[];
  onChange: (value: string[]) => void;
}

// MCPExclusions chooses what an agent does not get among the MCP servers
// every agent gets (ADR-0010): a whole server, or some of its tools, listed
// by starting the server. Exclusions naming a server absent from this machine
// are kept, and shown as such: another developer may have that server.
export function MCPExclusions({ value, onChange }: Props) {
  const [servers, setServers] = useState<MCPServer[] | null>(null);

  useEffect(() => {
    let cancelled = false;
    listMCPServers()
      .then((s) => !cancelled && setServers(s))
      .catch(() => !cancelled && setServers([]));
    return () => {
      cancelled = true;
    };
  }, []);

  const names = servers?.map((s) => s.name) ?? [];
  const elsewhere = value.filter((e) => !names.includes(e.split("/")[0]));

  return (
    <div className={`${ui.label} ${styles.wide}`}>
      MCP SERVERS
      <span className={styles.hint}>Every agent gets every server below. Uncheck what this one should not get.</span>
      {servers?.length === 0 && <span className={styles.hint}>No MCP server in the project or in your Claude Code configuration.</span>}
      <div className={styles.mcpServers}>
        {servers?.map((s) => (
          <ServerChoice key={s.name} server={s} value={value} onChange={onChange} />
        ))}
      </div>
      {servers && elsewhere.length > 0 && (
        <div className={styles.chips}>
          <span className={styles.hint}>Also excluded, for servers absent from this machine:</span>
          {elsewhere.map((e) => (
            <button key={e} type="button" className={`${ui.chip} ${ui.chipOn}`} title="Click to drop this exclusion" onClick={() => onChange(value.filter((x) => x !== e))}>
              {e} ✕
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

// ServerChoice is one server: given or not, and when given, its tools on
// demand, each given or not.
function ServerChoice({ server, value, onChange }: { server: MCPServer; value: string[]; onChange: (value: string[]) => void }) {
  const [tools, setTools] = useState<MCPTool[] | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const given = !value.includes(server.name);
  const prefix = `${server.name}/`;
  const excludedTools = value.filter((e) => e.startsWith(prefix)).length;

  function setGiven(on: boolean) {
    // Excluding the whole server makes its tool exclusions pointless.
    onChange(on ? value.filter((e) => e !== server.name) : [...value.filter((e) => !e.startsWith(prefix)), server.name]);
  }

  function setToolGiven(tool: string, on: boolean) {
    const entry = prefix + tool;
    onChange(on ? value.filter((e) => e !== entry) : [...value, entry]);
  }

  async function loadTools() {
    setLoading(true);
    try {
      setTools(await listMCPTools(server.name));
      setError(null);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setLoading(false);
    }
  }

  return (
    <div className={styles.mcpServer}>
      <div className={styles.mcpServerHead}>
        <label>
          <input type="checkbox" checked={given} onChange={(e) => setGiven(e.target.checked)} /> <strong>{server.name}</strong>
        </label>
        <span className={styles.hint}>
          {server.type} · {server.source}
          {given && excludedTools > 0 && ` · ${excludedTools} tool${excludedTools > 1 ? "s" : ""} excluded`}
        </span>
        {given && !tools && (
          <button type="button" className={ui.link} disabled={loading} onClick={() => void loadTools()}>
            {loading ? "Starting the server…" : "Choose tools"}
          </button>
        )}
      </div>
      {error && <span className={ui.error}>{error}</span>}
      {given && tools && (
        <div className={styles.mcpTools}>
          {tools.length === 0 && <span className={styles.hint}>This server offers no tool.</span>}
          {tools.map((t) => (
            <label key={t.name} className={styles.mcpTool} title={t.description}>
              <input type="checkbox" checked={!value.includes(prefix + t.name)} onChange={(e) => setToolGiven(t.name, e.target.checked)} />
              <code>{t.name}</code>
              {t.description && <span className={styles.hint}>{t.description}</span>}
            </label>
          ))}
        </div>
      )}
    </div>
  );
}
