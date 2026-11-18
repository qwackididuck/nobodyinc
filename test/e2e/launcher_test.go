package e2e

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/qwackididuck/nobodyinc/internal/launcher"
	"github.com/qwackididuck/nobodyinc/internal/org"
	"github.com/qwackididuck/nobodyinc/internal/schedule"
)

// testLauncher is the real launcher (ADR-0014) on a local port, starting
// real project daemons (the nobodyd binary) with fakeclaude agents. Its
// grace period runs on a manual clock.
type testLauncher struct {
	url      string
	clock    *schedule.ManualClock
	home     string
	launcher *launcher.Launcher
}

func startLauncher(t *testing.T) *testLauncher {
	t.Helper()

	return startLauncherWith(t, func(*launcher.Options) {})
}

// startLauncherWith is startLauncher with options changed by change, e.g.
// the desktop app's hooks.
func startLauncherWith(t *testing.T, change func(*launcher.Options)) *testLauncher {
	t.Helper()

	home := t.TempDir()
	clock := schedule.NewManualClock(time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC))

	opts := launcher.Options{
		Logger:      slog.New(slog.DiscardHandler),
		Home:        home,
		Executable:  nobodydBinary,
		ProjectArgs: []string{"-claude", fakeClaude, "-hook", hookBinary},
		Output:      io.Discard,
		Clock:       clock,
	}
	change(&opts)

	l := launcher.New(opts)
	srv := httptest.NewServer(l.Handler)

	t.Cleanup(func() {
		l.Close()
		srv.Close()
	})

	return &testLauncher{url: srv.URL, clock: clock, home: home, launcher: l}
}

// do calls the launcher and checks the status.
func (tl *testLauncher) do(t *testing.T, method, path, body string, want int) []byte {
	t.Helper()

	data, status := tl.request(t, method, path, body)
	if status != want {
		t.Fatalf("%s %s = %d, want %d: %s", method, path, status, want, data)
	}

	return data
}

func (tl *testLauncher) request(t *testing.T, method, path, body string) ([]byte, int) {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), method, tl.url+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}

	req.Header.Set("Content-Type", "application/json")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()

	data, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}

	return data, res.StatusCode
}

type launcherProject struct {
	ID    string `json:"id"`
	Path  string `json:"path"`
	Git   bool   `json:"git"`
	Held  bool   `json:"held"`
	State string `json:"state"`
}

// open makes a folder a project (POST /api/projects).
func (tl *testLauncher) open(t *testing.T, path string, create bool) launcherProject {
	t.Helper()

	body, err := json.Marshal(map[string]any{"path": path, "create": create})
	if err != nil {
		t.Fatal(err)
	}

	var p launcherProject
	decodeBody(t, tl.do(t, http.MethodPost, "/api/projects", string(body), http.StatusCreated), &p)

	return p
}

// project returns the launcher's view of one project.
func (tl *testLauncher) project(t *testing.T, id string) launcherProject {
	t.Helper()

	var list []launcherProject
	decodeBody(t, tl.do(t, http.MethodGet, "/api/projects", "", http.StatusOK), &list)

	for _, p := range list {
		if p.ID == id {
			return p
		}
	}

	t.Fatalf("project %s not listed", id)

	return launcherProject{}
}

// testWindow is a control room window holding a project.
type testWindow struct {
	conn *websocket.Conn
}

type windowMessage struct {
	Type    string `json:"type"`
	State   string `json:"state"`
	Error   string `json:"error"`
	Focused bool   `json:"focused"`
}

func (tl *testLauncher) attach(t *testing.T, project, window string, takeover bool) *testWindow {
	t.Helper()

	u := "ws" + strings.TrimPrefix(tl.url, "http") + "/api/projects/" + project + "/attach?window=" +
		url.QueryEscape(window)
	if takeover {
		u += "&takeover=true"
	}

	conn, _, err := websocket.Dial(t.Context(), u, nil) //nolint:bodyclose // closed with the connection
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = conn.CloseNow() })

	return &testWindow{conn: conn}
}

// next reads the window's next message.
func (w *testWindow) next(t *testing.T) windowMessage {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	var msg windowMessage
	if err := wsjson.Read(ctx, w.conn, &msg); err != nil {
		t.Fatalf("read from the launcher: %v", err)
	}

	return msg
}

// waitRunning reads messages until the project daemon runs.
func (w *testWindow) waitRunning(t *testing.T) {
	t.Helper()

	for {
		msg := w.next(t)
		if msg.Type == "state" && msg.State == "running" {
			return
		}

		if msg.Type == "state" && msg.Error != "" {
			t.Fatalf("project daemon failed: %s", msg.Error)
		}
	}
}

