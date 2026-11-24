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

		serveTerminal(w, r, terminal{
			output:  output,
			input:   func(data []byte) error { return s.Input(address, data) },
			resize:  func(rows, cols uint16) error { return s.Resize(address, rows, cols) },
			ended:   sessionEnded,
			subject: address,
		}, allowedOrigins, logger)
	})
}

// terminal is what the WebSocket protocol drives: the output to stream and
// how to type into, and resize, the pseudo-terminal behind it. An agent's
// terminal (TerminalHandler) and a shell (ShellsHandler) both are one.
type terminal struct {
	output *Output
	input  func([]byte) error
	resize func(rows, cols uint16) error
	// ended is the close reason sent when the output ends.
	ended string
	// subject names the terminal in the logs.
	subject string
}

// serveTerminal upgrades the request to a WebSocket and runs the protocol
// described on TerminalHandler until the viewer leaves or the output ends.
func serveTerminal(w http.ResponseWriter, r *http.Request, t terminal, allowedOrigins []string, logger *slog.Logger) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: allowedOrigins})
	if err != nil {
		logger.Warn("terminal websocket refused", "terminal", t.subject, "err", err)

		return
	}
	defer conn.CloseNow() //nolint:errcheck // closing an already closed connection is harmless

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	backlog, updates, unsubscribe := t.output.Subscribe()
	defer unsubscribe()

	go streamOutput(ctx, cancel, conn, backlog, updates, t.ended)

	readControls(ctx, conn, t, logger)
}

// sessionEnded is the close reason sent when the agent's process ends (or
// the viewer was too slow): the control room says so and reconnects, which
// attaches it to the agent's next session.
const sessionEnded = "the agent's session ended"

// streamOutput sends the backlog then the live output to the browser. It is
// the only goroutine writing to the connection. When the output ends, the
// connection is closed with sessionEnded.
func streamOutput(
	ctx context.Context, done context.CancelFunc, conn *websocket.Conn, backlog []byte, updates <-chan []byte, reason string,
) {
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

	if ctx.Err() == nil {
		_ = conn.Close(websocket.StatusNormalClosure, reason) // the browser may already be gone
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
func readControls(ctx context.Context, conn *websocket.Conn, t terminal, logger *slog.Logger) {
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return // browser gone or output stream ended
		}

		var c control
		if err := json.Unmarshal(data, &c); err != nil {
			logger.Warn("bad terminal control message", "terminal", t.subject, "err", err)

			continue
		}

		if err := apply(t, c); err != nil && !errors.Is(err, ErrNotRunning) {
			logger.Warn("terminal control", "terminal", t.subject, "type", c.Type, "err", err)
		}
	}
}

func apply(t terminal, c control) error {
	switch c.Type {
	case "input":
		return t.input([]byte(c.Data))
	case "resize":
		if c.Rows == 0 || c.Cols == 0 {
			return errors.New("resize needs rows and cols")
		}

		return t.resize(c.Rows, c.Cols)
	default:
		return errors.New("unknown control type " + c.Type)
	}
}
