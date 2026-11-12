# ADR-0002 — Stack: Go daemon, Next.js UI, SQLite, Claude Code CLI in a PTY

- Status: accepted
- Date: 2026-11-11

## Context

The tool must start and supervise many agent sessions, route messages, receive hooks, expose an MCP server and run both locally and 24/7 on a VPS, with a supervision interface reachable remotely.

## Decision

- **Go daemon** (`cmd/nobodyd`): a single binary, many PTYs and streams in parallel thanks to goroutines, a small memory footprint. A browser can neither start processes, nor access git, nor stay active once closed: a permanent backend is therefore required.
- **Next.js UI** (`web/`, App Router, strict TypeScript), the single client for both local and VPS modes.
- **SQLite** as the source of truth (state, messages, tickets, event log); decisions and deliverables are exported as versioned markdown.
- **`claude` CLI started in a PTY** rather than the Agent SDK: it reuses Claude Code as is, and lets the human take over in an agent's terminal. Structured data comes from hooks and OTel, never from reading the screen; reading JSONL transcripts (an undocumented internal format) is optional and isolated.
- The daemon listens on `127.0.0.1` by default: it starts processes and must not be exposed without authentication.

## Consequences

- Two languages (Go, TypeScript), in line with the usual stack.
- How the UI is served in production (static export served by Go, or a separate Next.js server) is still to be decided.
- A fake `claude` runtime (`test/fakeclaude`) runs end-to-end scenarios without API costs; the real CLI is covered by the contract tests.