// closedHeld checks the launcher closed the connection because another
// window holds the project.
func (w *testWindow) closedHeld(t *testing.T) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	_, _, err := w.conn.Read(ctx)
	if websocket.CloseStatus(err) != 4409 {
		t.Fatalf("close = %v, want status 4409", err)
	}
}

func (w *testWindow) send(t *testing.T, typ string) {
	t.Helper()

	if err := wsjson.Write(t.Context(), w.conn, map[string]string{"type": typ}); err != nil {
		t.Fatal(err)
	}
}

// directorIdle waits until the project's director, reached through the
// launcher's proxy, is idle.
func (tl *testLauncher) directorIdle(t *testing.T, project string) {
	t.Helper()

	eventually(t, "the director of "+project+" idle", func() bool {
		data, status := tl.request(t, http.MethodGet, "/p/"+project+"/api/agents", "")
		if status != http.StatusOK {
			return false
		}

		var list []struct {
			Status string `json:"status"`
		}

		decodeBody(t, data, &list)

		return len(list) == 1 && list[0].Status == "idle"
	})
}

// Each window holds one project: both projects run side by side, each with
// its own daemon behind the launcher, and closing a window stops its
// project once the grace period is over.
func TestLauncherRunsOneProjectPerWindow(t *testing.T) {
	t.Parallel()

	tl := startLauncher(t)
	shop := tl.open(t, t.TempDir(), false)
	blog := tl.open(t, t.TempDir(), false)

	if !org.HasConfig(shop.Path) {
		t.Fatal("opening a folder should set it up")
	}

	shopWindow := tl.attach(t, shop.ID, "w-shop", false)
	blogWindow := tl.attach(t, blog.ID, "w-blog", false)
	shopWindow.waitRunning(t)
	blogWindow.waitRunning(t)

	tl.directorIdle(t, shop.ID)
	tl.directorIdle(t, blog.ID)

	// The proxy reaches each project's own daemon.
	for _, p := range []launcherProject{shop, blog} {
		var settings struct {
			ProjectID string `json:"project_id"`
		}

		decodeBody(t, tl.do(t, http.MethodGet, "/p/"+p.ID+"/api/settings", "", http.StatusOK), &settings)

		if settings.ProjectID != p.ID {
			t.Fatalf("/p/%s reached project %s", p.ID, settings.ProjectID)
		}
	}

	// The shop window closes: the project keeps running during the grace
	// period, then stops. The blog goes on.
	_ = shopWindow.conn.Close(websocket.StatusGoingAway, "window closed")

	tl.clock.WaitForTimers(1)

	if p := tl.project(t, shop.ID); !p.Held || p.State != "running" {
		t.Fatalf("during the grace period: %+v", p)
	}

	tl.clock.Advance(launcher.DefaultGrace)

	eventually(t, "the shop stopped", func() bool {
		p := tl.project(t, shop.ID)

		return !p.Held && p.State == "stopped"
	})

	tl.do(t, http.MethodGet, "/p/"+shop.ID+"/api/agents", "", http.StatusServiceUnavailable)
	tl.directorIdle(t, blog.ID)
}

// A project is held by one window at most: a second window is refused until
// it takes the project over; switching project releases it at once.
func TestLauncherOneWindowPerProject(t *testing.T) {
	t.Parallel()

	tl := startLauncher(t)
	shop := tl.open(t, t.TempDir(), false)

	first := tl.attach(t, shop.ID, "w1", false)
	first.waitRunning(t)

	second := tl.attach(t, shop.ID, "w2", false)
	if msg := second.next(t); msg.Type != "held" {
		t.Fatalf("second window got %+v, want held", msg)
	}

	second.closedHeld(t)

	third := tl.attach(t, shop.ID, "w3", true)
	if msg := third.next(t); msg.Type != "state" || msg.State != "running" {
		t.Fatalf("takeover got %+v, want the running state", msg)
	}

	if msg := first.next(t); msg.Type != "taken" {
		t.Fatalf("first window got %+v, want taken", msg)
	}

	first.closedHeld(t)

	// The project keeps running for the window that took it.
	if p := tl.project(t, shop.ID); !p.Held || p.State != "running" {
		t.Fatalf("after the takeover: %+v", p)
	}

	// The window switches project: no grace period.
	third.send(t, "release")

	eventually(t, "the project stopped", func() bool {
		p := tl.project(t, shop.ID)

		return !p.Held && p.State == "stopped"
	})
}

