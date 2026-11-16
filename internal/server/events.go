package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/qwackididuck/nobodyinc/internal/events"
)

// defaultHeartbeat is how often an idle stream sends a comment line. The
// Next.js dev proxy closes a connection that stays silent for 30 seconds.
const defaultHeartbeat = 15 * time.Second

// retryMillis tells EventSource how long to wait before reconnecting.
const retryMillis = 2000

// streamEvents serves GET /api/events: every change published on the bus,
// as Server-Sent Events (docs/events.md). The stream starts with "hello"
// (reload everything: nothing is replayed) and ends when the client goes
// away, when it falls too far behind, or when the daemon stops.
func (s *server) streamEvents(w http.ResponseWriter, r *http.Request) {
	if s.Events == nil {
		s.writeError(w, http.StatusServiceUnavailable, errors.New("events are not available"))

		return
	}

	stream, stop := s.Events.Subscribe()
	defer stop()

	rc := http.NewResponseController(w)
	if err := openStream(w, rc); err != nil {
		return
	}

	s.pump(r.Context(), w, rc, stream)
}

// openStream sends the headers, the reconnection delay and "hello".
func openStream(w http.ResponseWriter, rc *http.ResponseController) error {
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	// no-transform keeps proxies from compressing the stream: the Next.js
	// development server gzips responses and would hold events back.
	h.Set("Cache-Control", "no-cache, no-transform")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no") // tells buffering proxies to pass events at once
	w.WriteHeader(http.StatusOK)

	if _, err := fmt.Fprintf(w, "retry: %d\n\n", retryMillis); err != nil {
		return err
	}

	return writeEvent(w, rc, events.Event{Type: events.Hello, Data: events.Empty{}})
}

// pump writes events as they come, and a comment when nothing happened for
// a while, until ctx ends or the stream is closed.
func (s *server) pump(ctx context.Context, w http.ResponseWriter, rc *http.ResponseController, stream <-chan events.Event) {
	heartbeat := time.NewTicker(s.heartbeat())
	defer heartbeat.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case e, open := <-stream:
			if !open {
				return
			}

			if err := writeEvent(w, rc, e); err != nil {
				s.Logger.Debug("event stream closed", "err", err)

				return
			}
		case <-heartbeat.C:
			if err := writePing(w, rc); err != nil {
				return
			}
		}
	}
}

// writePing writes a comment line, which EventSource ignores.
func writePing(w http.ResponseWriter, rc *http.ResponseController) error {
	if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
		return err
	}

	return rc.Flush()
}

// writeEvent writes one event in the text/event-stream format and flushes it.
func writeEvent(w http.ResponseWriter, rc *http.ResponseController, e events.Event) error {
	data, err := json.Marshal(e.Data)
	if err != nil {
		return fmt.Errorf("encode event %s: %w", e.Type, err)
	}

	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.Type, data); err != nil {
		return err
	}

	return rc.Flush()
}

func (s *server) heartbeat() time.Duration {
	if s.Heartbeat > 0 {
		return s.Heartbeat
	}

	return defaultHeartbeat
}
