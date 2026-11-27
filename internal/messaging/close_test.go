package messaging

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"
)

// openStore is a store with a fixed list of open conversations.
type openStore struct {
	memStore

	open    []Message
	deleted []string
}

func (s *openStore) OpenAgentConversations(_ context.Context, _ []string, _ []Act, tickets []string) ([]Message, error) {
	var found []Message

	for _, m := range s.open {
		if tickets == nil || slices.Contains(tickets, m.Ticket) {
			found = append(found, m)
		}
	}

	return found, nil
}

func (s *openStore) DeleteConversations(_ context.Context, conversations []string) ([]string, error) {
	s.deleted = append(s.deleted, conversations...)

	return conversations, nil
}

func TestCloserClose(t *testing.T) {
	t.Parallel()

	noon := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	open := []Message{
		{ID: "old", Conversation: "c-old", From: "dev1@dev", To: "archi@specs", Act: ActQuery, Subject: "old", Ticket: "T-1", SentAt: noon.Add(-48 * time.Hour)},
		{ID: "new", Conversation: "c-new", From: "tester@qa", To: "dev1@dev", Act: ActRequest, Subject: "new", Ticket: "T-2", SentAt: noon},
	}

	tests := []struct {
		name       string
		scope      CloseScope
		wantClosed []string // conversations
		wantTold   []string // agents told
	}{
		{name: "everything", scope: CloseScope{Reason: "r"}, wantClosed: []string{"c-old", "c-new"}, wantTold: []string{"dev1@dev", "tester@qa"}},
		{name: "a ticket", scope: CloseScope{Tickets: []string{"T-2"}, Reason: "r"}, wantClosed: []string{"c-new"}, wantTold: []string{"tester@qa"}},
		{name: "no ticket given", scope: CloseScope{Tickets: []string{}, Reason: "r"}},
		{name: "older than", scope: CloseScope{Before: noon.Add(-time.Hour), Reason: "r"}, wantClosed: []string{"c-old"}, wantTold: []string{"dev1@dev"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store := &openStore{memStore: memStore{}, open: open}
			svc := newService(10)
			svc.Store = store

			var announced []string

			c := &Closer{Store: store, Messages: svc, OnDeleted: func(ids []string) { announced = append(announced, ids...) }}

			if _, err := c.Close(context.Background(), tt.scope); err != nil {
				t.Fatal(err)
			}

			if !slices.Equal(store.deleted, tt.wantClosed) {
				t.Fatalf("closed %v, want %v", store.deleted, tt.wantClosed)
			}

			if !slices.Equal(announced, tt.wantClosed) {
				t.Fatalf("announced %v, want %v", announced, tt.wantClosed)
			}

			var told []string

			for _, m := range store.memStore {
				if m.From != "nobody" || m.Act != ActInform || !strings.Contains(m.Body, "closed without an answer: r") {
					t.Errorf("unexpected message %+v", m)
				}

				told = append(told, m.To)
			}

			slices.Sort(told)

			if !slices.Equal(told, tt.wantTold) {
				t.Fatalf("told %v, want %v", told, tt.wantTold)
			}
		})
	}
}
