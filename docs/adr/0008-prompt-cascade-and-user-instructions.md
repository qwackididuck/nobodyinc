# ADR-0008 — The prompt cascade, and the user's CLAUDE.md as text

- Status: accepted; the user's MCP servers reach the agents since ADR-0010 (their settings, hooks and plugins still do not)
- Date: 2026-11-16
- Amends: ADR-0003

## Context

An agent's system prompt held Nobody Inc's protocol, its team's goal, its role and its own instructions. Two levels were missing:

- **What the whole project or a whole team should know** (the stack, the conventions, how the team works). It had to be copied into every agent file, and kept in sync by hand.
- **The user's own instructions to Claude**, in their `~/.claude/CLAUDE.md`: how they want code written, their commit rules, their tools. ADR-0003 starts agents with `--setting-sources project,local`, which leaves out the user's settings, hooks and plugins (the incident that led to it), and with them that file.

The user wants their instructions to reach the agents, but read-only: nothing in them may run or configure anything.

## Decision

1. **A cascade of prompts**, from the general to the particular, rendered in this order into the system prompt (`internal/runtime/prompt.tmpl`):
   1. Nobody Inc and who the agent is;
   2. the user's instructions: the text of `~/.claude/CLAUDE.md` (or `$CLAUDE_CONFIG_DIR/CLAUDE.md`, as Claude Code does);
   3. the project prompt: `.nobody/project.md`;
   4. the team: its goal (`org.yaml`) and its prompt, `.nobody/teams/<team>/TEAM.md`;
   5. the agent's role and instructions (its file's body);
   6. the organization chart, how to communicate, and whom to ask.

   A level with nothing to say is left out. The project and team prompts are files in `.nobody/`, committed with the project like the rest (ADR-0004), and written by the daemon on behalf of the control room and the director.
2. **The user's CLAUDE.md is given as text, nothing more.** The daemon reads it at each agent launch (`internal/daemon/userinstructions.go`) and puts it in the system prompt, introduced as the user's general instructions, which yield to Nobody Inc's rules where they differ (communication, tickets, permissions). `--setting-sources project,local` does not change: the user's settings, hooks, plugins and MCP servers stay out of the agents, as ADR-0003 decided. Its `@imports` are not resolved: an agent sees the line, not the file it points to.
3. **A change to a shared prompt needs a fresh session.** A resumed Claude Code session keeps the system prompt it started with: when the project prompt, a team's goal or a team's prompt changes, the agents it concerns restart with a fresh session once idle, as for a change of their own instructions (`staffing.sharedPrompt`). An edit to the user's CLAUDE.md is picked up by the agents that start afterwards; running agents keep the text they started with.

## Consequences

- Shared context is written once; agent templates stay generic, about a trade, not a project.
- The user's instructions may say things that make no sense for an agent (a tool it does not have, "ask me before committing"). The prompt tells the agent that Nobody Inc's rules win and that what the text mentions is not set up for it; the user can say more in the project prompt.
- Everything the agents are told is in files a human can read: `.nobody/` for the project, and the user's own CLAUDE.md.
