package e2e

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/qwackididuck/nobodyinc/internal/daemon"
	"github.com/qwackididuck/nobodyinc/internal/messaging"
	"github.com/qwackididuck/nobodyinc/internal/org"
)

// Conversations between agents that nobody answers are closed by force
// (messaging.Closer): from the wastebasket, when their ticket is closed, and
// when they are old. The agent that waited is told by nobody.

// openQuery files an unanswered query of dev1 to dev2 directly in the store:
// the fake agents would answer it. The message is sent `age` ago.
func openQuery(t *testing.T, d *daemon.Daemon, id, ticket string, age time.Duration) {
	t.Helper()

	err := d.Store.InsertMessage(context.Background(), messaging.Message{
		ID: id, Conversation: id, From: "dev1@dev", To: "dev2@dev", Act: messaging.ActQuery, Priority: messaging.Normal,
		Subject: "Which index " + id + "?", Ticket: ticket, SentAt: time.Now().UTC().Add(-age),
	})
	if err != nil {
		t.Fatal(err)
	}
}

// stored reports whether a message is still in the store.
func stored(t *testing.T, d *daemon.Daemon, id string) bool {
	t.Helper()

	_, err := d.Store.GetMessage(context.Background(), id)

	return err == nil
}

// notices returns the notices nobody sent to dev1 about closed conversations.
func notices(t *testing.T, d *daemon.Daemon) []messaging.Message {
	t.Helper()

	all, err := d.Store.RecentMessages(context.Background(), 200)
	if err != nil {
		t.Fatal(err)
	}

	var found []messaging.Message

	for _, m := range all {
		if m.From == org.SystemAddress && m.To == "dev1@dev" && strings.HasPrefix(m.Subject, "Conversation closed") {
			found = append(found, m)
		}
	}

	return found
}

func TestOpenConversationsAreClosedFromTheWastebasket(t *testing.T) {
	t.Parallel()

	d := startDaemon(t, staffProject(t))
	openQuery(t, d, "q1", "", time.Minute)

	// Emptying the wastebasket keeps an open conversation...
	call(t, d, http.MethodDelete, "/api/messages?scope=agents", "", http.StatusOK)

	if !stored(t, d, "q1") {
		t.Fatal("the wastebasket deleted an open conversation")
	}

	// ...unless asked to close it too.
	var deleted struct {
		IDs []string `json:"ids"`
	}

	decodeBody(t, call(t, d, http.MethodDelete, "/api/messages?scope=agents&include_open=true", "", http.StatusOK), &deleted)

	if len(deleted.IDs) != 1 || deleted.IDs[0] != "q1" || stored(t, d, "q1") {
		t.Fatalf("deleted %v, want q1 gone", deleted.IDs)
	}

	if got := notices(t, d); len(got) != 1 || !strings.Contains(got[0].Body, "closed it from the wastebasket") {
		t.Fatalf("dev1 should be told its query was closed: %+v", got)
	}
}

func TestOpenConversationsAreClosedWithTheirTicket(t *testing.T) {
	t.Parallel()

	d := startDaemon(t, staffProject(t))

	call(t, d, http.MethodPost, "/api/tickets", `{"title":"Add an index","team":"dev"}`, http.StatusCreated)
	call(t, d, http.MethodPost, "/api/tickets", `{"title":"Other work","team":"dev"}`, http.StatusCreated)
	openQuery(t, d, "about-t1", "T-1", time.Minute)
	openQuery(t, d, "about-t2", "T-2", time.Minute)

	call(t, d, http.MethodPost, "/api/tickets/T-1/status", `{"status":"done"}`, http.StatusOK)

	eventually(t, "the conversation about T-1 is closed", func() bool { return !stored(t, d, "about-t1") })

	if !stored(t, d, "about-t2") {
		t.Fatal("closing T-1 closed a conversation about T-2")
	}

	if got := notices(t, d); len(got) != 1 || !strings.Contains(got[0].Body, "its ticket T-1 is closed") {
		t.Fatalf("dev1 should be told why: %+v", got)
	}

	call(t, d, http.MethodPost, "/api/tickets/T-2/status", `{"status":"canceled"}`, http.StatusOK)
	eventually(t, "the conversation about T-2 is closed", func() bool { return !stored(t, d, "about-t2") })
}

// An old open conversation is closed when the project opens again; a recent
// one stays.
func TestOldOpenConversationsAreClosedWhenTheProjectOpens(t *testing.T) {
	t.Parallel()

	o := staffProject(t)
	db := filepath.Join(t.TempDir(), "nobody.db")

	first := startDaemonWithDB(t, o, db)
	openQuery(t, first, "old", "", 10*24*time.Hour)
	openQuery(t, first, "recent", "", 24*time.Hour)

	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	second := startDaemonWithDB(t, o, db)

	eventually(t, "the old conversation is closed", func() bool { return !stored(t, second, "old") })

	if !stored(t, second, "recent") {
		t.Fatal("a conversation of yesterday was closed")
	}

	if got := notices(t, second); len(got) != 1 || !strings.Contains(got[0].Body, "nobody answered for 7 days") {
		t.Fatalf("dev1 should be told why: %+v", got)
	}
}
