package launcher

import (
	"errors"
	"sync"
	"time"

	"github.com/qwackididuck/nobodyinc/internal/schedule"
)

// Errors of the leases.
var (
	errHeld     = errors.New("the project is open in another window")
	errDeleting = errors.New("the project is being deleted")
)

// leases says which window holds which project (ADR-0014): a project runs
// only while a window holds it, and a window holds one project at most.
//
//   - A window acquires a project when it connects. The same window
//     connecting again gets its hold back; another window is refused while
//     the holder is connected, unless it takes the project over (the holder
//     is then told, through onTaken).
//   - When the holder's connection drops, the hold lasts for a grace period:
//     a page reload or a short network loss does not stop the project. Any
//     window may take a disconnected hold without taking over. When the
//     grace period runs out, onExpired stops the project.
//   - A window that switches project releases its hold at once.
//
// The grace period is measured with the injected clock, so that tests never
// wait for real time.
type leases struct {
	clock     schedule.Clock
	grace     time.Duration
	onExpired func(project string)

	mu        sync.Mutex
	byProject map[string]*hold
	// deleting lists the projects being deleted: nobody may acquire them.
	deleting map[string]bool
}

// hold is a window's hold on a project.
type hold struct {
	window    string
	connected bool
	onTaken   func()
	// cancelGrace stops the grace period running while disconnected.
	cancelGrace chan struct{}
}

func newLeases(clock schedule.Clock, grace time.Duration, onExpired func(project string)) *leases {
	return &leases{
		clock:     clock,
		grace:     grace,
		onExpired: onExpired,
		byProject: make(map[string]*hold),
		deleting:  make(map[string]bool),
	}
}

// acquire gives the project to window, connected, and returns the hold of
// this connection. onTaken is called, once, if another connection takes the
// project over later.
func (ls *leases) acquire(project, window string, takeover bool, onTaken func()) (*hold, error) {
	ls.mu.Lock()
	defer ls.mu.Unlock()

	if ls.deleting[project] {
		return nil, errDeleting
	}

	current := ls.byProject[project]

	switch {
	case current == nil:
	case current.connected && current.window != window && !takeover:
		return nil, errHeld
	default:
		// The same window reconnecting, a takeover, or a hold in its grace
		// period. A connection still open with the same window id (a
		// duplicated browser tab copies it, or the launcher has not noticed
		// the old connection drop yet) loses to the new one.
		current.stopGrace()

		if current.connected {
			go current.onTaken()
		}
	}

	h := &hold{window: window, connected: true, onTaken: onTaken}
	ls.byProject[project] = h

	return h, nil
}

// disconnect records that the connection of hold h dropped: the hold lasts
// for the grace period. It does nothing if h no longer holds the project.
func (ls *leases) disconnect(project string, h *hold) {
	ls.mu.Lock()
	defer ls.mu.Unlock()

	if ls.byProject[project] != h || !h.connected {
		return
	}

	h.connected = false
	h.cancelGrace = make(chan struct{})

	timer := ls.clock.NewTimer(ls.grace)

	go func(cancel <-chan struct{}) {
		select {
		case <-timer.C():
			ls.expire(project, h)
		case <-cancel:
			timer.Stop()
		}
	}(h.cancelGrace)
}

// expire ends a hold whose grace period ran out, unless it was taken or
// reconnected meanwhile.
func (ls *leases) expire(project string, h *hold) {
	ls.mu.Lock()

	if ls.byProject[project] != h || h.connected {
		ls.mu.Unlock()

		return
	}

	delete(ls.byProject, project)
	ls.mu.Unlock()

	ls.onExpired(project)
}

// release ends hold h at once.
func (ls *leases) release(project string, h *hold) {
	ls.mu.Lock()

	if ls.byProject[project] != h {
		ls.mu.Unlock()

		return
	}

	h.stopGrace()
	delete(ls.byProject, project)
	ls.mu.Unlock()

	ls.onExpired(project)
}

// held reports whether a window holds the project, connected or within its
// grace period.
func (ls *leases) held(project string) bool {
	ls.mu.Lock()
	defer ls.mu.Unlock()

	return ls.byProject[project] != nil
}

// heldElsewhere reports whether another window than window is connected to
// the project.
func (ls *leases) heldElsewhere(project, window string) bool {
	ls.mu.Lock()
	defer ls.mu.Unlock()

	h := ls.byProject[project]

	return h != nil && h.connected && h.window != window
}

// startDeleting takes the project away from every window so that it can be
// deleted, and keeps windows from acquiring it until finishDeleting. It is
// refused while another window than window is connected to it; window
// itself loses its hold.
func (ls *leases) startDeleting(project, window string) error {
	ls.mu.Lock()
	defer ls.mu.Unlock()

	if ls.deleting[project] {
		return errDeleting
	}

	if h := ls.byProject[project]; h != nil {
		if h.connected && h.window != window {
			return errHeld
		}

		h.stopGrace()
		delete(ls.byProject, project)
	}

	ls.deleting[project] = true

	return nil
}

// finishDeleting lets windows acquire the project again: the deletion
// failed, or it succeeded and the project is no longer known (opening the
// folder again makes it a new project).
func (ls *leases) finishDeleting(project string) {
	ls.mu.Lock()
	defer ls.mu.Unlock()

	delete(ls.deleting, project)
}

func (h *hold) stopGrace() {
	if h.cancelGrace != nil {
		close(h.cancelGrace)
		h.cancelGrace = nil
	}
}
