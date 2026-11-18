package schedule

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/qwackididuck/nobodyinc/internal/catalog"
	"github.com/qwackididuck/nobodyinc/internal/org"
)

// testOrg: ops (watcher with the allowlist profile, backup with the default
// one), empty (no members), and the director god.
func testOrg() *org.Org {
	allowlist := catalog.Definition{Permissions: catalog.Permissions{Profile: catalog.ProfileAllowlist}}

	return &org.Org{
		Teams: []org.Team{
			{ID: "ops", Agents: []org.Agent{{Name: "watcher", Definition: allowlist}, {Name: "backup"}}},
			{ID: "empty"},
		},
		God: &org.Agent{Name: "god", Definition: allowlist},
	}
}

func valid() Schedule {
	return Schedule{
		ID: "watch", Title: "Watch", Every: "10m", Target: "watcher@ops", Mode: ModeMessage,
		Enabled: true, Instructions: "Look at the dashboard.",
	}
}

func TestCheck(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		change func(*Schedule)
		want   string // part of the error; "" for valid
	}{
		{"valid interval", func(*Schedule) {}, ""},
		{"valid cron", func(s *Schedule) { s.Every, s.Cron = "", "0 9 * * 1-5" }, ""},
		{"valid descriptor", func(s *Schedule) { s.Every, s.Cron = "", "@daily" }, ""},
		{"bad id", func(s *Schedule) { s.ID = "Watch!" }, "lowercase"},
		{"no title", func(s *Schedule) { s.Title = " " }, "title"},
		{"no instructions", func(s *Schedule) { s.Instructions = "" }, "instructions"},
		{"bad mode", func(s *Schedule) { s.Mode = "mail" }, "mode"},
		{"no target", func(s *Schedule) { s.Target = "" }, "target"},
		{"bare team prefix", func(s *Schedule) { s.Target = "@" }, "target"},
		{"no recurrence", func(s *Schedule) { s.Every = "" }, "every"},
		{"both", func(s *Schedule) { s.Cron = "@daily" }, "not both"},
		{"too short", func(s *Schedule) { s.Every = "30s" }, "at least"},
		{"not a duration", func(s *Schedule) { s.Every = "daily" }, "duration"},
		{"bad cron", func(s *Schedule) { s.Every, s.Cron = "", "* * *" }, "cron"},
		{"cron time zone", func(s *Schedule) { s.Every, s.Cron = "", "CRON_TZ=UTC 0 9 * * *" }, "time zones"},
		{"cron @every", func(s *Schedule) { s.Every, s.Cron = "", "@every 5m" }, "every field"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := valid()
			tt.change(&s)

			err := s.Check()
			if tt.want == "" {
				if err != nil {
					t.Fatalf("Check() = %v, want nil", err)
				}

				return
			}

			if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Check() = %v, want ErrInvalid mentioning %q", err, tt.want)
			}
		})
	}
}

func TestCandidatesAndCheckTarget(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		target string
		mode   Mode
		want   []string
		why    string // CheckTarget's error when there are no candidates
	}{
		{"agent", "watcher@ops", ModeTicket, []string{"watcher@ops"}, ""},
		{"team", "@ops", ModeTicket, []string{"watcher@ops", "backup@ops"}, ""},
		{"director, message", "god", ModeMessage, []string{"god"}, ""},
		{"director, ticket", "god", ModeTicket, nil, "no tickets"},
		{"dismissed agent", "gone@ops", ModeMessage, nil, "no such agent"},
		{"deleted team", "@qa", ModeMessage, nil, "no such team"},
		{"empty team", "@empty", ModeMessage, nil, "no members"},
	}

	o := testOrg()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := valid()
			s.Target, s.Mode = tt.target, tt.mode

			var got []string
			for _, m := range s.Candidates(o) {
				got = append(got, m.Address)
			}

			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Fatalf("Candidates = %v, want %v", got, tt.want)
			}

			err := s.CheckTarget(o)
			if tt.why == "" {
				if err != nil {
					t.Fatalf("CheckTarget = %v", err)
				}

				return
			}

			if err == nil || !strings.Contains(err.Error(), tt.why) {
				t.Fatalf("CheckTarget = %v, want %q", err, tt.why)
			}
		})
	}
}

func TestWarning(t *testing.T) {
	t.Parallel()

	o := testOrg()

	s := valid()
	if w := s.Warning(o); w != "" {
		t.Fatalf("allowlist target: warning %q", w)
	}

	s.Target = "@ops" // backup uses the default profile
	if w := s.Warning(o); !strings.Contains(w, "backup@ops") {
		t.Fatalf("default profile: warning %q", w)
	}
}

