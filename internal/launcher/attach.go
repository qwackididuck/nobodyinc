package launcher

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/qwackididuck/nobodyinc/internal/projects"
)

// statusHeld closes the connection of a window that does not (or no longer)
// hold the project: another window does. It mirrors HTTP's 409.
const statusHeld websocket.StatusCode = 4409

// pingTimeout bounds the answer to a ping.
const pingTimeout = 5 * time.Second

// Messages between the launcher and a window (api/openapi.yaml, attach).
type (
	// outgoing is a message to the window: "state", "held" or "taken".
	outgoing struct {
		stateEvent

		Type string `json:"type"`
		// Focused, with "held": the desktop app brought the window holding
		// the project to the front (Options.OnHeld).
		Focused bool `json:"focused,omitempty"`
	}
	// incoming is a message from the window: "release" (stop the project
	// now, the window switches project) or "start" (start it again after it
	// stopped on its own).
	incoming struct {
		Type string `json:"type"`
	}
)

// attach serves GET /api/projects/{project}/attach: the WebSocket through
// which a window holds a project (leases) and follows its daemon's state.
func (l *Launcher) attach(w http.ResponseWriter, r *http.Request) {
	entry, err := l.registry.Get(r.PathValue("project"))
	if errors.Is(err, projects.ErrUnknownProject) {
		writeError(w, http.StatusNotFound, err)

		return
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, err)

		return
	}

	window := r.URL.Query().Get("window")
	if window == "" {
		writeError(w, http.StatusBadRequest, errors.New("window is required"))

		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: l.opts.Origins})
	if err != nil {
		l.opts.Logger.Warn("attach websocket refused", "project", entry.ID, "err", err)

		return
	}
	defer conn.CloseNow() //nolint:errcheck // closing an already closed connection is harmless

	w2 := &windowConn{l: l, conn: conn, entry: entry, window: window}
	w2.serve(r.Context(), r.URL.Query().Get("takeover") == "true")
}

// windowConn is one window's connection holding a project.
type windowConn struct {
	l      *Launcher
	conn   *websocket.Conn
	entry  projects.Entry
	window string

	// Once the project is held.
	project *project
	hold    *hold
}

// serve holds the project for the window while the connection lasts, starts
// its daemon, and tells the window how the daemon is doing.
func (c *windowConn) serve(ctx context.Context, takeover bool) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	taken := make(chan struct{})

	h, err := c.l.leases.acquire(c.entry.ID, c.window, takeover, sync.OnceFunc(func() { close(taken) }))
	if err != nil {
		c.refuse(ctx, err)

		return
	}

	// When the connection ends without a release, the hold lasts for the
	// grace period. A hold released or taken is no longer this one: nothing
	// happens then.
	defer c.l.leases.disconnect(c.entry.ID, h)

	if err := c.l.registry.Touch(c.entry.ID, c.l.opts.Clock.Now()); err != nil {
		c.l.opts.Logger.Warn("record when the project was opened", "project", c.entry.ID, "err", err)
	}

	c.project, c.hold = c.l.project(c.entry), h

	current, events, unsubscribe := c.project.subscribe()
	defer unsubscribe()

	c.l.start(c.project)

	messages := make(chan incoming)
	go c.read(ctx, cancel, messages)
	go c.ping(ctx, cancel)

	c.follow(ctx, current, events, messages, taken)
}

// follow passes the daemon's state on to the window and acts on the
// window's messages, until the connection ends, the window releases the
// project, or another window takes it.
func (c *windowConn) follow(ctx context.Context, current stateEvent, events <-chan stateEvent,
	messages <-chan incoming, taken <-chan struct{},
) {
	if !c.send(ctx, outgoing{Type: "state", stateEvent: current}) {
		return
	}

	for {
		select {
		case event := <-events:
			if !c.send(ctx, outgoing{Type: "state", stateEvent: event}) {
				return
			}
		case msg := <-messages:
			if c.handle(msg) {
				return
			}
		case <-taken:
			c.send(ctx, outgoing{Type: "taken"})
			_ = c.conn.Close(statusHeld, "the project was opened in another window") // it may be gone already

			return
		case <-ctx.Done():
			return
		}
	}
}

// handle acts on a message from the window and reports whether the
// connection ends.
func (c *windowConn) handle(msg incoming) bool {
	switch msg.Type {
	case "release":
		c.l.leases.release(c.entry.ID, c.hold)
		_ = c.conn.Close(websocket.StatusNormalClosure, "released") // it may be gone already

		return true
	case "start":
		c.l.start(c.project)
	default:
		c.l.opts.Logger.Warn("unknown message from a window", "project", c.entry.ID, "type", msg.Type)
	}

	return false
}

// refuse tells a window it cannot hold the project, and closes. In the
// desktop app, the window holding the project is brought to the front
// instead (Options.OnHeld), and the window that asked is told so.
func (c *windowConn) refuse(ctx context.Context, err error) {
	if errors.Is(err, errHeld) {
		focused := false
		if onHeld := c.l.opts.OnHeld; onHeld != nil {
			focused = onHeld(c.entry.ID, c.l.leases.holder(c.entry.ID))
		}

		c.send(ctx, outgoing{Type: "held", Focused: focused})
		_ = c.conn.Close(statusHeld, err.Error()) // it may be gone already

		return
	}

	_ = c.conn.Close(websocket.StatusTryAgainLater, err.Error()) // it may be gone already
}

// send writes a message to the window and reports whether it went.
func (c *windowConn) send(ctx context.Context, msg outgoing) bool {
	return wsjson.Write(ctx, c.conn, msg) == nil
}

// read passes the window's messages on, and cancels the connection when it
// closes. It also lets pings get their answers (coder/websocket needs a
// reader for that).
func (c *windowConn) read(ctx context.Context, cancel context.CancelFunc, messages chan<- incoming) {
	defer cancel()

	for {
		_, data, err := c.conn.Read(ctx)
		if err != nil {
			return
		}

		var msg incoming
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}

		select {
		case messages <- msg:
		case <-ctx.Done():
			return
		}
	}
}

// ping checks the connection regularly: a connection that stopped answering
// (half-open) is treated as dropped.
func (c *windowConn) ping(ctx context.Context, cancel context.CancelFunc) {
	ticker := time.NewTicker(c.l.opts.PingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pingCtx, done := context.WithTimeout(ctx, pingTimeout)
			err := c.conn.Ping(pingCtx)

			done()

			if err != nil {
				cancel()

				return
			}
		}
	}
}
