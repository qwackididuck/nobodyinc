package main

import "github.com/wailsapp/wails/v3/pkg/application"

// closingBackground is the control room's background (--bg of its theme),
// so that the window never flashes white before its page is drawn.
var closingBackground = application.NewRGB(0x1b, 0x16, 0x26) //nolint:mnd // a color

// closingHTML is the page of the closing window (quit.go): three pixel squares
// hopping one after the other, and what is happening. It needs no server (the
// launcher's closes first) and no script.
const closingHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Nobody Inc</title>
<style>
  :root { color-scheme: dark; }
  html, body { height: 100%; margin: 0; }
  body {
    display: grid;
    place-items: center;
    background: #1b1626;
    color: #f4ecdb;
    font: 13px/1.45 -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
    cursor: default;
    -webkit-user-select: none;
  }
  main {
    box-sizing: border-box;
    display: grid;
    place-content: center;
    gap: 10px;
    width: 100%;
    height: 100%;
    border: 3px solid #3e3558;
    text-align: center;
  }
  h1 {
    margin: 0;
    font: 700 18px ui-monospace, "SF Mono", Menlo, monospace;
    letter-spacing: 0.02em;
  }
  p { margin: 0; color: #a99dc2; }
  .dots { display: flex; justify-content: center; gap: 6px; height: 12px; align-items: flex-end; }
  .dots i {
    width: 8px;
    height: 8px;
    background: #ff8a5c;
    animation: hop 0.9s steps(1) infinite;
  }
  .dots i:nth-child(2) { animation-delay: 0.3s; }
  .dots i:nth-child(3) { animation-delay: 0.6s; }
  @keyframes hop {
    0%, 33% { transform: translateY(-4px); }
    34%, 100% { transform: none; }
  }
  @media (prefers-reduced-motion: reduce) { .dots i { animation: none; } }
</style>
</head>
<body>
  <main role="status" aria-live="polite">
    <div class="dots" aria-hidden="true"><i></i><i></i><i></i></div>
    <h1>Closing Nobody Inc…</h1>
    <p>Stopping your agents. Their sessions are kept.</p>
  </main>
</body>
</html>
`