func TestNextInterval(t *testing.T) {
	t.Parallel()

	armed := time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC)
	s := valid() // every 10m

	tests := []struct {
		after time.Time
		want  time.Time
	}{
		{armed, armed.Add(10 * time.Minute)},
		{armed.Add(-time.Hour), armed.Add(10 * time.Minute)},
		{armed.Add(10 * time.Minute), armed.Add(20 * time.Minute)},
		{armed.Add(25 * time.Minute), armed.Add(30 * time.Minute)}, // a missed occurrence is not caught up
	}

	for _, tt := range tests {
		if got := s.Next(armed, tt.after); !got.Equal(tt.want) {
			t.Errorf("Next(armed, %s) = %s, want %s", tt.after, got, tt.want)
		}
	}
}

// TestNextAroundDaylightSaving pins robfig/cron's behavior in a zone with
// daylight saving (Europe/Paris, 2026): a time that does not exist that day
// is skipped, a time that happens twice runs twice.
func TestNextAroundDaylightSaving(t *testing.T) {
	t.Parallel()

	paris, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Skip("no tzdata:", err)
	}

	occurrences := func(spec string, from time.Time, n int) []string {
		s := Schedule{Cron: spec}

		var got []string

		at := from
		for range n {
			at = s.nextIn(paris, at, at)
			got = append(got, at.Format(time.RFC3339))
		}

		return got
	}

	tests := []struct {
		name string
		spec string
		from time.Time
		want []string
	}{
		{
			name: "spring forward skips 02:30",
			spec: "30 2 * * *",
			from: time.Date(2026, 3, 28, 12, 0, 0, 0, paris),
			want: []string{"2026-03-30T02:30:00+02:00", "2026-03-31T02:30:00+02:00"},
		},
		{
			name: "fall back runs 02:30 twice",
			spec: "30 2 * * *",
			from: time.Date(2026, 10, 24, 12, 0, 0, 0, paris),
			want: []string{"2026-10-25T02:30:00+02:00", "2026-10-25T02:30:00+01:00", "2026-10-26T02:30:00+01:00"},
		},
		{
			name: "hourly jumps over the missing hour",
			spec: "0 * * * *",
			from: time.Date(2026, 3, 29, 0, 30, 0, 0, paris),
			want: []string{"2026-03-29T01:00:00+01:00", "2026-03-29T03:00:00+02:00"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := occurrences(tt.spec, tt.from, len(tt.want)); strings.Join(got, " ") != strings.Join(tt.want, " ") {
				t.Fatalf("occurrences = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLoadAndSave(t *testing.T) {
	t.Parallel()

	t.Run("no file", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()

		got, err := Load(dir)
		if err != nil || len(got) != 0 {
			t.Fatalf("Load = %v, %v", got, err)
		}
	})

	t.Run("written by hand, enabled by default", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()

		writeFile(t, dir, "schedules:\n  - id: report\n    title: Report\n    cron: \"@daily\"\n"+
			"    target: \"@ops\"\n    mode: ticket\n    instructions: Write it.\n")

		got, err := Load(dir)
		if err != nil {
			t.Fatal(err)
		}

		if len(got) != 1 || !got[0].Enabled || got[0].Target != "@ops" || got[0].Mode != ModeTicket {
			t.Fatalf("Load = %+v", got)
		}
	})

	t.Run("round trip", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()

		paused := valid()
		paused.ID, paused.Enabled = "paused", false
		want := []Schedule{valid(), paused}

		if err := Save(dir, want); err != nil {
			t.Fatal(err)
		}

		got, err := Load(dir)
		if err != nil {
			t.Fatal(err)
		}

		if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
			t.Fatalf("Load = %+v, want %+v", got, want)
		}
	})

	t.Run("refused", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()

		if err := Save(dir, []Schedule{valid(), valid()}); !errors.Is(err, ErrExists) {
			t.Fatalf("duplicate id: %v", err)
		}

		writeFile(t, dir, "schedules:\n  - id: x\n    unknown: 1\n")

		if _, err := Load(dir); err == nil {
			t.Fatal("unknown key accepted")
		}
	})
}

