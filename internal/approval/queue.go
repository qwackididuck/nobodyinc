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
	ErrNotFound   = errors.New("no pending approval with this id")
	ErrNoDecision = errors.New("no decision was taken in time")
)

// Request is a permission an agent is waiting for.
type Request struct {
	ID        string          `json:"id"`
	Agent     string          `json:"agent"`
	ToolName  string          `json:"tool_name"`
	ToolInput json.RawMessage `json:"tool_input,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
}

// Decision is the human's answer.
type Decision struct {
	Allow bool
	// Message is shown to the agent, typically to explain a refusal.
	Message string
}

// Queue holds the pending requests. It is safe for concurrent use.
type Queue struct {
	mu      sync.Mutex
	pending map[string]*waiting
	// OnChange, when set, is called after a request is added or removed.
	OnChange func()
}

type waiting struct {
	req      Request
	decision chan Decision
}

// NewQueue returns an empty queue.
func NewQueue() *Queue {
	return &Queue{pending: map[string]*waiting{}}
}

// Ask files a request and blocks until the human decides or ctx ends. In the
// latter case it returns ErrNoDecision and the request leaves the queue.
func (q *Queue) Ask(ctx context.Context, req Request) (Decision, error) {
	w := &waiting{req: req, decision: make(chan Decision, 1)}

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
	}
}

// Decide answers a pending request.
func (q *Queue) Decide(id string, d Decision) error {
	q.mu.Lock()

	w, ok := q.pending[id]
	if ok {
		delete(q.pending, id) // a request is decided once
	}
	q.mu.Unlock()

	if !ok {
		return ErrNotFound
	}

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
