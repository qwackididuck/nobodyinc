package store

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/qwackididuck/nobodyinc/internal/messaging"
)

// between is a message from one address to another, sent at t0 + minute.
func between(id, from, to string, minute int) messaging.Message {
	t0 := time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC)

	return messaging.Message{
		ID: id, Conversation: id, From: from, To: to,
		Act: messaging.ActInform, Priority: messaging.Normal, Subject: id, SentAt: t0.Add(time.Duration(minute) * time.Minute),
	}
}

func TestMessageFilters(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := openTestStore(t)

	for _, m := range []messaging.Message{
		between("m1", "human", "dev1@dev", 1),
		between("m2", "dev1@dev", "human", 2),
		between("m3", "dev1@dev", "archi@specs", 3),
		between("m4", "archi@specs", "dev1@dev", 4),
		between("m5", "god", "archi@specs", 5),
		between("m6", "nobody", "dev1@dev", 6),
		between("m7", "human", "archi@specs", 7),
		between("m8", "dev1@dev", "archi@specs", 8), // never delivered
	} {
		if err := s.InsertMessage(ctx, m); err != nil {
			t.Fatal(err)
		}
	}

	delivered := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	for recipient, ids := range map[string][]string{"archi@specs": {"m3", "m5", "m7"}, "dev1@dev": {"m4", "m6"}} {
		if err := s.MarkDelivered(ctx, recipient, ids, delivered); err != nil {
			t.Fatal(err)
		}
	}

	excluded := []string{"human", "nobody"}

	tests := []struct {
		name  string
		query func() ([]messaging.Message, error)
		want  []string
	}{
		{"conversation with dev1, both directions", func() ([]messaging.Message, error) {
			return s.ConversationMessages(ctx, "human", "dev1@dev", 100)
		}, []string{"m1", "m2"}},
		{"conversation keeps the latest, oldest first", func() ([]messaging.Message, error) {
			return s.ConversationMessages(ctx, "human", "dev1@dev", 1)
		}, []string{"m2"}},
		{"between agents: delivered only, director included, human and nobody excluded", func() ([]messaging.Message, error) {
			return s.DeliveredMessagesAmong(ctx, excluded, 100)
		}, []string{"m3", "m4", "m5"}},
		{"between agents keeps the latest", func() ([]messaging.Message, error) {
			return s.DeliveredMessagesAmong(ctx, excluded, 2)
		}, []string{"m4", "m5"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := tt.query()
			if err != nil {
				t.Fatal(err)
			}

			if ids := messageIDs(got); !slices.Equal(ids, tt.want) {
				t.Fatalf("got %v, want %v", ids, tt.want)
			}
		})
	}
}

func TestOnDeliveredReportsNewDeliveriesOnly(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := openTestStore(t)

	var reported [][]string

	s.OnDelivered = func(_ string, ids []string) { reported = append(reported, ids) }

	if err := s.InsertMessage(ctx, between("m1", "human", "dev1@dev", 1)); err != nil {
		t.Fatal(err)
	}

	at := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	for range 2 {
		if err := s.MarkDelivered(ctx, "dev1@dev", []string{"m1"}, at); err != nil {
			t.Fatal(err)
		}
	}

	if len(reported) != 1 || !slices.Equal(reported[0], []string{"m1"}) {
		t.Fatalf("reported = %v, want one delivery of m1", reported)
	}
}

func messageIDs(ms []messaging.Message) []string {
	ids := make([]string, 0, len(ms))
	for _, m := range ms {
		ids = append(ids, m.ID)
	}

	return ids
}

func TestDeleteFinishedConversations(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := openTestStore(t)

	// in builds a message of a conversation.
	in := func(conversation, id, from, to string, act messaging.Act, minute int) messaging.Message {
		m := between(id, from, to, minute)
		m.Conversation, m.Act = conversation, act

		return m
	}

	messages := []messaging.Message{
		in("finished", "f1", "dev1@dev", "archi@specs", messaging.ActQuery, 1),
		in("finished", "f2", "archi@specs", "dev1@dev", messaging.ActInform, 2),
		in("unanswered", "u1", "dev1@dev", "archi@specs", messaging.ActQuery, 3),
		in("undelivered", "d1", "dev1@dev", "archi@specs", messaging.ActDone, 4),
		in("with-human", "h1", "human", "dev1@dev", messaging.ActInform, 5),
		in("with-nobody", "n1", "nobody", "dev1@dev", messaging.ActInform, 6),
		in("to-director", "g1", "dev1@dev", "god", messaging.ActDone, 7),
	}
	for _, m := range messages {
		if err := s.InsertMessage(ctx, m); err != nil {
			t.Fatal(err)
		}
	}

	at := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	for recipient, ids := range map[string][]string{
		"archi@specs": {"f1", "u1"}, "dev1@dev": {"f2", "h1", "n1"}, "god": {"g1"},
	} {
		if err := s.MarkDelivered(ctx, recipient, ids, at); err != nil {
			t.Fatal(err)
		}
	}

	deleted, err := s.DeleteFinishedConversations(ctx, []string{"human", "nobody"}, messaging.ActsRequiringReply())
	if err != nil {
		t.Fatal(err)
	}

	slices.Sort(deleted)

	if want := []string{"f1", "f2", "g1"}; !slices.Equal(deleted, want) {
		t.Fatalf("deleted %v, want %v (finished conversations between agents only)", deleted, want)
	}

	left, err := s.RecentMessages(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}

	if ids := messageIDs(left); !slices.Equal(ids, []string{"u1", "d1", "h1", "n1"}) {
		t.Fatalf("left %v", ids)
	}
}
