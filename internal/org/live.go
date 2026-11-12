package org

import "sync"

// Source gives the current organization. Components that live as long as the
// daemon (supervisor, post office, ticket service, HTTP API...) hold a Source
// rather than an *Org, and ask for Current() each time they need it, so that
// recruiting or editing an agent takes effect without restarting the daemon.
//
// The *Org returned is a snapshot: it is never modified, a change replaces it
// as a whole (see Live). Callers may keep it for the duration of one
// operation, so that the operation sees a consistent organization.
type Source interface {
	Current() *Org
}

// Current makes a fixed organization a Source of itself, for tests and tools
// that never change it.
func (o *Org) Current() *Org {
	return o
}

// Live is the organization of a running daemon: a snapshot that is replaced
// as a whole when the project's .nobody/ changes.
type Live struct {
	mu sync.RWMutex
	o  *Org
}

// NewLive starts from an already loaded organization.
func NewLive(o *Org) *Live {
	return &Live{o: o}
}

// Current returns the current snapshot. It must not be modified.
func (l *Live) Current() *Org {
	l.mu.RLock()
	defer l.mu.RUnlock()

	return l.o
}

// Reload reads the project's .nobody/ again and makes it current. On error
// (e.g. a broken file being edited by hand) the current snapshot is kept.
func (l *Live) Reload() (*Org, error) {
	o, err := LoadProject(l.Current().ProjectDir)
	if err != nil {
		return nil, err
	}

	l.mu.Lock()
	l.o = o
	l.mu.Unlock()

	return o, nil
}
