package budget

import (
	"context"
	"math"
	"testing"
	"time"
)

func TestDelta(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		previous sessionTotal
		known    bool
		session  string
		total    float64
		want     float64
	}{
		{name: "first report", session: "s1", total: 0.3, want: 0.3},
		{name: "the total grows", previous: sessionTotal{"s1", 0.3}, known: true, session: "s1", total: 0.5, want: 0.2},
		{name: "same total", previous: sessionTotal{"s1", 0.3}, known: true, session: "s1", total: 0.3, want: 0},
		{name: "a late, lower report", previous: sessionTotal{"s1", 0.5}, known: true, session: "s1", total: 0.4, want: 0},
		{name: "another session starts from 0", previous: sessionTotal{"s1", 0.5}, known: true, session: "s2", total: 0.1, want: 0.1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := Delta(tt.previous, tt.known, tt.session, tt.total); math.Abs(got-tt.want) > 1e-9 {
				t.Fatalf("Delta = %v, want %v", got, tt.want)
			}
		})
	}
}

type memSpend struct{ records []Record }

func (m *memSpend) InsertSpend(_ context.Context, r Record) (Record, error) {
	r.ID = int64(len(m.records) + 1)
	m.records = append(m.records, r)

	return r, nil
}

func newMeter(attribution Attribution) (*Meter, *memSpend) {
	st := &memSpend{}
	now := time.Date(2026, 11, 30, 23, 30, 0, 0, time.UTC)

	return &Meter{
		Store:     st,
		Attribute: func(context.Context, string) (Attribution, error) { return attribution, nil },
		Now:       func() time.Time { return now },
	}, st
}

// A session's reports become deltas; a resumed session (same ID, total back
// to 0) counts from 0 once the supervisor says the process started again.
func TestMeterReport(t *testing.T) {
	t.Parallel()

	m, st := newMeter(Attribution{Agent: "dev1@dev", Team: "dev", Ticket: "T-3", Feature: "F-1"})
	ctx := context.Background()

	reports := []struct {
		session string
		total   float64
		forget  bool
	}{
		{session: "s1", total: 0},     // the session is ready: nothing spent
		{session: "s1", total: 0.10},  // +0.10
		{session: "s1", total: 0.25},  // +0.15
		{session: "s1", total: 0.20},  // late report: ignored
		{session: "s1", forget: true}, // the agent restarts with --resume
		{session: "s1", total: 0},     // the resumed session starts from 0
		{session: "s1", total: 0.05},  // +0.05
	}

	for _, r := range reports {
		if r.forget {
			m.Forget("dev1@dev")

			continue
		}

		if err := m.Report(ctx, "dev1@dev", r.session, r.total); err != nil {
			t.Fatal(err)
		}
	}

	want := []float64{0.10, 0.15, 0.05}
	if len(st.records) != len(want) {
		t.Fatalf("%d records, want %d: %+v", len(st.records), len(want), st.records)
	}

	for i, r := range st.records {
		if math.Abs(r.AmountUSD-want[i]) > 1e-9 || r.Source != SourceStatusLine || r.Session != "s1" || r.Ticket != "T-3" || r.Feature != "F-1" {
			t.Fatalf("record %d = %+v, want %.2f for T-3 of F-1", i, r, want[i])
		}
	}
}

// Months are the machine's: 23:30 UTC on November 30 is already December in
// Paris.
func TestMonthIsLocal(t *testing.T) { //nolint:paralleltest // changes time.Local
	paris, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Skip("no tzdata:", err)
	}

	saved := time.Local
	time.Local = paris

	t.Cleanup(func() { time.Local = saved })

	if got := Month(time.Date(2026, 11, 30, 23, 30, 0, 0, time.UTC)); got != "2026-12" {
		t.Fatalf("Month = %s, want 2026-12", got)
	}
}

func TestMeterFork(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		asker string
		want  Attribution
	}{
		{name: "an agent asked: its ticket", asker: "dev1@dev", want: Attribution{Agent: "dev1@dev", Team: "dev", Ticket: "T-3"}},
		{name: "the human asked: the staff only", asker: "", want: Attribution{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m, st := newMeter(Attribution{Agent: "dev1@dev", Team: "dev", Ticket: "T-3"})

			if err := m.Fork(context.Background(), tt.asker, "s-fork", 0.012); err != nil {
				t.Fatal(err)
			}

			if len(st.records) != 1 || st.records[0].Attribution != tt.want || st.records[0].Source != SourceFork {
				t.Fatalf("records = %+v, want one fork record for %+v", st.records, tt.want)
			}
		})
	}
}