func writeFile(t *testing.T, dir, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Join(dir, org.ConfigDirName), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(Path(dir), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestProposalID(t *testing.T) {
	t.Parallel()

	if id := FormatProposalID(4); id != "SP-4" {
		t.Fatalf("FormatProposalID = %q", id)
	}

	for _, bad := range []string{"4", "SP-", "SP-0", "T-4"} {
		if _, err := ParseProposalID(bad); !errors.Is(err, ErrNotFound) {
			t.Errorf("ParseProposalID(%q) = %v", bad, err)
		}
	}
}

// due records what the scheduler fires.
type due struct {
	ch chan string
}

func (d due) fn(_ context.Context, s Schedule, at time.Time) {
	d.ch <- s.ID + "@" + at.Format("15:04")
}

func (d due) expect(t *testing.T, want string) {
	t.Helper()

	select {
	case got := <-d.ch:
		if got != want {
			t.Fatalf("fired %s, want %s", got, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("nothing fired, want %s", want)
	}
}

func (d due) expectNothing(t *testing.T) {
	t.Helper()

	select {
	case got := <-d.ch:
		t.Fatalf("fired %s, want nothing", got)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestScheduler(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC)
	clock := NewManualClock(start)
	d := due{ch: make(chan string, 10)}
	s := NewScheduler(clock, d.fn)

	ctx := t.Context()

	every10 := valid()
	every15 := valid()
	every15.ID, every15.Every = "slow", "15m"
	s.Set([]Schedule{every10, every15})

	go s.Run(ctx)

	step := func(d time.Duration) {
		clock.WaitForTimers(1)
		clock.Advance(d)
	}

	step(10 * time.Minute)
	d.expect(t, "watch@10:10")

	step(5 * time.Minute)
	d.expect(t, "slow@10:15")

	if next, ok := s.Next("watch"); !ok || !next.Equal(start.Add(20*time.Minute)) {
		t.Fatalf("Next(watch) = %s, %v", next, ok)
	}

	// Paused: dropped. Edited: armed again from now (10:15).
	every10.Enabled = false
	every15.Every = "20m"
	s.Set([]Schedule{every10, every15})

	if _, ok := s.Next("watch"); ok {
		t.Fatal("a paused schedule is still armed")
	}

	step(5 * time.Minute) // 10:20: watch would have fired
	d.expectNothing(t)

	step(15 * time.Minute) // 10:35
	d.expect(t, "slow@10:35")

	// The machine slept through several occurrences: one is due, not three.
	step(70 * time.Minute) // 11:45
	d.expect(t, "slow@10:55")
	d.expectNothing(t)

	if next, _ := s.Next("slow"); !next.Equal(start.Add(115 * time.Minute)) {
		t.Fatalf("after sleeping, Next(slow) = %s, want 11:55", next.Format("15:04"))
	}
}

func TestBook(t *testing.T) {
	t.Parallel()

	o := testOrg()
	o.ProjectDir = t.TempDir()

	var announced [][]Schedule

	book, err := OpenBook(o.ProjectDir, o, func(s []Schedule) { announced = append(announced, s) })
	if err != nil {
		t.Fatal(err)
	}

	s := valid()
	s.Enabled = false // a new schedule is enabled whatever it says

	if created, err := book.Create(s); err != nil || !created.Enabled {
		t.Fatalf("Create = %+v, %v", created, err)
	}

	if _, err := book.Create(s); !errors.Is(err, ErrExists) {
		t.Fatalf("Create twice: %v", err)
	}

	gone := valid()
	gone.ID, gone.Target = "gone", "nobody@ops"

	if _, err := book.Create(gone); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Create with a missing target: %v", err)
	}

	if _, err := book.SetEnabled("watch", false); err != nil {
		t.Fatal(err)
	}

	s.Every = "1h"
	if updated, err := book.Update("watch", s); err != nil || updated.Enabled || updated.Every != "1h" {
		t.Fatalf("Update = %+v, %v; want it still paused", updated, err)
	}

	// A schedule added by hand is kept by the next change.
	byHand := valid()
	byHand.ID = "by-hand"

	if err := Save(o.ProjectDir, append(book.List(), byHand)); err != nil {
		t.Fatal(err)
	}

	if err := book.Delete("watch"); err != nil {
		t.Fatal(err)
	}

	if got := book.List(); len(got) != 1 || got[0].ID != "by-hand" {
		t.Fatalf("after Delete, List = %+v", got)
	}

	if err := book.Delete("watch"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Delete twice: %v", err)
	}

	if len(announced) != 4 {
		t.Fatalf("OnChange called %d times, want 4 (create, pause, update, delete)", len(announced))
	}
}

func TestPreview(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC)

	next, err := Preview("10m", "", now, 3)
	if err != nil {
		t.Fatal(err)
	}

	want := []time.Time{now.Add(10 * time.Minute), now.Add(20 * time.Minute), now.Add(30 * time.Minute)}
	for i := range want {
		if !next[i].Equal(want[i]) {
			t.Fatalf("Preview = %v, want %v", next, want)
		}
	}

	if _, err := Preview("", "", now, 3); !errors.Is(err, ErrInvalid) {
		t.Fatalf("no recurrence: %v", err)
	}
}
