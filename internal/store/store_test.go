package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/qwackididuck/nobodyinc/internal/messaging"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()

	s, err := Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { s.Close() })

	return s
}

func msg(id, to string, p messaging.Priority, sentAt time.Time) messaging.Message {
	return messaging.Message{
		ID: id, Conversation: id, From: "dev1@dev", To: to,
		Act: messaging.ActQuery, Priority: p, Subject: "s-" + id, Body: "b-" + id, SentAt: sentAt,
	}
}

func TestMessageLifecycle(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := openTestStore(t)
	t0 := time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC)

	for _, m := range []messaging.Message{
		msg("m2", "archi@specs", messaging.Normal, t0.Add(time.Minute)),
		msg("m1", "archi@specs", messaging.Normal, t0),
		msg("m3", "archi@specs", messaging.Urgent, t0),
		msg("m4", "secu@specs", messaging.Normal, t0),
	} {
		if err := s.InsertMessage(ctx, m); err != nil {
			t.Fatal(err)
		}
	}

	got, err := s.GetMessage(ctx, "m1")
	if err != nil {
		t.Fatal(err)
	}

	if got.Body != "b-m1" || !got.SentAt.Equal(t0) || got.DeliveredAt != nil {
		t.Fatalf("GetMessage = %+v", got)
	}

	assertPending(t, s, "archi@specs", messaging.Normal, "m1", "m2")
	assertPending(t, s, "archi@specs", messaging.Urgent, "m3")
	assertPending(t, s, "secu@specs", messaging.Normal, "m4")

	if err := s.MarkDelivered(ctx, "archi@specs", []string{"m1", "m3", "m4"}, t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	// m4 belongs to secu@specs: archi cannot acknowledge it.
	assertPending(t, s, "secu@specs", messaging.Normal, "m4")
	assertPending(t, s, "archi@specs", messaging.Normal, "m2")
	assertPending(t, s, "archi@specs", messaging.Urgent)

	if err := s.Requeue(ctx, "archi@specs", []string{"m3"}); err != nil {
		t.Fatal(err)
	}

	assertPending(t, s, "archi@specs", messaging.Urgent, "m3")

	delivered, err := s.GetMessage(ctx, "m1")
	if err != nil {
		t.Fatal(err)
	}

	if delivered.DeliveredAt == nil || !delivered.DeliveredAt.Equal(t0.Add(time.Hour)) {
		t.Fatalf("delivered_at = %v", delivered.DeliveredAt)
	}

	recent, err := s.RecentMessages(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}

	if len(recent) != 2 || recent[1].ID != "m2" {
		t.Fatalf("RecentMessages = %v, want the 2 latest ending with m2", ids(recent))
	}
}

func TestMigrationsAreIdempotent(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "nobody.db")
	for i := range 2 { // reopening applies nothing twice
		s, err := Open(context.Background(), path)
		if err != nil {
			t.Fatalf("open #%d: %v", i+1, err)
		}

		s.Close()
	}
}

func TestGetMessageNotFound(t *testing.T) {
	t.Parallel()

	_, err := openTestStore(t).GetMessage(context.Background(), "nope")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
}

func assertPending(t *testing.T, s *Store, to string, p messaging.Priority, want ...string) {
	t.Helper()

	got, err := s.PendingMessages(context.Background(), to, p)
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != len(want) {
		t.Fatalf("%s %s tray = %v, want %v", to, p, ids(got), want)
	}

	for i := range want {
		if got[i].ID != want[i] {
			t.Fatalf("%s %s tray = %v, want %v", to, p, ids(got), want)
		}
	}
}

func ids(messages []messaging.Message) []string {
	out := make([]string, len(messages))
	for i, m := range messages {
		out[i] = m.ID
	}

	return out
}
