// Shared sample data and behaviour for the three UI mockups.
//
// Every mockup has the same layout: a top bar with tabs, a chat panel on the
// left (avatar rail + thread of the selected agent) and the office in the main
// area. This file renders the rail, the thread, the tabs and the feature
// banner, and plays a scripted scene. Each mockup draws its own office and
// listens to these events on `document`:
//   mock:select  {id}                 an agent was selected (rail or office)
//   mock:message {from, to, label, reply?}  an agent sends a message; a page
//                that animates it can send `reply` back the same way on arrival
//   mock:arrive  {id}                 a newly recruited agent joins the office
// Clicking an agent in the office must call Mock.select(id).

(function () {
  "use strict";

  const TEAMS = [
    { id: "specs", name: "Specs", goal: "Turn requests into specs" },
    { id: "dev", name: "Dev", goal: "Build and test" },
    { id: "review", name: "Review", goal: "Review, security, QA" },
  ];

  // status: working | idle | awaiting_approval | stopped
  const AGENTS = [
    { id: "god", name: "god", role: "Director", team: null, status: "working", ticket: "Planning next feature", badge: { kind: "count", value: 1 } },
    { id: "maya", name: "maya", role: "Spec writer", team: "specs", status: "idle", ticket: "T-11 done" },
    { id: "alice", name: "alice", role: "Go developer", team: "dev", status: "working", ticket: "T-12 CSV writer", badge: { kind: "urgent", value: "!" } },
    { id: "bob", name: "bob", role: "Frontend developer", team: "dev", status: "awaiting_approval", ticket: "T-13 Download button", badge: { kind: "approval", value: "1" } },
    { id: "carol", name: "carol", role: "Reviewer", team: "review", status: "idle", ticket: "Waiting for T-12, T-13" },
    { id: "dan", name: "dan", role: "Security reviewer", team: "review", status: "stopped", ticket: "On leave" },
  ];

  // Recruited a few seconds after load, to show the arrival animation.
  const NEWCOMER = { id: "erin", name: "erin", role: "QA engineer", team: "review", status: "idle", ticket: "Just joined", badge: { kind: "count", value: 1 } };

  // Messages between agents end up in the wastebasket once delivered, like
  // paper planes thrown in the bin. It is read-only: the chat is between you
  // and one agent, the full story of an agent is in its terminal.
  const BIN = [
    { from: "god", to: "maya", t: "09:21", text: "T-11 is yours: columns, date format, encoding." },
    { from: "maya", to: "god", t: "09:39", text: "T-11 delivered: docs/specs/csv-export.md", reply: true },
    { from: "god", to: "alice", t: "09:41", text: "T-12 is yours once T-11 is in." },
    { from: "alice", to: "maya", t: "09:52", text: "Encoding: UTF-8 with a BOM, for Excel?" },
    { from: "maya", to: "alice", t: "09:55", text: "Yes, UTF-8 with a BOM. Added to the spec.", reply: true },
    { from: "bob", to: "alice", t: "10:01", text: "What will the endpoint be called?" },
    { from: "alice", to: "bob", t: "10:02", text: "GET /api/reports/{id}/export.csv", reply: true },
  ];

  const STATUS_LABEL = {
    working: "Working",
    idle: "Idle",
    awaiting_approval: "Needs approval",
    stopped: "On leave",
  };

  const THREADS = {
    god: [
      { from: "you", t: "09:12", text: "Users keep asking to download the reports page. Can we add a CSV export?" },
      { from: "god", t: "09:14", text: "Yes. I wrote it up as “CSV export for reports” with three acceptance criteria and split it into 7 tickets: spec (maya), CSV writer (alice), download button (bob), review (carol)." },
      { from: "you", t: "09:20", text: "Started. Keep me posted." },
      { from: "god", t: "10:02", text: "While reading the access logs I saw one client calling /api/reports 4,000 times an hour. I suggest we rate-limit the public API next." },
      { from: "god", t: "10:03", card: "proposal", title: "Rate-limit the public API", ac: ["429 with Retry-After above 60 requests/min per token", "Limits configurable per plan", "Load test in CI"] },
    ],
    alice: [
      { from: "alice", t: "09:31", text: "Took T-12 “CSV writer for report rows”. I stream the rows with encoding/csv so large reports never sit in memory." },
      { from: "alice", t: "10:05", card: "urgent", text: "Separator: comma or semicolon? French Excel opens comma files as one single column.", options: ["Semicolon", "Comma", "Let the user choose"] },
    ],
    bob: [
      { from: "bob", t: "09:48", text: "T-13 “Download button”: the button is in place, I’m wiring the request." },
      { from: "bob", t: "10:06", card: "approval", tool: "Bash", cmd: "pnpm add papaparse@5.4.1", why: "Parse the file for the preview before download." },
    ],
    maya: [
      { from: "maya", t: "09:40", text: "T-11 done: the spec is in docs/specs/csv-export.md. Columns and date format (ISO 8601) agreed with god." },
    ],
    carol: [
      { from: "carol", t: "09:50", text: "Nothing to review yet. T-14 waits for T-12 and T-13." },
    ],
    dan: [{ from: "system", text: "dan is on leave. Messages are kept until they return." }],
    erin: [
      { from: "system", text: "erin joined Review from the qa-engineer@2 template." },
      { from: "erin", t: "now", text: "Hi! I’ll write the end-to-end checks for the CSV export once T-14 is reviewed." },
    ],
  };

  const TERMINAL = [
    "● Read internal/reports/export.go",
    "● Edit internal/reports/csv.go  (+48 −3)",
    "● Bash(go test ./internal/reports/...)",
    "  ok   github.com/acme/reports/internal/reports  0.41s",
    "● ticket_comment T-12 “Writer done, separator pending”",
    "✳ Thinking…",
  ];

  // The scene: messages between agents, played in a loop.
  // `reply` is sent back by the recipient once the message has arrived.
  const EXCHANGES = [
    { from: "god", to: "maya", label: "T-15 spec: rate limits", reply: "On it, draft by noon" },
    { from: "alice", to: "carol", label: "T-12 ready for review", reply: "Looking now" },
    { from: "bob", to: "alice", label: "Which column order?", reply: "Date, client, amount" },
    { from: "carol", to: "god", label: "T-12 approved", reply: "Thanks, merging" },
    { from: "maya", to: "god", label: "Spec draft ready", reply: "Reading it" },
    { from: "god", to: "bob", label: "Priority: T-13", reply: "Understood" },
  ];

  const state = { selected: "god", view: "chat", agents: AGENTS.slice(), binUnread: 0 };

  const $ = (sel, root = document) => root.querySelector(sel);
  const el = (tag, cls, text) => {
    const node = document.createElement(tag);
    if (cls) node.className = cls;
    if (text != null) node.textContent = text;
    return node;
  };

  function agent(id) {
    return state.agents.find((a) => a.id === id) || (id === NEWCOMER.id ? NEWCOMER : null);
  }

  function avatarIndex(id) {
    const order = ["god", "maya", "alice", "bob", "carol", "dan", "erin"];
    const i = order.indexOf(id);
    return i < 0 ? 0 : i % 7;
  }

  function avatar(a, size) {
    const node = el("span", "avatar" + (size ? " avatar-" + size : ""), a.name.slice(0, 1).toUpperCase());
    node.style.setProperty("--av", "var(--av-" + avatarIndex(a.id) + ")");
    node.dataset.status = a.status;
    return node;
  }

  function teamName(a) {
    if (!a.team) return "Leads every team";
    const t = TEAMS.find((x) => x.id === a.team);
    return t ? t.name : a.team;
  }

  // ---- Rail ---------------------------------------------------------------

  function renderRail() {
    const rail = $("#rail");
    if (!rail) return;
    rail.replaceChildren();
    state.agents.forEach((a, i) => {
      const btn = el("button", "rail-item");
      btn.type = "button";
      btn.dataset.agent = a.id;
      btn.title = a.name + " · " + a.role + " · " + STATUS_LABEL[a.status];
      btn.setAttribute("aria-label", btn.title);
      if (a.id === state.selected) btn.setAttribute("aria-current", "true");
      if (a.fresh) btn.classList.add("fresh");
      btn.append(avatar(a));
      btn.append(el("span", "rail-dot s-" + a.status));
      if (a.badge) btn.append(el("span", "rail-badge b-" + a.badge.kind, String(a.badge.value)));
      btn.append(el("span", "rail-name", a.name));
      btn.addEventListener("click", () => select(a.id));
      rail.append(btn);
      if (i === 0) rail.append(el("span", "rail-sep"));
    });
    rail.append(el("span", "rail-sep rail-sep-bottom"));
    const bin = el("button", "rail-item rail-bin");
    bin.type = "button";
    bin.dataset.agent = "bin";
    bin.title = "Wastebasket \u00b7 messages between agents";
    bin.setAttribute("aria-label", bin.title);
    if (state.selected === "bin") bin.setAttribute("aria-current", "true");
    bin.append(binIcon());
    if (state.binUnread) bin.append(el("span", "rail-badge b-count", String(state.binUnread)));
    bin.append(el("span", "rail-name", "bin"));
    bin.addEventListener("click", () => select("bin"));
    rail.append(bin);
  }

  function binIcon() {
    const icon = el("span", "avatar avatar-bin");
    icon.innerHTML = '<svg viewBox="0 0 12 12" width="22" height="22" shape-rendering="crispEdges" aria-hidden="true"><rect x="2" y="3" width="8" height="8" fill="currentColor" opacity=".55"/><rect x="1" y="2" width="10" height="1" fill="currentColor"/><rect x="3" y="4" width="1" height="6" fill="currentColor"/><rect x="5" y="4" width="1" height="6" fill="currentColor"/><rect x="7" y="4" width="1" height="6" fill="currentColor"/><rect x="3" y="0" width="3" height="2" fill="#fffaf0"/><rect x="6" y="1" width="2" height="1" fill="#e9dfcc"/></svg>';
    return icon;
  }

  // ---- Thread -------------------------------------------------------------

  function renderThread() {
    const root = $("#thread");
    if (!root) return;
    if (state.selected === "bin") return renderBin(root);
    const a = agent(state.selected);
    root.replaceChildren();

    const head = el("header", "thread-head");
    head.append(avatar(a, "lg"));
    const who = el("div", "thread-who");
    who.append(el("strong", "thread-name", a.name));
    who.append(el("span", "thread-role", a.role + " · " + teamName(a)));
    head.append(who);
    head.append(el("span", "pill s-" + a.status, STATUS_LABEL[a.status]));
    root.append(head);

    const toggle = el("div", "view-toggle");
    toggle.setAttribute("role", "tablist");
    ["chat", "terminal"].forEach((v) => {
      const b = el("button", "view-btn", v === "chat" ? "Chat" : "Terminal");
      b.type = "button";
      b.setAttribute("role", "tab");
      b.setAttribute("aria-selected", String(state.view === v));
      b.addEventListener("click", () => { state.view = v; renderThread(); });
      toggle.append(b);
    });
    root.append(toggle);

    const body = el("div", "thread-body");
    if (state.view === "terminal") {
      const pre = el("pre", "term");
      pre.textContent = a.status === "stopped"
        ? "This agent is on leave. Its session is kept."
        : "$ claude --resume " + a.id + "-7f3c\n\n" + TERMINAL.join("\n");
      body.append(pre);
    } else {
      (THREADS[a.id] || []).forEach((m) => body.append(renderMessage(m, a)));
    }
    root.append(body);

    const form = el("form", "composer");
    const input = el("input", "composer-input");
    input.id = "composer-input";
    input.placeholder = a.status === "stopped" ? "Leave a message for " + a.name + "…" : "Message " + a.name + "…";
    input.setAttribute("aria-label", input.placeholder);
    const send = el("button", "composer-send", "Send");
    send.type = "submit";
    form.append(input, send);
    form.addEventListener("submit", (e) => {
      e.preventDefault();
      if (!input.value.trim()) return;
      (THREADS[a.id] = THREADS[a.id] || []).push({ from: "you", t: "now", text: input.value.trim() });
      renderThread();
      const field = $("#composer-input");
      if (field) field.focus();
    });
    root.append(form);
    body.scrollTop = body.scrollHeight;
  }

  // The wastebasket: delivered messages between agents, as crumpled sheets of
  // paper unfolded again. Read-only.
  function renderBin(root) {
    root.replaceChildren();
    const head = el("header", "thread-head");
    head.append(binIcon());
    const who = el("div", "thread-who");
    who.append(el("strong", "thread-name", "Wastebasket"));
    who.append(el("span", "thread-role", "Messages between agents, once delivered"));
    head.append(who);
    head.append(el("span", "pill s-idle", BIN.length + " sheets"));
    root.append(head);
    const body = el("div", "thread-body bin-body");
    BIN.forEach((m, i) => body.append(paper(m, i)));
    root.append(body);
    root.append(el("p", "bin-foot", "Read-only. What an agent did in full is in its terminal."));
    body.scrollTop = body.scrollHeight;
  }

  function paper(m, i) {
    const sheet = el("article", "paper" + (m.reply ? " paper-reply" : ""));
    sheet.style.setProperty("--tilt", ((i * 7) % 5 - 2) * 0.8 + "deg");
    sheet.style.animationDelay = Math.min(i, 8) * 60 + "ms";
    const top = el("div", "paper-top");
    top.append(el("span", "paper-who", m.from + " \u2192 " + m.to));
    top.append(el("span", "paper-time", m.t));
    sheet.append(top);
    sheet.append(el("p", "paper-text", m.text));
    if (m.reply) sheet.append(el("span", "paper-tag", "reply"));
    return sheet;
  }

  function trash(m) {
    const now = new Date();
    BIN.push(Object.assign({ t: String(now.getHours()).padStart(2, "0") + ":" + String(now.getMinutes()).padStart(2, "0") }, m));
    if (state.selected === "bin") {
      const body = $(".bin-body");
      if (body) { body.append(paper(BIN[BIN.length - 1], BIN.length - 1)); body.scrollTop = body.scrollHeight; }
      const count = $("#thread .thread-head .pill");
      if (count) count.textContent = BIN.length + " sheets";
    } else {
      state.binUnread += 1;
    }
    renderRail();
    document.dispatchEvent(new CustomEvent("mock:bin", { detail: { unread: state.binUnread } }));
  }

  function renderMessage(m, a) {
    if (m.from === "system") return el("p", "msg-system", m.text);
    const out = m.from === "you";
    const wrap = el("div", "msg " + (out ? "out" : "in"));
    if (m.card === "approval") {
      const card = el("div", "card card-approval");
      card.append(el("span", "card-kicker", "Permission request"));
      card.append(el("p", "card-text", a.name + " wants to run"));
      card.append(el("code", "card-code", m.tool + "(" + m.cmd + ")"));
      card.append(el("p", "card-note", m.why));
      card.append(actions(["Allow once", "Always allow", "Deny"], "Allowed once"));
      wrap.append(card);
    } else if (m.card === "urgent") {
      const card = el("div", "card card-urgent");
      card.append(el("span", "card-kicker", "Urgent question"));
      card.append(el("p", "card-text", m.text));
      card.append(actions(m.options, null));
      wrap.append(card);
    } else if (m.card === "proposal") {
      const card = el("div", "card card-proposal");
      card.append(el("span", "card-kicker", "Feature proposal"));
      card.append(el("strong", "card-title", m.title));
      const ul = el("ul", "card-list");
      m.ac.forEach((x) => ul.append(el("li", null, x)));
      card.append(ul);
      card.append(actions(["Start", "Refuse…"], "Started"));
      wrap.append(card);
    } else {
      wrap.append(el("p", "bubble", m.text));
    }
    if (m.t) wrap.append(el("span", "msg-time", m.t));
    return wrap;
  }

  function actions(labels, doneLabel) {
    const row = el("div", "card-actions");
    labels.forEach((label, i) => {
      const b = el("button", "btn" + (i === 0 ? " btn-primary" : ""), label);
      b.type = "button";
      b.addEventListener("click", () => {
        row.replaceChildren(el("span", "card-done", doneLabel && i === 0 ? doneLabel : "Answered: " + label));
      });
      row.append(b);
    });
    return row;
  }

  // ---- Tabs and banner ----------------------------------------------------

  function initTabs() {
    document.querySelectorAll("[data-tab]").forEach((tab) => {
      tab.addEventListener("click", () => {
        document.querySelectorAll("[data-tab]").forEach((t) => t.setAttribute("aria-selected", String(t === tab)));
        document.querySelectorAll("[data-panel]").forEach((p) => { p.hidden = p.dataset.panel !== tab.dataset.tab; });
      });
    });
  }

  function renderBanner() {
    const root = $("#banner");
    if (!root) return;
    root.innerHTML =
      '<div class="banner-feature">' +
      '<span class="banner-kicker">Active feature</span>' +
      '<strong class="banner-title">CSV export for reports</strong>' +
      '<span class="banner-meta"><code>nobody/csv-export</code> · 3 of 7 tickets done</span>' +
      '<span class="banner-progress" aria-hidden="true"><span style="width:43%"></span></span>' +
      "</div>" +
      '<button type="button" class="banner-proposal" data-open="god">' +
      '<span class="banner-kicker">Proposal from god</span>' +
      "<span>Rate-limit the public API</span>" +
      "</button>";
    root.querySelector("[data-open]").addEventListener("click", () => select("god"));
  }

  // ---- Selection and scene ------------------------------------------------

  function select(id) {
    if (id !== "bin" && !agent(id)) return;
    state.selected = id;
    state.view = "chat";
    if (id === "bin") {
      state.binUnread = 0;
      document.dispatchEvent(new CustomEvent("mock:bin", { detail: { unread: 0 } }));
    } else {
      const a = agent(id);
      if (a.badge && a.badge.kind === "count") delete a.badge;
    }
    renderRail();
    renderThread();
    document.dispatchEvent(new CustomEvent("mock:select", { detail: { id } }));
  }

  function playScene() {
    const reduced = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    let i = 0;
    setTimeout(function next() {
      const ex = EXCHANGES[i % EXCHANGES.length];
      document.dispatchEvent(new CustomEvent("mock:message", { detail: ex }));
      i += 1;
      setTimeout(next, reduced ? 7000 : 5200);
    }, 1200);
    setTimeout(() => {
      const a = Object.assign({ fresh: true }, NEWCOMER);
      state.agents.push(a);
      renderRail();
      document.dispatchEvent(new CustomEvent("mock:arrive", { detail: { id: a.id } }));
    }, 4500);
  }

  // Used by the Recruitment mockup: put an agent on leave or back, dismiss it.
  function setStatus(id, status, ticket) {
    const a = agent(id);
    if (!a) return;
    a.status = status;
    if (ticket) a.ticket = ticket;
    renderRail();
    if (state.selected === id) renderThread();
  }

  function remove(id) {
    state.agents = state.agents.filter((a) => a.id !== id);
    if (state.selected === id) state.selected = "god";
    renderRail();
    renderThread();
  }

  window.Mock = {
    TEAMS, AGENTS, NEWCOMER, STATUS_LABEL, agent, avatarIndex, select, setStatus, remove, trash,
    get agents() { return state.agents.slice(); },
    get selected() { return state.selected; },
  };

  document.addEventListener("DOMContentLoaded", () => {
    initTabs();
    renderRail();
    renderThread();
    renderBanner();
    document.dispatchEvent(new CustomEvent("mock:ready"));
    document.dispatchEvent(new CustomEvent("mock:select", { detail: { id: state.selected } }));
    playScene();
  });
})();
