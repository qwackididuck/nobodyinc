# ADR-0001 — Build our own agent-teams tool

- Status: accepted
- Date: 2026-11-11

## Context

We want several teams of Claude Code agents (architecture + security specs, development, BDD validation, pentest, performance, documentation) to work together, able to talk within a team and across teams, with per-ticket traceability and human approvals, all supervised from a graphical interface.

Tools evaluated (November 2026):

| Tool | Why it does not fit on its own |
|---|---|
| Paperclip | Tested: sequential, driven by periodic heartbeats and tickets, slow for exchanges |
| OpenRig (+ Herdr) | Solid `role@team` messaging, but a terminal-centric interface, with no budgets or governance |
| munder-difflin | Good message protocol (the "hive"), but "The Office" theming, no notion of team, messages delivered only at the end of a turn |
| oh-my-claudecode | Depends on the experimental Agent Teams flag |
| Orca, agent-orchestrator, omnigent, Orkas | Agents run in parallel, without talking to each other |
| MCP Agent Mail, BMAD, LangGraph, CrewAI… | Partial building blocks (messaging only, method only, or outside Claude Code) |

## Decision

Build a dedicated tool that reuses the best existing ideas:
- OpenRig's teams and `role@team` addressing;
- the hive's message protocol (FIPA-inspired acts, hop cap, trays);
- adding an urgent tray (session fork, mid-turn injection), a no-code blueprint with one-way channels, a god session with calls for tender, and tickets with deliverables.

The tool stays a thin layer on top of Claude Code, relying on its documented interfaces (hooks, CLI options, OTel).

## Consequences

- We accept the cost of building and maintaining it.
- Dependence on Claude Code changes is contained by an isolated runtime adapter and contract tests.
- If Agent Teams becomes stable, it can become an additional runtime rather than a competitor.
