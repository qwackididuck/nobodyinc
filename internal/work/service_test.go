package work_test

import (
	"context"
	"errors"
	"testing"

	"github.com/qwackididuck/nobodyinc/internal/org"
	"github.com/qwackididuck/nobodyinc/internal/store"
	"github.com/qwackididuck/nobodyinc/internal/work"
)

// newService: specs (archi), dev (dev1, dev2), qa (tester); channel dev → specs.
func newService(t *testing.T) (*work.Service, *[]work.Event) {
	t.Helper()

	st, err := store.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { st.Close() })

	var events []work.Event

	return &work.Service{
		Store: st,
		Org: &org.Org{
			Teams: []org.Team{
				{ID: "specs", Agents: []org.Agent{{Name: "archi"}}},
				{ID: "dev", Agents: []org.Agent{{Name: "dev1"}, {Name: "dev2"}}},
				{ID: "qa", Agents: []org.Agent{{Name: "tester"}}},
			},
			Channels: []org.Channel{{From: "dev", To: "specs"}},
			God:      &org.Agent{Name: "god"},
		},
		OnEvent: func(e work.Event) { events = append(events, e) },
	}, &events
}

// create files a ticket as the human.
func create(t *testing.T, s *work.Service, team string, blockedBy ...string) work.Ticket {
	t.Helper()

	tk, err := s.Create(context.Background(), org.HumanAddress, work.Draft{Title: "Do " + team + " work", Team: team, BlockedBy: blockedBy})
	if err != nil {
		t.Fatal(err)
	}

	return tk
}

func TestTicketLifecycle(t *testing.T) {
	t.Parallel()

	s, events := newService(t)
	ctx := context.Background()

	tk := create(t, s, "dev")
	if tk.ID != "T-1" || tk.Status != work.StatusOpen || tk.Assignee != "" {
		t.Fatalf("created ticket = %+v", tk)
	}

	claimed, err := s.Claim(ctx, "dev1@dev", "") // next available in the queue
	if err != nil {
		t.Fatal(err)
	}

	if claimed.ID != "T-1" || claimed.Assignee != "dev1@dev" || claimed.Status != work.StatusInProgress {
		t.Fatalf("claimed ticket = %+v", claimed)
	}

	if _, err := s.Claim(ctx, "dev2@dev", "T-1"); !errors.Is(err, work.ErrConflict) {
		t.Fatalf("claiming someone else's ticket: error = %v, want ErrConflict", err)
	}

	if _, err := s.AddDeliverable(ctx, "dev1@dev", "T-1", work.Deliverable{Kind: "commit", Reference: "abc123", Summary: "Add the endpoint"}); err != nil {
		t.Fatal(err)
	}

	if _, err := s.Comment(ctx, "archi@specs", "T-1", "Looks good"); err != nil {
		t.Fatal(err)
	}

	if _, err := s.SetStatus(ctx, "dev2@dev", "T-1", work.StatusDone); !errors.Is(err, work.ErrForbidden) {
		t.Fatalf("closing someone else's ticket: error = %v, want ErrForbidden", err)
	}

	done, err := s.SetStatus(ctx, "dev1@dev", "T-1", work.StatusDone)
	if err != nil {
		t.Fatal(err)
	}

	if done.Status != work.StatusDone {
		t.Fatalf("status = %s", done.Status)
	}

	kinds := []work.EventKind{}
	for _, e := range *events {
		kinds = append(kinds, e.Kind)
	}

	want := []work.EventKind{work.EventCreated, work.EventClaimed, work.EventDone}
	if len(kinds) != len(want) || kinds[0] != want[0] || kinds[1] != want[1] || kinds[2] != want[2] {
		t.Fatalf("events = %v, want %v", kinds, want)
	}
}

func TestDependenciesBlockClaims(t *testing.T) {
	t.Parallel()

	s, _ := newService(t)
	ctx := context.Background()
	specs := create(t, s, "specs")
	devWork := create(t, s, "dev", specs.ID)

	if _, err := s.Claim(ctx, "dev1@dev", devWork.ID); !errors.Is(err, work.ErrBlocked) {
		t.Fatalf("claiming a blocked ticket: error = %v, want ErrBlocked", err)
	}

	if _, err := s.Claim(ctx, "dev1@dev", ""); !errors.Is(err, work.ErrNoTicket) {
		t.Fatalf("the queue should be empty while blocked: error = %v", err)
	}

	if _, err := s.Claim(ctx, "archi@specs", specs.ID); err != nil {
		t.Fatal(err)
	}

	finished, err := s.SetStatus(ctx, "archi@specs", specs.ID, work.StatusDone)
	if err != nil {
		t.Fatal(err)
	}

	unblocked, err := s.Unblocked(ctx, finished)
	if err != nil {
		t.Fatal(err)
	}

	if len(unblocked) != 1 || unblocked[0].ID != devWork.ID {
		t.Fatalf("unblocked = %+v, want %s", unblocked, devWork.ID)
	}

	if _, err := s.Claim(ctx, "dev1@dev", ""); err != nil {
		t.Fatalf("once specs are done, dev can claim: %v", err)
	}
}