// Deleting a project removes Nobody Inc and never the code: a feature that
// is not finished stops the deletion until the human chooses what to do
// with it.
func TestLauncherDeletesAProject(t *testing.T) {
	t.Parallel()

	tl := startLauncher(t)
	shop := tl.open(t, filepath.Join(t.TempDir(), "shop"), true)

	if !shop.Git {
		t.Fatal("a new project should be a git repository")
	}

	window := tl.attach(t, shop.ID, "w1", false)
	window.waitRunning(t)
	tl.directorIdle(t, shop.ID)

	var feature struct {
		Branch string `json:"branch"`
	}

	decodeBody(t, tl.do(t, http.MethodPost, "/p/"+shop.ID+"/api/features", `{"title":"Orders"}`, http.StatusCreated),
		&feature)

	// Another window may not delete a project it does not hold.
	tl.do(t, http.MethodDelete, "/api/projects/"+shop.ID+"?window=w2", "", http.StatusConflict)

	var refusal struct {
		Feature *struct {
			Title string `json:"title"`
		} `json:"feature"`
	}

	decodeBody(t, tl.do(t, http.MethodDelete, "/api/projects/"+shop.ID+"?window=w1", "", http.StatusConflict), &refusal)

	if refusal.Feature == nil || refusal.Feature.Title != "Orders" {
		t.Fatalf("refusal = %+v, want the unfinished feature", refusal)
	}

	// The refusal changed nothing: the project still runs for its window.
	if p := tl.project(t, shop.ID); !p.Held || p.State != "running" {
		t.Fatalf("after the refusal: %+v", p)
	}

	tl.do(t, http.MethodDelete, "/api/projects/"+shop.ID+"?window=w1&feature=discard", "", http.StatusNoContent)

	if org.HasConfig(shop.Path) {
		t.Fatal(".nobody/ still there")
	}

	if _, err := os.Stat(filepath.Join(tl.home, "projects", shop.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("state directory still there: %v", err)
	}

	if branches := git(t, shop.Path, "branch", "--list", "nobody/*"); !strings.Contains(branches, feature.Branch) {
		t.Fatalf("the feature branch %s is code: it must stay (branches: %q)", feature.Branch, branches)
	}

	_, status := tl.request(t, http.MethodGet, "/api/projects", "")
	if status != http.StatusOK || len(tl.list(t)) != 0 {
		t.Fatal("the project should be forgotten")
	}
}

// Merging as it is finishes the feature through a daemon started without
// its agents, then deletes the project.
func TestLauncherDeletesAProjectMergingItsFeature(t *testing.T) {
	t.Parallel()

	tl := startLauncher(t)
	shop := tl.open(t, filepath.Join(t.TempDir(), "shop"), true)

	window := tl.attach(t, shop.ID, "w1", false)
	window.waitRunning(t)
	tl.directorIdle(t, shop.ID)

	var feature struct {
		Branch string `json:"branch"`
	}

	decodeBody(t, tl.do(t, http.MethodPost, "/p/"+shop.ID+"/api/features", `{"title":"Orders"}`, http.StatusCreated),
		&feature)

	// Some work on the feature branch, so that there is something to merge.
	scratch := filepath.Join(t.TempDir(), "scratch")
	git(t, shop.Path, "worktree", "add", scratch, feature.Branch)

	if err := os.WriteFile(filepath.Join(scratch, "orders.go"), []byte("package orders\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	git(t, scratch, "add", ".")
	git(t, scratch, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "feat: orders")
	git(t, shop.Path, "worktree", "remove", scratch)

	// The window closes: the project stops, its feature still active.
	window.send(t, "release")
	eventually(t, "the project stopped", func() bool { return tl.project(t, shop.ID).State == "stopped" })

	tl.do(t, http.MethodDelete, "/api/projects/"+shop.ID+"?feature=merge", "", http.StatusNoContent)

	if org.HasConfig(shop.Path) {
		t.Fatal(".nobody/ still there")
	}

	if log := git(t, shop.Path, "log", "--format=%s", "-1"); !strings.Contains(log, "Merge feature") {
		t.Fatalf("the feature was not merged into the base branch: last commit %q", log)
	}

	if _, err := os.Stat(filepath.Join(shop.Path, "orders.go")); err != nil {
		t.Fatal("the feature's work should be in the project")
	}
}

func (tl *testLauncher) list(t *testing.T) []launcherProject {
	t.Helper()

	var list []launcherProject
	decodeBody(t, tl.do(t, http.MethodGet, "/api/projects", "", http.StatusOK), &list)

	return list
}
