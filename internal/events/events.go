// Package events announces changes to the control room, which listens to them
// through GET /api/events (Server-Sent Events, docs/events.md).
//
// Services publish an Event on the Bus when something changes; the HTTP
// layer streams them to every connected browser. Most events only name what
// changed (an invalidation): the UI reads that resource again through the
// REST API, which stays the source of truth. A few carry data for the
// office animations (an agent's status, a message being sent).
//
// The bus never blocks a publisher: a subscriber that falls behind is
// dropped (its channel is closed), and the browser reconnects, receives
// "hello" and reloads everything. Nothing is replayed.
package events

import "sync"

// Event types, as sent in the "event:" field of the stream.
const (
	// Hello is the first event of every connection: reload everything.
	Hello = "hello"
	// AgentStatus carries an AgentStatusData.
	AgentStatus = "agent.status"
	// MessageSent carries the messaging.Message that was sent.
	MessageSent = "message.sent"
	// MessagesDelivered carries a DeliveredData.
	MessagesDelivered = "message.delivered"
	// MessagesDeleted carries a DeletedData: the wastebasket was emptied.
	MessagesDeleted = "messages.deleted"
	// ApprovalsChanged: a permission request was added or answered.
	ApprovalsChanged = "approvals.changed"
	// AutomationChanged: the project's automatic mode was switched on or off
	// (docs/automation.md).
	AutomationChanged = "automation.changed"
	// TicketsChanged carries a ChangedData with the ticket ID.
	TicketsChanged = "tickets.changed"
	// FeaturesChanged carries a ChangedData with the feature ID.
	FeaturesChanged = "features.changed"
	// OrgChanged: an agent was recruited, edited, put on leave, brought back
	// or dismissed.
	OrgChanged = "org.changed"
	// CatalogChanged: a template was saved or deleted.
	CatalogChanged = "catalog.changed"
	// SchedulesChanged: a schedule, one of its occurrences or a director's
	// proposal changed (ADR-0012).
	SchedulesChanged = "schedules.changed"
	// SpendChanged: agents spent; published at most once a second (ADR-0013).
	SpendChanged = "spend.changed"
	// BudgetsChanged: the budgets file, a ticket's or feature's budget, a
	// hold, a budget request or a cap proposal changed (ADR-0013).
	BudgetsChanged = "budgets.changed"
	// KBChanged carries a KBChangedData: pages of the knowledge base changed,
	// or the knowledge base itself (ADR-0019).
	KBChanged = "kb.changed"
	// KBRead carries a KBReadData: an agent used a knowledge tool, or is
	// about to read or change a file of the knowledge base with its own
	// tools, and the office walks it to the library.
	KBRead = "kb.read"
)

// KBChangedData is the data of a KBChanged event.
type KBChangedData struct {
	Pages   int `json:"pages"`
	Added   int `json:"added"`
	Removed int `json:"removed"`
	Changed int `json:"changed"`
	// Reset is true when the folder itself changed (set up, moved, removed):
	// the control room reloads without announcing new pages.
	Reset bool `json:"reset"`
}

// KBReadData is the data of a KBRead event.
type KBReadData struct {
	Agent string `json:"agent"`
	// Tool is kb_search, kb_read or kb_list, or a file tool (Read, Edit,
	// Write, MultiEdit, NotebookEdit).
	Tool string `json:"tool"`
	// Subject is the query, the page or the folder; for a file tool, the
	// file's path in the knowledge folder.
	Subject string `json:"subject"`
}

// Event is one change. Data is encoded as JSON in the stream.
type Event struct {
	Type string
	Data any
}

// AgentStatusData is the data of an AgentStatus event.
type AgentStatusData struct {
	Address string `json:"address"`
	Status  string `json:"status"`
}

// DeliveredData is the data of a MessagesDelivered event.
type DeliveredData struct {
	Recipient string   `json:"recipient"`
	IDs       []string `json:"ids"`
}

// DeletedData is the data of a MessagesDeleted event.
type DeletedData struct {
	IDs []string `json:"ids"`
}

// ChangedData names the record that changed.
type ChangedData struct {
	ID string `json:"id"`
}

// Empty is the data of events that carry nothing.
type Empty struct{}

// subscriberBuffer is how many events a subscriber may lag behind before it
// is dropped.
const subscriberBuffer = 256

// Bus fans events out to subscribers. The zero value is not usable: call
// NewBus. A nil *Bus ignores everything, so that services can publish
// without checking whether anyone listens.
type Bus struct {
	mu     sync.Mutex
	subs   map[chan Event]struct{}
	closed bool
}

// NewBus returns a bus without subscribers.
func NewBus() *Bus {
	return &Bus{subs: map[chan Event]struct{}{}}
}

// Publish sends e to every subscriber without waiting. A subscriber whose
// buffer is full is dropped.
func (b *Bus) Publish(e Event) {
	if b == nil {
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	for ch := range b.subs {
		select {
		case ch <- e:
		default:
			delete(b.subs, ch)
			close(ch)
		}
	}
}

// Subscribe returns a channel receiving every event published from now on,
// and a function to stop receiving them. The channel is closed when the
// subscriber is dropped, unsubscribes, or the bus is closed.
func (b *Bus) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, subscriberBuffer)

	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		close(ch)

		return ch, func() {}
	}

	b.subs[ch] = struct{}{}

	return ch, func() {
		b.mu.Lock()
		defer b.mu.Unlock()

		if _, ok := b.subs[ch]; ok {
			delete(b.subs, ch)
			close(ch)
		}
	}
}

// Close closes every subscriber's channel; later subscriptions are closed at
// once. Streams use it to end when the daemon stops (an HTTP server's
// Shutdown does not interrupt them).
func (b *Bus) Close() {
	if b == nil {
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return
	}

	b.closed = true

	for ch := range b.subs {
		delete(b.subs, ch)
		close(ch)
	}
}
