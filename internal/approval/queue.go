// Package approval is the queue of permission requests waiting for the human.
//
// When an agent's Claude Code session needs a permission (run a command, edit
// a file…), its PermissionRequest hook asks the daemon, which files the
// request here and waits for the human to allow or deny it from the UI.
//
// A request that gets no decision in time is abandoned: the hook then answers
// nothing, and Claude Code falls back to its usual prompt in the agent's
// terminal. The queue lives in memory: after a daemon restart, pending
// requests fall back to the terminal the same way.
package approval

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"time"
)

// Errors returned by the queue.
var (
	ErrNotFound      = errors.New("no pending approval with this id")
	ErrNoDecision    = errors.New("no decision was taken in time")
	ErrBadSuggestion = errors.New("this request has no such suggestion")
)

// Request is a permission an agent is waiting for.
type Request struct {
	ID        string          `json:"id"`
	Agent     string          `json:"agent"`
	ToolName  string          `json:"tool_name"`
	ToolInput json.RawMessage `json:"tool_input,omitempty"`
	// Suggestions are the permission updates Claude Code offers with the
	// prompt, as it sent them (an allow rule, a new permission mode…). The
	// human can allow the request together with one of them.
	Suggestions []json.RawMessage `json:"suggestions,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
}

// Decision is the human's answer.
type Decision struct {
	Allow bool
	// Message is shown to the agent, typically to explain a refusal.
	Message string
	// Suggestion is the index in the request's Suggestions to apply with an
	// allow; nil for none.
	Suggestion *int
}

// Queue holds the pending requests. It is safe for concurrent use.
type Queue struct {
	mu      sync.Mutex
	pending map[string]*waiting
	// closed is closed by Close: every request is then abandoned.
	closed chan struct{}
	// OnChange, when set, is called after a request is added or removed.
	OnChange func()
}

type waiting struct {
	req      Request
	decision chan Decision
	// withdrawn is closed when the request was answered elsewhere (Withdraw).
	withdrawn chan struct{}
}

// NewQueue returns an empty queue.
func NewQueue() *Queue {
	return &Queue{pending: map[string]*waiting{}, closed: make(chan struct{})}
}

// Close abandons every pending request, and any asked afterwards: each Ask
// returns ErrNoDecision, and the agent falls back to its terminal prompt, as
// after a daemon restart. The daemon calls it when it stops: an agent waiting
// for the human holds an HTTP request open on the daemon, which a graceful
// shutdown would otherwise wait for, up to its timeout, with the control
// room's window frozen. It is safe to call more than once.
func (q *Queue) Close() {
	q.mu.Lock()
	defer q.mu.Unlock()

	select {
	case <-q.closed:
	default:
		close(q.closed)
	}
}

// Ask files a request and blocks until the human decides, ctx ends or the
// queue is closed. In the last two cases it returns ErrNoDecision and the
// request leaves the queue.
func (q *Queue) Ask(ctx context.Context, req Request) (Decision, error) {
	select {
	case <-q.closed:
		return Decision{}, ErrNoDecision
	default:
	}

	w := &waiting{req: req, decision: make(chan Decision, 1), withdrawn: make(chan struct{})}

	q.mu.Lock()
	q.pending[req.ID] = w
	q.mu.Unlock()
	q.changed()

	defer func() {
		q.mu.Lock()
		delete(q.pending, req.ID)
		q.mu.Unlock()
		q.changed()
	}()

	select {
	case d := <-w.decision:
		return d, nil
	case <-ctx.Done():
		return Decision{}, ErrNoDecision
	case <-w.withdrawn:
		return Decision{}, ErrNoDecision
	case <-q.closed:
		return Decision{}, ErrNoDecision
	}
}

// Withdraw removes the agent's pending requests for which answered returns
// true, and reports how many. It is for requests the human answered in the
// agent's own terminal, which the daemon only learns from the agent's next
// events: each withdrawn Ask returns ErrNoDecision, like an abandoned one.
func (q *Queue) Withdraw(agent string, answered func(Request) bool) int {
	q.mu.Lock()
	defer q.mu.Unlock()

	n := 0

	for id, w := range q.pending {
		if w.req.Agent == agent && answered(w.req) {
			delete(q.pending, id)
			close(w.withdrawn)

			n++
		}
	}

	return n
}

// Decide answers a pending request.
func (q *Queue) Decide(id string, d Decision) error {
	q.mu.Lock()

	w, ok := q.pending[id]
	if !ok {
		q.mu.Unlock()

		return ErrNotFound
	}

	if s := d.Suggestion; s != nil && (!d.Allow || *s < 0 || *s >= len(w.req.Suggestions)) {
		q.mu.Unlock()

		return ErrBadSuggestion
	}

	delete(q.pending, id) // a request is decided once
	q.mu.Unlock()

	w.decision <- d

	return nil
}

// Pending lists the requests waiting for a decision, oldest first.
func (q *Queue) Pending() []Request {
	q.mu.Lock()
	defer q.mu.Unlock()

	requests := make([]Request, 0, len(q.pending))
	for _, w := range q.pending {
		requests = append(requests, w.req)
	}

	sort.Slice(requests, func(i, j int) bool {
		return requests[i].CreatedAt.Before(requests[j].CreatedAt)
	})

	return requests
}

func (q *Queue) changed() {
	if q.OnChange != nil {
		q.OnChange()
	}
}
