package server

import (
	"bufio"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/qwackididuck/nobodyinc/internal/events"
)

// sseLine reads the stream line by line in the background.
func sseLines(t *testing.T, body *bufio.Reader) <-chan string {
	t.Helper()

	lines := make(chan string, 64)

	go func() {
		defer close(lines)

		for {
			line, err := body.ReadString('\n')
			if err != nil {
				return
			}

			lines <- strings.TrimRight(line, "\n")
		}
	}()

	return lines
}

// drain reads until the server closes the stream (the channel closes).
func drain(lines <-chan string) {
	for range lines {
		continue
	}
}

// expectLine waits for a line with the given prefix and returns it.
func expectLine(t *testing.T, lines <-chan string, prefix string) string {
	t.Helper()

	timeout := time.After(5 * time.Second)

	for {
		select {
		case line, open := <-lines:
			if !open {
				t.Fatalf("stream closed while waiting for %q", prefix)
			}

			if strings.HasPrefix(line, prefix) {
				return line
			}
		case <-timeout:
			t.Fatalf("no line starting with %q", prefix)
		}
	}
}

func TestEventStream(t *testing.T) {
	t.Parallel()

	bus := events.NewBus()
	srv := httptest.NewServer(New(Deps{Logger: slog.New(slog.DiscardHandler), Events: bus, Heartbeat: 50 * time.Millisecond}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/events", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()

	if ct := res.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q", ct)
	}

	if cc := res.Header.Get("Cache-Control"); !strings.Contains(cc, "no-transform") {
		t.Fatalf("Cache-Control = %q: proxies would compress and hold events back", cc)
	}

	lines := sseLines(t, bufio.NewReader(res.Body))

	expectLine(t, lines, "retry: ")
	expectLine(t, lines, "event: hello")

	bus.Publish(events.Event{Type: events.AgentStatus, Data: events.AgentStatusData{Address: "dev1@dev", Status: "working"}})
	expectLine(t, lines, "event: agent.status")

	if data := expectLine(t, lines, "data: "); data != `data: {"address":"dev1@dev","status":"working"}` {
		t.Fatalf("data line = %q", data)
	}

	expectLine(t, lines, ": ping")

	// Stopping the daemon ends the stream.
	bus.Close()

	drain(lines)
}

func TestEventStreamWithoutBus(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	New(Deps{Logger: slog.New(slog.DiscardHandler)}).ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/events", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}
