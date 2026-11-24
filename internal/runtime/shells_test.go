package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func newTestShells(t *testing.T) *Shells {
	t.Helper()

	s := &Shells{Dir: t.TempDir(), Shell: "/bin/sh", Logger: slog.New(slog.DiscardHandler)}
	t.Cleanup(s.CloseAll)

	return s
}

// waitFor polls until the shell's output holds want.
func waitForOutput(t *testing.T, o *Output, want string) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		backlog, _, cancel := o.Subscribe()
		cancel()

		if bytes.Contains(backlog, []byte(want)) {
			return
		}

		time.Sleep(20 * time.Millisecond)
	}

	t.Fatalf("output never held %q", want)
}

// A shell starts in the project's folder, runs what is typed, and is gone from
// the list once closed.
func TestShellsOpenTypeClose(t *testing.T) {
	t.Parallel()

	s := newTestShells(t)

	shell, err := s.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if got := s.List(); len(got) != 1 || got[0].ID != shell.ID {
		t.Fatalf("List = %+v, want just %s", got, shell.ID)
	}

	output, _ := s.Output(shell.ID)

	if err := s.Input(shell.ID, []byte("echo $((6*7)); pwd\n")); err != nil {
		t.Fatal(err)
	}

	waitForOutput(t, output, "42")
	waitForOutput(t, output, strings.TrimPrefix(s.Dir, "/private")) // macOS resolves /var to /private/var

	if err := s.Close(shell.ID); err != nil {
		t.Fatal(err)
	}

	if got := s.List(); len(got) != 0 {
		t.Errorf("List after Close = %+v, want none", got)
	}

	if err := s.Close(shell.ID); !errors.Is(err, ErrUnknownShell) {
		t.Errorf("closing twice = %v, want ErrUnknownShell", err)
	}
}

// A shell that ends by itself (the user typed exit) leaves the list.
func TestShellsForgetAShellThatExits(t *testing.T) {
	t.Parallel()

	s := newTestShells(t)

	shell, err := s.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if err := s.Input(shell.ID, []byte("exit\n")); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for len(s.List()) > 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}

	if got := s.List(); len(got) != 0 {
		t.Fatalf("an exited shell is still listed: %+v", got)
	}
}

func TestShellsLimitAndShutdown(t *testing.T) {
	t.Parallel()

	s := newTestShells(t)

	for range maxShells {
		if _, err := s.Open(context.Background()); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := s.Open(context.Background()); !errors.Is(err, ErrTooManyShells) {
		t.Fatalf("Open over the limit = %v, want ErrTooManyShells", err)
	}

	s.CloseAll()

	if got := s.List(); len(got) != 0 {
		t.Errorf("shells left after CloseAll: %d", len(got))
	}

	if _, err := s.Open(context.Background()); !errors.Is(err, ErrStopping) {
		t.Errorf("Open after CloseAll = %v, want ErrStopping", err)
	}
}

func TestShellEnvSetsTerminal(t *testing.T) {
	t.Parallel()

	got := shellEnv([]string{"A=1", "TERM=dumb", "TERMINAL=x"})
	want := []string{"A=1", "TERMINAL=x", "TERM=xterm-256color", "COLORTERM=truecolor"}

	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("shellEnv = %v, want %v", got, want)
	}
}

// The routes open, list and close shells, and the WebSocket drives one with
// the protocol of the agents' terminals.
func TestShellsHandlers(t *testing.T) {
	t.Parallel()

	s := newTestShells(t)
	mux := http.NewServeMux()

	for pattern, h := range ShellsHandlers(s, nil, s.Logger) {
		mux.Handle(pattern, h)
	}

	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	res, err := do(ctx, http.MethodPost, srv.URL+"/api/shells")
	if err != nil {
		t.Fatal(err)
	}

	var shell Shell
	if err := json.NewDecoder(res.Body).Decode(&shell); err != nil || res.StatusCode != http.StatusCreated {
		t.Fatalf("POST = %d, %v", res.StatusCode, err)
	}

	res.Body.Close()

	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/api/shells/"+shell.ID+"/terminal", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow() //nolint:errcheck // test cleanup

	msg, err := json.Marshal(control{Type: "input", Data: "echo marker-$((1+1))\n"})
	if err != nil {
		t.Fatal(err)
	}

	if err := conn.Write(ctx, websocket.MessageText, msg); err != nil {
		t.Fatal(err)
	}

	var seen strings.Builder
	for !strings.Contains(seen.String(), "marker-2") {
		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("reading the terminal: %v (saw %q)", err, seen.String())
		}

		seen.Write(data)
	}

	res, err = do(ctx, http.MethodDelete, srv.URL+"/api/shells/"+shell.ID)
	if err != nil || res.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE = %v, %v", res, err)
	}

	res.Body.Close()

	// The viewer is let go once the shell is gone.
	for {
		if _, _, err := conn.Read(ctx); err != nil {
			break
		}
	}

	res, err = do(ctx, http.MethodGet, srv.URL+"/api/shells/"+shell.ID+"/terminal")
	if err != nil || res.StatusCode != http.StatusNotFound {
		t.Fatalf("terminal of a closed shell = %v, %v; want 404", res, err)
	}

	res.Body.Close()
}

// do sends a request without a body.
func do(ctx context.Context, method, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, http.NoBody)
	if err != nil {
		return nil, err
	}

	return http.DefaultClient.Do(req)
}
