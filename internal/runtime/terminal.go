package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/coder/websocket"
)

// TerminalHandler streams an agent's terminal over a WebSocket, so the web UI
// can show it with xterm.js and the human can type into it. Mount it on a
// pattern with an {address} wildcard, e.g. "GET /api/agents/{address}/terminal".
//
// Protocol:
//
//   - server → browser, binary frames: raw terminal output. The first frame
//     is the recent backlog, then live output follows.
//   - browser → server, text frames: JSON control messages,
//     {"type":"input","data":"..."} for keystrokes and
//     {"type":"resize","rows":40,"cols":120} when the view changes size.
//
// allowedOrigins lists the browser origins allowed to connect besides the
// daemon's own (e.g. "localhost:3000" for the Next.js dev server).
func TerminalHandler(s *Supervisor, allowedOrigins []string, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		address := r.PathValue("address")

		output, ok := s.Output(address)
		if !ok {
			http.Error(w, "agent is not running", http.StatusNotFound)

			return
		}

		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: allowedOrigins})
		if err != nil {
			logger.Warn("terminal websocket refused", "agent", address, "err", err)

			return
		}
		defer conn.CloseNow() //nolint:errcheck // closing an already closed connection is harmless

		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()

		backlog, updates, unsubscribe := output.Subscribe()
		defer unsubscribe()

		go streamOutput(ctx, cancel, conn, backlog, updates)

		readControls(ctx, conn, s, address, logger)
	})
}

// streamOutput sends the backlog then the live output to the browser. It is
// the only goroutine writing to the connection.
func streamOutput(ctx context.Context, done context.CancelFunc, conn *websocket.Conn, backlog []byte, updates <-chan []byte) {
	defer done()

	if len(backlog) > 0 {
		if err := conn.Write(ctx, websocket.MessageBinary, backlog); err != nil {
			return
		}
	}

	for chunk := range updates {
		if err := conn.Write(ctx, websocket.MessageBinary, chunk); err != nil {
			return
		}
	}
}

// control is a message sent by the browser.
type control struct {
	Type string `json:"type"` // "input" or "resize"
	Data string `json:"data,omitempty"`
	Rows uint16 `json:"rows,omitempty"`
	Cols uint16 `json:"cols,omitempty"`
}

// readControls applies the browser's keystrokes and resizes until it leaves.
func readControls(ctx context.Context, conn *websocket.Conn, s *Supervisor, address string, logger *slog.Logger) {
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return // browser gone or output stream ended
		}

		var c control
		if err := json.Unmarshal(data, &c); err != nil {
			logger.Warn("bad terminal control message", "agent", address, "err", err)

			continue
		}

		if err := apply(s, address, c); err != nil && !errors.Is(err, ErrNotRunning) {
			logger.Warn("terminal control", "agent", address, "type", c.Type, "err", err)
		}
	}
}

func apply(s *Supervisor, address string, c control) error {
	switch c.Type {
	case "input":
		return s.Input(address, []byte(c.Data))
	case "resize":
		if c.Rows == 0 || c.Cols == 0 {
			return errors.New("resize needs rows and cols")
		}

		return s.Resize(address, c.Rows, c.Cols)
	default:
		return errors.New("unknown control type " + c.Type)
	}
}
