package e2e

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/qwackididuck/nobodyinc/internal/events"
)

// streamedEvent is one event read from GET /api/events.
type streamedEvent struct {
	Type string
	Data string
}

// subscribe opens the daemon's event stream and returns the events as they
// arrive.
func subscribe(t *testing.T, handler http.Handler) <-chan streamedEvent {
	t.Helper()

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/api/events", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}

	res, err := http.DefaultClient.Do(req) //nolint:bodyclose // closed by the reading goroutine
	if err != nil {
		t.Fatal(err)
	}

	out := make(chan streamedEvent, 256)

	go func() {
		defer res.Body.Close()
		defer close(out)

		var e streamedEvent

		scanner := bufio.NewScanner(res.Body)
		for scanner.Scan() {
			line := scanner.Text()

			switch {
			case strings.HasPrefix(line, "event: "):
				e.Type = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				e.Data = strings.TrimPrefix(line, "data: ")
			case line == "" && e.Type != "":
				out <- e

				e = streamedEvent{}
			}
		}
	}()

	return out
}

// waitForEvent reads events until one of type typ matches ok.
func waitForEvent(t *testing.T, stream <-chan streamedEvent, typ string, ok func(data string) bool) {
	t.Helper()

	timeout := time.After(20 * time.Second)

	for {
		select {
		case e, open := <-stream:
			if !open {
				t.Fatalf("event stream closed while waiting for %s", typ)
			}

			if e.Type == typ && ok(e.Data) {
				return
			}
		case <-timeout:
			t.Fatalf("no %s event", typ)
		}
	}
}

// The control room follows a message without polling: it sees the message
// sent, the recipient start working, the delivery, and the recipient going
// back to idle.
func TestEventsFollowAMessage(t *testing.T) {
	t.Parallel()

	d := startDaemon(t, testOrg(t))
	stream := subscribe(t, d.Handler)

	waitForEvent(t, stream, events.Hello, func(string) bool { return true })

	sendAsHuman(t, d, "archi@specs", "Data model", "Which tables do we need for orders?")

	waitForEvent(t, stream, events.MessageSent, func(data string) bool {
		var m struct {
			From string `json:"from"`
			To   string `json:"to"`
		}

		return json.Unmarshal([]byte(data), &m) == nil && m.From == "human" && m.To == "archi@specs"
	})
	waitForEvent(t, stream, events.AgentStatus, func(data string) bool {
		return data == `{"address":"archi@specs","status":"working"}`
	})
	waitForEvent(t, stream, events.MessagesDelivered, func(data string) bool {
		return strings.Contains(data, `"recipient":"archi@specs"`)
	})
	waitForEvent(t, stream, events.AgentStatus, func(data string) bool {
		return data == `{"address":"archi@specs","status":"idle"}`
	})
}
