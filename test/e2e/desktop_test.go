package e2e

import (
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/qwackididuck/nobodyinc/internal/launcher"
)

// The launcher as the desktop app runs it (ADR-0016): it serves the control
// room, opens windows for it, brings the window holding a project to the
// front, and lets a closed window's project go at once.

// desktopHooks records what the launcher asks of the desktop app.
type desktopHooks struct {
	mu      sync.Mutex
	opened  []string // projects of the windows opened ("" for the projects page)
	focused []string // windows brought to the front
}

func (h *desktopHooks) install(o *launcher.Options) {
	o.UI = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("the control room")) })
	o.OpenWindow = func(project string) error {
		h.mu.Lock()
		defer h.mu.Unlock()

		h.opened = append(h.opened, project)

		return nil
	}
	o.OnHeld = func(_, holder string) bool {
		h.mu.Lock()
		defer h.mu.Unlock()

		h.focused = append(h.focused, holder)

		return true
	}
}

// recorded returns the projects of the windows opened, and the windows
// brought to the front.
func (h *desktopHooks) recorded() ([]string, []string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	return append([]string(nil), h.opened...), append([]string(nil), h.focused...)
}

func TestLauncherInTheDesktopApp(t *testing.T) {
	t.Parallel()

	hooks := &desktopHooks{}
	tl := startLauncherWith(t, hooks.install)
	shop := tl.open(t, t.TempDir(), false)

	// The control room is served at /, read-only.
	if body := tl.do(t, http.MethodGet, "/", "", http.StatusOK); string(body) != "the control room" {
		t.Fatalf("GET / = %q", body)
	}

	tl.do(t, http.MethodPost, "/", "{}", http.StatusMethodNotAllowed)

	// "Open in a new window" asks the app.
	tl.do(t, http.MethodPost, "/api/windows", `{"project":"`+shop.ID+`"}`, http.StatusNoContent)
	tl.do(t, http.MethodPost, "/api/windows", `{}`, http.StatusNoContent)
	tl.do(t, http.MethodPost, "/api/windows", `{"project":"nope"}`, http.StatusNotFound)

	if opened, _ := hooks.recorded(); strings.Join(opened, ",") != shop.ID+"," {
		t.Fatalf("windows opened for %q, want the project then the projects page", opened)
	}

	// A project held by a window: the app brings that window to the front.
	first := tl.attach(t, shop.ID, "w1", false)
	first.waitRunning(t)

	second := tl.attach(t, shop.ID, "w2", false)
	if msg := second.next(t); msg.Type != "held" || !msg.Focused {
		t.Fatalf("second window got %+v, want held and focused", msg)
	}

	if _, focused := hooks.recorded(); strings.Join(focused, ",") != "w1" {
		t.Fatalf("windows brought to the front: %q, want w1", focused)
	}

	// The control room served by the launcher changes things in the project,
	// through the proxy, with the launcher's own origin (no allowed origin
	// configured): the project daemon must take it as same-origin.
	tl.directorIdle(t, shop.ID)

	if data, status := tl.requestFrom(t, tl.url, http.MethodPut, "/p/"+shop.ID+"/api/project", `{"prompt":"Ship it."}`); status != http.StatusOK {
		t.Fatalf("PUT /api/project from the launcher's page = %d: %s", status, data)
	}

	if _, status := tl.requestFrom(t, "http://evil.example", http.MethodPut, "/p/"+shop.ID+"/api/project", `{"prompt":"x"}`); status != http.StatusForbidden {
		t.Fatalf("PUT /api/project from another site = %d, want 403", status)
	}

	// Closing w1 in the app stops the project at once: no grace period (the
	// manual clock never moves).
	tl.launcher.Release("w1")

	eventually(t, "the project stopped", func() bool {
		p := tl.project(t, shop.ID)

		return !p.Held && p.State == "stopped"
	})
}

// Outside the desktop app, the launcher opens no window.
func TestLauncherOpensNoWindowOutsideTheApp(t *testing.T) {
	t.Parallel()

	tl := startLauncher(t)
	tl.do(t, http.MethodPost, "/api/windows", `{}`, http.StatusNotFound)
}
