package launcher

import (
	"context"
	"net/http"

	"github.com/qwackididuck/nobodyinc/internal/updates"
)

// checkAtStart looks for a newer release once, in the background: the
// launcher never waits for GitHub (docs/updates.md).
func (l *Launcher) checkAtStart() {
	go func() {
		status := l.updates.Check(l.ctx)

		switch {
		case status.Error != "":
			l.opts.Logger.Debug("update check failed", "err", status.Error)
		case status.Available:
			l.opts.Logger.Info("a newer release is available", "current", status.Current, "latest", status.Latest)
		}
	}()
}

// CheckUpdate looks for a newer release now and returns what it found. The
// desktop app's menu uses it (docs/desktop.md); the control room goes
// through POST /api/update/check.
func (l *Launcher) CheckUpdate(ctx context.Context) updates.Status {
	return l.updates.Check(ctx)
}

// getUpdate is GET /api/update: the last check's result.
func (l *Launcher) getUpdate(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, l.updates.Status())
}

// checkUpdate is POST /api/update/check.
func (l *Launcher) checkUpdate(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, l.updates.Check(r.Context()))
}
