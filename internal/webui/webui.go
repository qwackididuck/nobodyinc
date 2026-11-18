// Package webui embeds the control room, exported as a static site, so that
// the launcher serves it next to its API (ADR-0016): one origin for the
// page, the REST calls, the event streams and the WebSockets.
//
// `task web:export` builds the export (web/, NOBODY_WEB_EXPORT=1) and copies
// it into dist/, which is not versioned apart from its README. Built without
// it, the binaries serve a page saying how to build it. Development does not
// need it: `next dev` serves the control room on its own port.
package webui

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed all:dist
var dist embed.FS

// notBuilt is served at / when the binary was built without the export.
const notBuilt = `<!doctype html>
<meta charset="utf-8">
<title>Nobody Inc</title>
<body style="font-family: system-ui, sans-serif; max-width: 40rem; margin: 4rem auto; line-height: 1.5">
<h1>The control room is not built in</h1>
<p>This binary was built without the control room. Run <code>task web:export</code>, then build again,
or use <code>task dev:web</code> and open <a href="http://localhost:3000">http://localhost:3000</a>.</p>
</body>
`

// Handler serves the embedded control room, or the "not built" page.
func Handler() http.Handler {
	files, err := fs.Sub(dist, "dist")
	if err != nil || !Built(files) {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(notBuilt)) // the client may be gone
		})
	}

	return http.FileServerFS(files)
}

// Built reports whether files hold an exported control room.
func Built(files fs.FS) bool {
	_, err := fs.Stat(files, "index.html")

	return err == nil
}