func TestCreateRules(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		by      string
		draft   work.Draft
		wantErr error
	}{
		{"agent for its own team", "dev1@dev", work.Draft{Title: "x", Team: "dev"}, nil},
		{"agent for a team it has a channel to", "dev1@dev", work.Draft{Title: "x", Team: "specs"}, nil},
		{"agent for a team without channel", "archi@specs", work.Draft{Title: "x", Team: "dev"}, work.ErrForbidden},
		{"human for any team", org.HumanAddress, work.Draft{Title: "x", Team: "qa"}, nil},
		{"director for any team, assigned", "god", work.Draft{Title: "x", Team: "qa", Assignee: "tester@qa"}, nil},
		{"missing title", org.HumanAddress, work.Draft{Team: "qa"}, work.ErrInvalid},
		{"unknown team", org.HumanAddress, work.Draft{Title: "x", Team: "ghosts"}, work.ErrInvalid},
		{"assignee from another team", org.HumanAddress, work.Draft{Title: "x", Team: "dev", Assignee: "archi@specs"}, work.ErrInvalid},
		{"unknown dependency", org.HumanAddress, work.Draft{Title: "x", Team: "dev", BlockedBy: []string{"T-99"}}, work.ErrInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s, _ := newService(t)
			if _, err := s.Create(context.Background(), tt.by, tt.draft); !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestClaimRules(t *testing.T) {
	t.Parallel()

	s, _ := newService(t)
	ctx := context.Background()
	tk := create(t, s, "dev")

	if _, err := s.Claim(ctx, "tester@qa", tk.ID); !errors.Is(err, work.ErrForbidden) {
		t.Fatalf("claiming another team's ticket: error = %v, want ErrForbidden", err)
	}

	if _, err := s.Claim(ctx, "dev1@dev", "T-42"); !errors.Is(err, work.ErrNotFound) {
		t.Fatalf("claiming a missing ticket: error = %v, want ErrNotFound", err)
	}

	if _, err := s.Claim(ctx, "dev1@dev", tk.ID); err != nil {
		t.Fatal(err)
	}

	released, err := s.SetStatus(ctx, "dev1@dev", tk.ID, work.StatusOpen)
	if err != nil {
		t.Fatal(err)
	}

	if released.Assignee != "" || released.Status != work.StatusOpen {
		t.Fatalf("a released ticket goes back to the queue: %+v", released)
	}
}

func TestParseID(t *testing.T) {
	t.Parallel()

	for _, in := range []string{"T-7", "t-7", "7", " T-7 "} {
		if n, err := work.ParseID(in); err != nil || n != 7 {
			t.Fatalf("ParseID(%q) = %d, %v", in, n, err)
		}
	}

	for _, in := range []string{"", "T-", "T-x", "T--1", "T-0"} {
		if _, err := work.ParseID(in); err == nil {
			t.Fatalf("ParseID(%q) should fail", in)
		}
	}
}

func TestOneTicketAtATime(t *testing.T) {
	t.Parallel()

	s, _ := newService(t)
	ctx := context.Background()
	first := create(t, s, "dev")
	second := create(t, s, "dev")

	if _, err := s.Claim(ctx, "dev1@dev", first.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := s.Claim(ctx, "dev1@dev", second.ID); !errors.Is(err, work.ErrBusy) {
		t.Fatalf("second claim: error = %v, want ErrBusy", err)
	}

	if _, err := s.Claim(ctx, "dev2@dev", second.ID); err != nil {
		t.Fatalf("a teammate can take the other ticket: %v", err)
	}

	if _, err := s.Create(ctx, org.HumanAddress, work.Draft{Title: "x", Team: "dev", Assignee: "dev1@dev"}); !errors.Is(err, work.ErrBusy) {
		t.Fatalf("assigning a busy agent: error = %v, want ErrBusy", err)
	}
}

// fakeWorkspace records the git hooks and can refuse them.
type fakeWorkspace struct {
	claimErr, doneErr, finishErr error
	claims                       []string
	finished                     int
}

func (f *fakeWorkspace) PrepareFeature(_ context.Context, ft work.Feature) (work.Feature, error) {
	ft.Slug, ft.Branch, ft.BaseBranch = "slug", "nobody/slug", "main"

	return ft, nil
}

func (f *fakeWorkspace) BeforeClaim(_ context.Context, _ work.Feature, agent string) error {
	f.claims = append(f.claims, agent)

	return f.claimErr
}

func (f *fakeWorkspace) ReviewFeature(context.Context, work.Feature) (work.Changes, error) {
	return work.Changes{Commits: []work.ReviewCommit{{Hash: "abc123", Subject: "feat: implement"}}, Files: []work.FileChange{}}, nil
}

func (f *fakeWorkspace) FinishFeature(context.Context, work.Feature) (string, error) {
	f.finished++

	return "merge123", f.finishErr
}

func (f *fakeWorkspace) BeforeDone(context.Context, work.Feature, string) ([]work.Deliverable, error) {
	return []work.Deliverable{{Kind: "commit", Reference: "abc123", Summary: "feat: implement", Author: "dev1@dev"}}, f.doneErr
}

func TestFeatureAndGitHooks(t *testing.T) {
	t.Parallel()

	s, _ := newService(t)
	ws := &fakeWorkspace{}
	s.Workspace = ws
	ctx := context.Background()

	var started []string

	s.OnFeatureStarted = func(f work.Feature) { started = append(started, f.ID) }

	if _, err := s.StartFeature(ctx, "dev1@dev", work.FeatureDraft{Title: "x"}); !errors.Is(err, work.ErrForbidden) {
		t.Fatalf("only the human starts features: error = %v", err)
	}

	f, err := s.StartFeature(ctx, org.HumanAddress, work.FeatureDraft{Title: "Add the order API"})
	if err != nil {
		t.Fatal(err)
	}

	if f.ID != "F-1" || !f.Active || f.Branch != "nobody/slug" || len(started) != 1 {
		t.Fatalf("feature = %+v, started = %v", f, started)
	}

	tk := create(t, s, "dev")
	if tk.Feature != "F-1" {
		t.Fatalf("a new ticket belongs to the active feature, got %q", tk.Feature)
	}

	ws.claimErr = errors.New("merge conflict")

	if _, err := s.Claim(ctx, "dev1@dev", tk.ID); !errors.Is(err, work.ErrConflict) {
		t.Fatalf("a failed sync must refuse the claim: error = %v", err)
	}

	ws.claimErr = nil
	if _, err := s.Claim(ctx, "dev1@dev", tk.ID); err != nil || len(ws.claims) != 2 {
		t.Fatalf("claim: %v, sync calls %v", err, ws.claims)
	}

	ws.doneErr = errors.New("uncommitted changes")

	if _, err := s.SetStatus(ctx, "dev1@dev", tk.ID, work.StatusDone); !errors.Is(err, work.ErrConflict) {
		t.Fatalf("a failed delivery must refuse the closing: error = %v", err)
	}

	ws.doneErr = nil

	if _, err := s.SetStatus(ctx, "dev1@dev", tk.ID, work.StatusDone); err != nil {
		t.Fatal(err)
	}

	details, err := s.Get(ctx, tk.ID)
	if err != nil {
		t.Fatal(err)
	}

	if len(details.Deliverables) != 1 || details.Deliverables[0].Reference != "abc123" {
		t.Fatalf("delivered commits should become deliverables: %+v", details.Deliverables)
	}
}

func TestDirectorChangesAnyTicketStatus(t *testing.T) {
	t.Parallel()

	s, _ := newService(t)
	ctx := context.Background()

	tk := create(t, s, "dev")
	if _, err := s.Claim(ctx, "dev1@dev", tk.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := s.SetStatus(ctx, "archi@specs", tk.ID, work.StatusOpen); !errors.Is(err, work.ErrForbidden) {
		t.Fatalf("another agent released dev1's ticket: err = %v", err)
	}

	if _, err := s.SetStatus(ctx, "god", tk.ID, work.StatusOpen); err != nil {
		t.Fatalf("the director should be able to send a ticket back to the queue: %v", err)
	}
}
