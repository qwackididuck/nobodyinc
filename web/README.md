# Nobody Inc control room

The web UI of Nobody Inc: a single client-side page, talking to the daemon
(`cmd/nobodyd`) through its REST API and its event stream. Next.js 16, React 19,
strict TypeScript, pnpm. Read `AGENTS.md` first: this Next.js version has
breaking changes, and its documentation is in `node_modules/next/dist/docs/`.

## Run it

From the repository root:

```bash
task web:install            # once
task dev:daemon             # the daemon, on ../nobody-sandbox by default
task dev:web                # http://localhost:3000
```

In development, `/api/*` (REST and the event stream) goes through the Next.js
proxy to `NOBODY_DAEMON_URL` (default `http://127.0.0.1:7420`); terminals open
a WebSocket straight to `NEXT_PUBLIC_NOBODY_DAEMON_WS` (default
`ws://127.0.0.1:7420`). The page uses no server feature of Next.js, so that it
can be exported as a static site later.

## How it is built

- `src/lib/api.ts`: the API client, written from `api/openapi.yaml`.
- `src/lib/live.tsx`: the daemon as the UI sees it. It loads everything once,
  then reloads only what the event stream (`GET /api/events`, docs/events.md)
  says has changed: no polling. Components read it with `useLive()`; the office
  animations receive the raw events with `useDaemonEvents()`.
- `src/components/App.tsx`: the frame (top bar and tabs, chat panel, main area).
- `chat/`: the avatar rail, each agent's chat (messages, permission requests,
  urgent questions, feature proposals), its terminal, and the wastebasket of
  messages between agents.
- `office/`: the pixel office. `layout.ts` places rooms and desks (a pure
  function); `Office.tsx` draws them and plays arrivals, departures and new
  rooms by comparing the organization before and after a change; `effects.ts`
  runs the passing animations (paper planes, walkers, construction work)
  outside React; `flight.ts` computes the paper planes' paths.
- `kanban/`, `recruitment/`, `settings/`: the other tabs.
- `ui/ui.module.css`: pixel controls shared by the tabs.

Styles are CSS Modules; every color is a variable of the "Dusk" theme in
`src/app/globals.css`. Fonts (`next/font/google`) are downloaded at build time
and served with the app. The design reference is
`docs/mockups/ui-design/c-pixel-office.html`.

## Checks

`task check` at the repository root runs `tsc --noEmit` and ESLint on this app,
with the Go linters and tests.
