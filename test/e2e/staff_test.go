package e2e

import (
	"context"
	"testing"

	"github.com/qwackididuck/nobodyinc/internal/daemon"
	"github.com/qwackididuck/nobodyinc/internal/messaging"
	"github.com/qwackididuck/nobodyinc/internal/org"
	"github.com/qwackididuck/nobodyinc/internal/runtime"
	"github.com/qwackididuck/nobodyinc/internal/work"
)

// staffProject writes a project whose dev team has two members, and returns
// it as a live organization, as nobodyd runs it.
func staffProject(t *testing.T) *org.Live {
	t.Helper()

	o := &org.Org{
		ProjectDir: t.TempDir(),
		Teams: []org.Team{{ID: "dev", Agents: []org.Agent{
			{Name: "dev1", Role: "Developer"},
			{Name: "dev2", Role: "Developer"},
		}}},
	}
	if err := org.WriteProject(o); err != nil {
		t.Fatal(err)
	}

	loaded, err := org.LoadProject(o.ProjectDir)
	if err != nil {
		t.Fatal(err)
	}

	return org.NewLive(loaded)
}

// changeDev1 rewrites dev1's agent file (nil removes it: dismissal), reloads
// the organization and applies the change, as the API will.
func changeDev1(t *testing.T, d *daemon.Daemon, live *org.Live, a *org.Agent) {
	t.Helper()

	dir := live.Current().ProjectDir

	var err error
	if a != nil {
		err = org.WriteAgent(dir, "dev", *a)
	} else {
		err = org.RemoveAgent(dir, "dev", "dev1")
	}

	if err != nil {
		t.Fatal(err)
	}

	if _, err := live.Reload(); err != nil {
		t.Fatal(err)
	}

	if err := d.Supervisor.Apply(context.WithoutCancel(t.Context()), "dev1@dev", runtime.ApplyOptions{}); err != nil {
		t.Fatal(err)
	}
}

// An agent goes on leave with a ticket in progress: once it stops, a teammate
// takes the ticket over; mail sent to it waits, and the sender is told; team
// addressing skips it. Back from leave, it resumes and reads its mail.
// Dismissed, its undelivered mail is returned to the senders.
func TestLeaveReturnAndDismissal(t *testing.T) {
	t.Parallel()

	live := staffProject(t)
	d := startDaemon(t, live)
	ctx := context.Background()

	// dev1 holds a ticket in progress (inserted directly: no notice, so the
	// fake agent does not finish it on its own).
	tk, err := d.Store.InsertTicket(ctx, work.Ticket{
		Title: "Half-done work", Team: "dev", Assignee: "dev1@dev", Status: work.StatusInProgress, CreatedBy: org.HumanAddress,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Leave.
	changeDev1(t, d, live, &org.Agent{Name: "dev1", Role: "Developer", OnLeave: true})

	eventually(t, "dev1 stopped", func() bool { return d.Statuses.Get("dev1@dev") == runtime.StatusStopped })

	details := waitForDone(t, d, tk.ID)
	if details.Ticket.Assignee != "dev2@dev" {
		t.Fatalf("ticket finished by %q, want the teammate dev2@dev", details.Ticket.Assignee)
	}

	if len(details.Comments) == 0 || details.Comments[0].Author != org.SystemAddress {
		t.Fatalf("the release should be explained in a comment from nobody: %+v", details.Comments)
	}

	sent := send(t, d, "dev1@dev", "While you are away")
	eventually(t, "the sender told dev1 is on leave", func() bool {
		return hasNotice(t, d, org.HumanAddress, "dev1@dev is on leave")
	})

	if pending := pendingFor(t, d, "dev1@dev"); len(pending) != 1 || pending[0].ID != sent.ID {
		t.Fatalf("mail to an agent on leave should wait in its tray, got %+v", pending)
	}

	if got := send(t, d, "@dev", "Anyone?"); got.To != "dev2@dev" {
		t.Fatalf("@dev picked %s, want dev2@dev (dev1 is on leave)", got.To)
	}

	// Return: resumed, the waiting mail is delivered with its first prompt.
	changeDev1(t, d, live, &org.Agent{Name: "dev1", Role: "Developer"})

	eventually(t, "dev1 back and idle", func() bool { return d.Statuses.Get("dev1@dev") == runtime.StatusIdle })
	eventually(t, "the waiting mail delivered on return", func() bool { return len(pendingFor(t, d, "dev1@dev")) == 0 })

	// Dismissal: mail that can no longer be read goes back to its sender.
	changeDev1(t, d, live, &org.Agent{Name: "dev1", Role: "Developer", OnLeave: true})
	eventually(t, "dev1 on leave again", func() bool { return d.Statuses.Get("dev1@dev") == runtime.StatusStopped })

	send(t, d, "dev1@dev", "Never read")
	changeDev1(t, d, live, nil)

	eventually(t, "the sender told the message was not delivered", func() bool {
		return hasNotice(t, d, org.HumanAddress, "Message not delivered: Never read")
	})

	if pending := pendingFor(t, d, "dev1@dev"); len(pending) != 0 {
		t.Fatalf("a dismissed agent's tray should be cleared, got %+v", pending)
	}
}

func send(t *testing.T, d *daemon.Daemon, to, subject string) messaging.Message {
	t.Helper()

	m, err := d.Messages.Send(context.Background(), messaging.Draft{
		From: org.HumanAddress, To: to, Act: messaging.ActInform, Priority: messaging.Normal, Subject: subject,
	})
	if err != nil {
		t.Fatal(err)
	}

	return m
}

func pendingFor(t *testing.T, d *daemon.Daemon, address string) []messaging.Message {
	t.Helper()

	var all []messaging.Message

	for _, p := range []messaging.Priority{messaging.Normal, messaging.Urgent} {
		pending, err := d.Store.PendingMessages(context.Background(), address, p)
		if err != nil {
			t.Fatal(err)
		}

		all = append(all, pending...)
	}

	return all
}

func hasNotice(t *testing.T, d *daemon.Daemon, to, subject string) bool {
	t.Helper()

	messages, err := d.Store.RecentMessages(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}

	for _, m := range messages {
		if m.From == org.SystemAddress && m.To == to && m.Subject == subject {
			return true
		}
	}

	return false
}
