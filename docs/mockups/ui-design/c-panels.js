// Kanban, Recruitment and Settings for the pixel office mockup (variant C).
// Sample data only; relies on Mock (shared.js) and Pixel (the office script).

(function () {
  "use strict";

  const TEAM_COLOR = { director: "#9b5266", specs: "#a07a5c", dev: "#4f8a64", review: "#4f6fa8" };
  const TEAM_NAME = { director: "Director", specs: "Specs", dev: "Dev", review: "Review" };

  const h = (tag, attrs, ...children) => {
    const n = document.createElement(tag);
    Object.entries(attrs || {}).forEach(([k, v]) => {
      if (v == null || v === false) return;
      if (k === "class") n.className = v;
      else if (k === "style") n.style.cssText = v;
      else if (k.startsWith("on")) n.addEventListener(k.slice(2), v);
      else if (k === "html") n.innerHTML = v;
      else n.setAttribute(k, v === true ? "" : v);
    });
    children.flat().forEach((c) => c != null && n.append(c.nodeType ? c : document.createTextNode(c)));
    return n;
  };
  const mini = (look) => h("span", { class: "mini", "aria-hidden": "true", html: Pixel.sprite(look) });
  const goTo = (tab) => document.querySelector('[data-tab="' + tab + '"]').click();

  // ==== Kanban ===========================================================

  const COLUMNS = [
    { id: "open", name: "Open" },
    { id: "in_progress", name: "In progress" },
    { id: "done", name: "Done" },
    { id: "canceled", name: "Canceled" },
  ];
  const TICKETS = [
    { id: "T-10", title: "Agree the scope of the CSV export", team: "director", who: "god", status: "done", ac: "2/2", out: 1 },
    { id: "T-11", title: "Spec: columns, date format and encoding of the export", team: "specs", who: "maya", status: "done", ac: "3/3", out: 1 },
    { id: "T-18", title: "Index report_rows by report_id", team: "dev", who: "alice", status: "done", ac: "1/1", out: 1 },
    { id: "T-12", title: "CSV writer for report rows (streaming)", team: "dev", who: "alice", status: "in_progress", ac: "2/3", needs: "T-11", note: "Waiting for your answer: separator" },
    { id: "T-13", title: "Download button on the reports page", team: "dev", who: "bob", status: "in_progress", ac: "1/3", flag: "APPROVAL" },
    { id: "T-14", title: "Review the CSV export", team: "review", who: "carol", status: "open", ac: "0/2", needs: "T-12, T-13" },
    { id: "T-16", title: "End-to-end check of the export with a 50k-row report", team: "review", who: null, status: "open", ac: "0/2", needs: "T-14" },
    { id: "T-09", title: "Export reports as PDF", team: "dev", who: null, status: "canceled", ac: "0/3", note: "Replaced by the CSV export" },
  ];

  function ticketCard(t, i) {
    const card = h("article", {
      class: "ticket" + (t.status === "done" ? " is-done" : ""),
      draggable: "true",
      "data-id": t.id,
      style: "--team:" + TEAM_COLOR[t.team] + ";--tilt:" + ((i % 3) - 1) * 1.2 + "deg",
      ondragstart: (e) => { e.dataTransfer.setData("text/plain", t.id); card.classList.add("dragging"); },
      ondragend: () => card.classList.remove("dragging"),
    },
      h("div", { class: "t-top" }, h("span", { class: "t-id" }, t.id), h("span", { class: "t-team" }, TEAM_NAME[t.team])),
      h("p", { class: "t-title" }, t.title),
      h("div", { class: "t-meta" },
        h("span", null, "AC " + t.ac),
        t.needs ? h("span", null, "needs " + t.needs) : null,
        t.out ? h("span", null, t.out + " deliverable") : null,
        t.note ? h("span", { class: t.status === "in_progress" ? "warn" : null }, t.note) : null),
      h("div", { class: "t-foot" },
        t.who
          ? h("button", { type: "button", class: "t-who", title: "Open " + t.who + "'s chat", onclick: () => Mock.select(t.who) }, mini(t.who), t.who)
          : h("span", { class: "t-free" }, "In the " + TEAM_NAME[t.team] + " queue"),
        t.flag ? h("span", { class: "t-flag" }, t.flag) : null));
    return card;
  }

  function renderKanban() {
    const root = document.getElementById("kanban");
    root.replaceChildren(
      h("div", { class: "board-bar" },
        h("h2", { class: "px-title" }, "Tickets"),
        h("span", { class: "feature-note" }, "Feature: CSV export for reports"),
        ...["All teams", "Specs", "Dev", "Review"].map((name, i) =>
          h("button", { type: "button", class: "chip" + (i === 0 ? " chip-on" : ""), onclick: (e) => filterTeam(e.currentTarget, i ? name.toLowerCase() : null) }, name)),
        h("span", { class: "spacer" }),
        h("button", { type: "button", class: "px-btn px-btn-primary" }, "+ New ticket")),
      h("div", { class: "columns" }, COLUMNS.map((c) => {
        const items = TICKETS.filter((t) => t.status === c.id);
        const col = h("section", {
          class: "col col-" + c.id, "data-status": c.id, "aria-label": c.name,
          ondragover: (e) => { e.preventDefault(); col.classList.add("over"); },
          ondragleave: () => col.classList.remove("over"),
          ondrop: (e) => {
            e.preventDefault();
            col.classList.remove("over");
            const t = TICKETS.find((x) => x.id === e.dataTransfer.getData("text/plain"));
            if (t) { t.status = c.id; renderKanban(); }
          },
        }, h("span", { class: "col-head" }, c.name + " ", h("b", null, String(items.length))), items.map(ticketCard));
        return col;
      })));
  }

  function filterTeam(btn, team) {
    btn.parentElement.querySelectorAll(".chip").forEach((c) => c.classList.toggle("chip-on", c === btn));
    document.querySelectorAll("#kanban .ticket").forEach((card) => {
      const t = TICKETS.find((x) => x.id === card.dataset.id);
      card.hidden = !!team && t.team !== team;
    });
  }

  // ==== Recruitment ======================================================

  const TEMPLATES = [
    { id: "god", look: "t-god", role: "Director", source: "built-in", version: 1, model: "sonnet", profile: "default", tools: ["Read", "Grep", "Glob"], mcp: [], director: true,
      description: "Talks with you, turns requests into features and tickets, leads the teams.",
      body: "You lead the organisation on behalf of the human. You do not write code yourself: the teams do." },
    { id: "archi", look: "t-archi", role: "Software architect", source: "built-in", version: 1, model: "sonnet", profile: "default", tools: ["Read", "Grep", "Glob"], mcp: [], suggest: "iris", team: "specs",
      description: "Designs the solution and writes short, justified technical specs.",
      body: "You design the solution: components, data model, API contracts.\nRead the existing code before proposing anything, and follow its conventions.\nKeep decisions short and justified." },
    { id: "dev", look: "t-dev", role: "Developer", source: "built-in", version: 1, model: "sonnet", profile: "allowlist", tools: ["Read", "Edit", "Write", "Grep", "Glob"], mcp: [], suggest: "frank", team: "dev",
      description: "Implements the specs with unit tests.",
      body: "You implement what the specs describe, with unit tests.\nRun the tests before delivering. When something is unclear, ask the specs team before guessing." },
    { id: "reviewer", look: "t-reviewer", role: "Code reviewer", source: "built-in", version: 1, model: "sonnet", profile: "default", tools: ["Read", "Grep", "Glob"], mcp: [], suggest: "gina", team: "review",
      description: "Reviews changes for bugs, missing tests and unclear code.",
      body: "Correctness first, then missing tests, then readability.\nReport each problem with the file, the line and a concrete failure scenario." },
    { id: "qa-engineer", look: "t-qa", role: "QA engineer", source: "yours", version: 2, model: "haiku", profile: "allowlist", tools: ["Read", "Bash", "Grep"], mcp: [], suggest: "hugo", team: "review",
      description: "Writes and runs end-to-end checks against the acceptance criteria.",
      body: "You check every delivered ticket against its acceptance criteria.\nWrite the check first, run it, and report what failed with the exact steps." },
    { id: "go-developer", look: "t-go", role: "Go developer", source: "yours", version: 3, model: "sonnet", profile: "allowlist", tools: ["Read", "Edit", "Write", "Bash", "Grep", "Glob"], mcp: ["gitlab"], suggest: "ivan", team: "dev",
      description: "Go code with table-driven tests, golangci-lint clean.",
      body: "Write idiomatic Go. Table-driven tests with t.Run for anything non-trivial.\nPass context.Context, never store it. Run golangci-lint before delivering." },
    { id: "doc-writer", look: "t-doc", role: "Technical writer", source: "yours", version: 1, model: "haiku", profile: "default", tools: ["Read", "Edit", "Grep"], mcp: ["ragflow"], suggest: "jude", team: "specs",
      description: "Keeps the user guide and the docs/ pages in step with the code.",
      body: "You update the user guide when a feature ships.\nShort sentences, real examples, no marketing." },
  ];
  const FROM = { god: "god@1", maya: "archi@1", alice: "go-developer@3", bob: "dev@1", carol: "reviewer@1", dan: "reviewer@1", erin: "qa-engineer@2" };
  const rec = { view: "catalog", picked: "dev", editing: null, confirm: null, note: null };

  function renderRecruitment() {
    const root = document.getElementById("recruitment");
    const tabs = [["catalog", "Catalog"], ["staff", "Staff"], ["new", "New agent"]];
    root.replaceChildren(
      h("div", { class: "rec-head" },
        h("div", null,
          h("h2", { class: "px-title" }, rec.view === "catalog" ? "Choose your recruit " : rec.view === "staff" ? "Your staff" : rec.editing ? "Edit " + rec.editing : "New agent", rec.view === "catalog" ? h("span", { class: "blink" }, "▶") : null),
          h("p", { class: "px-sub" }, rec.view === "catalog" ? "Templates from Nobody Inc and from ~/.nobody/catalog. A recruit is a copy: later template changes never touch it."
            : rec.view === "staff" ? "Everyone in the office. Changes apply once the agent is idle, never in the middle of a turn."
            : "Writes the agent file in .nobody/ and starts the agent.")),
        h("div", { class: "subtabs", role: "tablist" }, tabs.map(([id, label]) =>
          h("button", { type: "button", role: "tab", "aria-selected": String(rec.view === id), onclick: () => { rec.view = id; rec.editing = null; rec.note = null; rec.confirm = null; renderRecruitment(); } }, label)))),
      h("div", { class: "rec-body" }, rec.view === "catalog" ? catalogView() : rec.view === "staff" ? staffView() : editorView()));
  }

  function catalogView() {
    const roster = h("div", { class: "roster" });
    [["built-in", "Built in"], ["yours", "Your templates · ~/.nobody/catalog"]].forEach(([source, label]) => {
      roster.append(h("p", { class: "roster-group" }, label));
      TEMPLATES.filter((t) => t.source === source).forEach((t) => roster.append(
        h("button", { type: "button", class: "tile", "aria-pressed": String(rec.picked === t.id), onclick: () => { rec.picked = t.id; rec.note = null; renderRecruitment(); } },
          h("span", { class: "portrait", html: Pixel.sprite(t.look) }),
          h("span", { class: "tile-name" }, t.id),
          h("span", { class: "tile-role" }, t.role),
          h("span", { class: "tile-badge" + (t.source === "yours" ? " yours" : "") }, t.source === "yours" ? "Yours · v" + t.version : "Built in · v" + t.version))));
    });
    return h("div", { class: "select" }, roster, profileCard(TEMPLATES.find((t) => t.id === rec.picked)));
  }

  function profileCard(t) {
    const hasDirector = Mock.agents.some((a) => !a.team);
    const blocked = t.director && hasDirector;
    const teams = Mock.TEAMS.map((x) => h("option", { value: x.id, selected: x.id === t.team }, x.name));
    const form = h("form", { class: "recruit-form", onsubmit: (e) => {
      e.preventDefault();
      const name = form.querySelector("#rec-name").value.trim().toLowerCase();
      const team = form.querySelector("#rec-team");
      if (Mock.agents.some((a) => a.id === name)) { rec.note = { bad: true, text: "Someone is already called " + name + ". Pick another name." }; }
      else { rec.note = { text: name + " joins " + team.options[team.selectedIndex].text + " and walks in through the door." }; }
      renderRecruitment();
    } },
      h("label", { class: "px-label" }, "NAME", h("input", { class: "px-input", id: "rec-name", value: t.suggest || "", disabled: blocked, required: true, pattern: "[a-z][a-z0-9-]*" })),
      h("label", { class: "px-label" }, "TEAM", h("select", { class: "px-select", id: "rec-team", disabled: blocked }, t.director ? h("option", null, "Director (no team)") : teams)),
      h("div", { class: "actions" },
        h("button", { type: "submit", class: "px-btn px-btn-primary px-btn-big", disabled: blocked }, "Recruit ▶"),
        h("button", { type: "button", class: "px-btn", onclick: () => { rec.note = { text: "Copied to ~/.nobody/catalog/" + t.id + ".md" + (t.source === "built-in" ? ": it now replaces the built-in " + t.id + "." : ".") }; renderRecruitment(); } }, "Customize as my template")),
      blocked ? h("p", { class: "done-note", style: "border-color:var(--warn);color:var(--warn)" }, "The project already has its director (god). There is one per project.") : null,
      rec.note ? h("p", { class: "done-note", style: rec.note.bad ? "border-color:var(--danger);color:var(--danger)" : null }, rec.note.text) : null);

    return h("aside", { class: "profile", "aria-label": "Template " + t.id },
      h("div", { class: "stage", html: Pixel.sprite(t.look) }),
      h("div", { class: "profile-name" }, h("strong", null, t.id), h("span", null, t.role.toUpperCase())),
      h("p", null, t.description),
      h("dl", { class: "attrs" },
        h("dt", null, "MODEL"), h("dd", null, h("span", { class: "chip" }, t.model)),
        h("dt", null, "PERMISSIONS"), h("dd", null, h("span", { class: "chip" }, t.profile)),
        h("dt", null, "TOOLS"), h("dd", null, t.tools.map((x) => h("span", { class: "chip" }, x))),
        h("dt", null, "MCP"), h("dd", null, t.mcp.length ? t.mcp.map((x) => h("span", { class: "chip" }, x)) : h("span", { class: "chip" }, "none"))),
      h("pre", { class: "instructions" }, t.body),
      form);
  }

  function staffView() {
    const groups = [{ id: null, name: "Director" }].concat(Mock.TEAMS);
    return h("div", { class: "staff" }, groups.map((g) => {
      const members = Mock.agents.filter((a) => (a.team || null) === g.id);
      if (!members.length) return null;
      return h("section", { class: "staff-team" }, h("h3", null, g.name.toUpperCase() + " · " + members.length),
        members.map((a) => {
          const away = a.status === "stopped";
          const row = h("div", { class: "staff-row" },
            mini(a.id),
            h("div", { class: "staff-who" }, h("strong", null, a.name), h("span", null, a.role + " · " + a.ticket)),
            h("span", { class: "pill s-" + a.status }, Mock.STATUS_LABEL[a.status]),
            h("span", { class: "staff-from", title: "Recruited from this template (information only)" }, "from " + (FROM[a.id] || "scratch")),
            h("div", { class: "staff-actions" },
              h("button", { type: "button", class: "px-btn", onclick: () => Mock.select(a.id) }, "Chat"),
              h("button", { type: "button", class: "px-btn", onclick: () => { rec.view = "new"; rec.editing = a.id; renderRecruitment(); } }, "Edit"),
              h("button", { type: "button", class: "px-btn", onclick: () => leaveOrReturn(a, away) }, away ? "Return" : "Leave"),
              h("button", { type: "button", class: "px-btn px-btn-danger", onclick: () => { rec.confirm = a.id; renderRecruitment(); } }, "Dismiss")));
          if (rec.confirm === a.id) {
            row.append(h("div", { class: "confirm" },
              h("span", null, "Dismiss " + a.name + "? Their file is deleted; their ticket goes back to the " + (TEAM_NAME[a.team] || "") + " queue. History is kept."),
              h("button", { type: "button", class: "px-btn px-btn-danger", onclick: () => dismiss(a) }, "Dismiss " + a.name),
              h("button", { type: "button", class: "px-btn", onclick: () => { rec.confirm = null; renderRecruitment(); } }, "Cancel")));
          }
          return row;
        }));
    }));
  }

  function leaveOrReturn(a, away) {
    Mock.setStatus(a.id, away ? "idle" : "stopped", away ? "Back from leave" : "On leave");
    Pixel.refresh(a.id);
    renderRecruitment();
  }

  // A dismissal is shown in the office: the agent walks out through the door.
  function dismiss(a) {
    rec.confirm = null;
    goTo("office");
    setTimeout(() => { Pixel.walkOut(a.id); Mock.remove(a.id); renderRecruitment(); }, 250);
  }

  function editorView() {
    const a = rec.editing ? Mock.agent(rec.editing) : null;
    const tpl = a ? TEMPLATES.find((t) => FROM[a.id] && FROM[a.id].startsWith(t.id + "@")) : null;
    const tools = new Set(tpl ? tpl.tools : ["Read", "Grep"]);
    const mcp = new Set(tpl ? tpl.mcp : []);
    const chipToggles = (all, set) => h("div", { class: "chips" }, all.map((x) =>
      h("button", { type: "button", class: "chip" + (set.has(x) ? " chip-on" : ""), "aria-pressed": String(set.has(x)), onclick: (e) => {
        const on = e.currentTarget.classList.toggle("chip-on");
        e.currentTarget.setAttribute("aria-pressed", String(on));
      } }, x)));
    const seg = (all, current) => h("div", { class: "seg" }, all.map((x) =>
      h("button", { type: "button", "aria-pressed": String(x === current), onclick: (e) => e.currentTarget.parentElement.querySelectorAll("button").forEach((b) => b.setAttribute("aria-pressed", String(b === e.currentTarget))) }, x)));
    const note = h("p", { class: "editor-note" }, a
      ? "Changing the role or the instructions restarts " + a.name + " with a new session: a resumed session keeps its original instructions. Other changes keep its memory."
      : "The agent starts as soon as it is created.");
    const done = (text) => () => { note.textContent = text; note.style.color = "var(--ok)"; };
    return h("form", { class: "editor", onsubmit: (e) => e.preventDefault() },
      h("label", { class: "px-label" }, "NAME", h("input", { class: "px-input", id: "ed-name", value: a ? a.name : "", disabled: !!a, placeholder: "e.g. kim" })),
      h("label", { class: "px-label" }, "TEAM", h("select", { class: "px-select", id: "ed-team", disabled: !!a }, Mock.TEAMS.map((t) => h("option", { value: t.id, selected: a && a.team === t.id }, t.name)))),
      h("label", { class: "px-label" }, "ROLE", h("input", { class: "px-input", id: "ed-role", value: a ? a.role : "", placeholder: "One line, shown to colleagues" })),
      h("label", { class: "px-label" }, "MODEL", h("select", { class: "px-select", id: "ed-model" }, ["sonnet", "opus", "haiku", "Claude Code default"].map((m) => h("option", { selected: tpl && tpl.model === m }, m)))),
      h("div", { class: "px-label wide" }, "PERMISSIONS", seg(["default", "allowlist", "auto"], tpl ? tpl.profile : "default")),
      h("div", { class: "px-label wide" }, "TOOLS WITHOUT ASKING", chipToggles(["Read", "Edit", "Write", "Bash", "Grep", "Glob", "WebFetch"], tools)),
      h("div", { class: "px-label wide" }, "MCP SERVERS (from .nobody/mcp.yaml)", chipToggles(["gitlab", "ragflow"], mcp)),
      h("label", { class: "px-label wide" }, "INSTRUCTIONS", h("textarea", { class: "px-text", id: "ed-body" }, tpl ? tpl.body : "")),
      note,
      h("div", { class: "actions" }, a
        ? [h("button", { type: "button", class: "px-btn px-btn-primary", onclick: done("Saved. " + a.name + " restarts when idle, keeping its memory.") }, "Save, restart when idle"),
           h("button", { type: "button", class: "px-btn", onclick: done("Saved. " + a.name + " restarts with a new session when idle.") }, "Save, new session"),
           h("button", { type: "button", class: "px-btn", onclick: () => { rec.view = "staff"; rec.editing = null; renderRecruitment(); } }, "Cancel")]
        : [h("button", { type: "button", class: "px-btn px-btn-primary", onclick: done("Created. The new agent walks in through the door.") }, "Create and start"),
           h("button", { type: "button", class: "px-btn", onclick: done("Saved as a template in ~/.nobody/catalog/.") }, "Save as template instead")]));
  }

  // ==== Settings =========================================================

  const set = { section: "project" };

  function opt(label, help, control) {
    return h("div", { class: "opt" }, h("div", { class: "opt-label" }, h("strong", null, label), help ? h("span", null, help) : null), control);
  }
  function segment(values, current) {
    return h("div", { class: "seg" }, values.map((v) => h("button", { type: "button", "aria-pressed": String(v === current), onclick: (e) =>
      e.currentTarget.parentElement.querySelectorAll("button").forEach((b) => b.setAttribute("aria-pressed", String(b === e.currentTarget))) }, v)));
  }
  function toggle(on, label) {
    return h("button", { type: "button", class: "switch", role: "switch", "aria-label": label, "aria-checked": String(on), onclick: (e) =>
      e.currentTarget.setAttribute("aria-checked", String(e.currentTarget.getAttribute("aria-checked") !== "true")) });
  }

  function projectSection() {
    return h("section", { class: "set-section" },
      h("h2", null, "Project and daemon"),
      h("p", { class: "px-sub" }, "What this office runs on."),
      opt("Project folder", "The folder that holds .nobody/. Agents work here, or in their team's worktree during a feature.", h("code", null, "~/Documents/workspace/nobody-sandbox")),
      opt("Data on this machine", "Database and worktrees. Never committed, never shared.", h("code", null, "~/.nobody/projects/a3f9c1/")),
      opt("Daemon address", "Set when the daemon starts (NOBODY_ADDR). Loopback only.", h("code", null, "127.0.0.1:7420")),
      opt("Allowed origins", "Pages allowed to drive the daemon.", h("div", { class: "chips" },
        h("span", { class: "chip" }, "localhost:3000"), h("span", { class: "chip" }, "127.0.0.1:3000"), h("button", { type: "button", class: "px-btn" }, "+ Add"))),
      opt("When an agent's commits change .nobody/", "Agents cannot edit it directly; a shell command could. Block the delivery, or deliver it with a warning to you.", segment(["Block", "Warn"], "Block")),
      opt("Start every agent with the daemon", null, toggle(true, "Start every agent with the daemon")),
      opt("Claude Code executable", null, h("input", { class: "px-input", id: "set-claude", value: "claude", style: "width:180px" })));
  }

  const SERVERS = [
    { name: "gitlab", type: "stdio", how: "gitlab-mcp", env: [["GITLAB_TOKEN", true], ["GITLAB_URL", true]], users: ["god", "alice"] },
    { name: "ragflow", type: "http", how: "http://localhost:9380/mcp", env: [["RAGFLOW_API_KEY", false]], users: ["god"] },
  ];

  function mcpSection() {
    return h("section", { class: "set-section" },
      h("h2", null, "MCP servers"),
      h("p", { class: "px-sub" }, "Servers you can give to agents. Each agent only sees the ones listed in its file."),
      h("div", { class: "mcp" },
        SERVERS.map((s) => h("div", { class: "server" },
          h("div", { class: "server-name" }, s.name, h("span", { class: "server-type" }, s.type.toUpperCase())),
          h("div", { class: "staff-actions" }, h("button", { type: "button", class: "px-btn" }, "Edit"), h("button", { type: "button", class: "px-btn" }, "Remove")),
          h("div", { class: "server-how" }, s.how),
          h("div", { class: "server-env" }, s.env.map(([k, ok]) => h("span", { class: ok ? "env-ok" : "env-missing" }, (ok ? "✓ " : "✗ ") + k + (ok ? " set" : " missing from the environment")))),
          h("div", { class: "server-users" }, "Used by", s.users.map((u) => mini(u)), s.users.join(", ")))),
        h("div", { class: "server server-locked" },
          h("div", { class: "server-name" }, "nobody", h("span", { class: "server-type" }, "BUILT IN")),
          h("span", { class: "chip" }, "Reserved"),
          h("div", { class: "server-how" }, "The office: mailboxes, tickets, the director's tools."),
          h("div", { class: "server-users" }, "Given to every agent")),
        h("div", null, h("button", { type: "button", class: "px-btn px-btn-primary" }, "+ Add a server"))),
      h("p", { class: "set-foot" }, "Saved to .nobody/mcp.yaml, which is committed with the project: secrets stay in environment variables, written as ${NAME}."));
  }

  function appearanceSection() {
    return h("section", { class: "set-section" },
      h("h2", null, "Appearance"),
      h("p", { class: "px-sub" }, "How the office looks on this computer."),
      opt("Theme", null, segment(["Dusk", "Daylight"], "Dusk")),
      opt("Animations", "Reduced keeps statuses and paper planes on straight paths, without loops, walking or confetti.", segment(["Full", "Reduced", "Off"], "Full")),
      opt("Labels on paper planes", "Show who writes to whom and the subject.", toggle(true, "Labels on paper planes")));
  }

  function renderSettings() {
    const root = document.getElementById("settings");
    const sections = [["project", "Project and daemon", projectSection], ["mcp", "MCP servers", mcpSection], ["appearance", "Appearance", appearanceSection]];
    const current = sections.find((s) => s[0] === set.section);
    root.replaceChildren(h("div", { class: "settings" },
      h("nav", { class: "set-nav", "aria-label": "Settings sections" }, sections.map(([id, label]) =>
        h("button", { type: "button", "aria-current": String(id === set.section), onclick: () => { set.section = id; renderSettings(); } }, label))),
      current[2]()));
  }

  renderKanban();
  renderRecruitment();
  renderSettings();
  // Keep the staff list in step with arrivals in the office.
  document.addEventListener("mock:arrive", () => { if (rec.view === "staff") renderRecruitment(); });
  // Open a tab from the address: c-pixel-office.html#kanban, #recruitment, #settings;
  // #bin opens the wastebasket in the chat.
  document.addEventListener("DOMContentLoaded", () => {
    const [tab, sub] = location.hash.slice(1).split("-");
    const btn = tab && document.querySelector('[data-tab="' + tab + '"]');
    if (btn) btn.click();
    if (tab === "recruitment" && sub) { rec.view = sub; renderRecruitment(); }
    if (tab === "bin") Mock.select("bin");
  });
})();
