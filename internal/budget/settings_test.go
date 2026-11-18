package budget

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qwackididuck/nobodyinc/internal/org"
)

func testOrg(t *testing.T) *org.Org {
	t.Helper()

	return &org.Org{
		ProjectDir: t.TempDir(),
		Teams:      []org.Team{{ID: "dev", Agents: []org.Agent{{Name: "dev1"}}}},
		God:        &org.Agent{Name: "god"},
	}
}

func TestSettingsCheck(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		s       Settings
		wantErr string
	}{
		{name: "defaults", s: Defaults()},
		{name: "caps", s: Settings{Enabled: true, Threshold: 80, Staff: &Cap{LimitUSD: 100, Monthly: true}, Teams: map[string]Cap{"dev": {LimitUSD: 30, Threshold: 90}}}},
		{name: "threshold 0", s: Settings{Threshold: 0}, wantErr: "between 1 and 100"},
		{name: "threshold above 100", s: Settings{Threshold: 120}, wantErr: "between 1 and 100"},
		{name: "a zero cap", s: Settings{Threshold: 80, Agents: map[string]Cap{"dev1@dev": {}}}, wantErr: "agent dev1@dev: the limit"},
		{name: "a cap's threshold", s: Settings{Threshold: 80, Staff: &Cap{LimitUSD: 1, Threshold: 101}}, wantErr: "the staff's threshold"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.s.Check()
			if tt.wantErr == "" && err != nil || tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("Check() = %v, want an error containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestSettingsCheckOrg(t *testing.T) {
	t.Parallel()

	o := testOrg(t)

	tests := []struct {
		name    string
		s       Settings
		wantErr string
	}{
		{name: "existing team and agent", s: Settings{Teams: map[string]Cap{"dev": {LimitUSD: 1}}, Agents: map[string]Cap{"dev1@dev": {LimitUSD: 1}}}},
		{name: "unknown team", s: Settings{Teams: map[string]Cap{"ops": {LimitUSD: 1}}}, wantErr: `no team "ops"`},
		{name: "unknown agent", s: Settings{Agents: map[string]Cap{"ghost@dev": {LimitUSD: 1}}}, wantErr: `no agent "ghost@dev"`},
		{name: "the director", s: Settings{Agents: map[string]Cap{"god": {LimitUSD: 1}}}, wantErr: "never held"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.s.CheckOrg(o)
			if tt.wantErr == "" && err != nil || tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("CheckOrg() = %v, want an error containing %q", err, tt.wantErr)
			}
		})
	}
}

type fixedOrg struct{ o *org.Org }

func (f fixedOrg) Current() *org.Org { return f.o }

// The book writes the file, keeps what was added by hand, and drops caps
// nobody can use any more.
func TestBook(t *testing.T) {
	t.Parallel()

	o := testOrg(t)

	var announced []Settings

	b, err := OpenBook(o.ProjectDir, fixedOrg{o}, func(s Settings) { announced = append(announced, s) })
	if err != nil {
		t.Fatal(err)
	}

	if got := b.Get(); !got.Enabled || got.Threshold != DefaultThreshold {
		t.Fatalf("no file: %+v, want the defaults", got)
	}

	if _, err := b.Replace(Settings{Enabled: true, Threshold: 75, Agents: map[string]Cap{"ghost@dev": {LimitUSD: 1}}}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a cap on an unknown agent: %v, want ErrInvalid", err)
	}

	if _, err := b.Replace(Settings{Enabled: true, Threshold: 75, Teams: map[string]Cap{"dev": {LimitUSD: 30, Monthly: true}}}); err != nil {
		t.Fatal(err)
	}

	// By hand: a cap for an agent since dismissed.
	path := Path(o.ProjectDir)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(data), "limit_usd: 30") || !strings.Contains(string(data), "monthly: true") {
		t.Fatalf("budgets.yaml:\n%s", data)
	}

	if err := os.WriteFile(path, append(data, []byte("agents:\n  gone@dev:\n    limit_usd: 5\n")...), 0o600); err != nil { //nolint:gosec // a test file
		t.Fatal(err)
	}

	if _, err := Load(o.ProjectDir); err != nil {
		t.Fatalf("a cap of a dismissed agent must still load: %v", err)
	}

	s := b.Get()
	s.Enabled = false

	if _, err := b.Replace(s); err != nil {
		t.Fatal(err)
	}

	got, err := Load(o.ProjectDir)
	if err != nil {
		t.Fatal(err)
	}

	if got.Enabled || got.Threshold != 75 || len(got.Agents) != 0 || got.Teams["dev"].LimitUSD != 30 {
		t.Fatalf("after the second write: %+v", got)
	}

	if len(announced) != 2 {
		t.Fatalf("%d announcements, want 2", len(announced))
	}
}

func TestLoadRejectsAnInvalidFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Dir(Path(dir)), 0o750); err != nil {
		t.Fatal(err)
	}

	for _, content := range []string{"threshold: 0\n", "nonsense: true\n", "staff:\n  limit_usd: -1\n"} {
		if err := os.WriteFile(Path(dir), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}

		if _, err := Load(dir); err == nil {
			t.Fatalf("Load accepted %q", content)
		}
	}
}

func TestGauges(t *testing.T) {
	t.Parallel()

	s := Settings{
		Enabled: true, Threshold: 80,
		Staff:  &Cap{LimitUSD: 100, Monthly: true},
		Teams:  map[string]Cap{"dev": {LimitUSD: 50, Threshold: 90}},
		Agents: map[string]Cap{"dev1@dev": {LimitUSD: 10, Monthly: true}},
	}
	month := Totals{StaffUSD: 20, Teams: map[string]float64{"dev": 15}, Agents: map[string]float64{"dev1@dev": 8}}
	allTime := Totals{StaffUSD: 70, Teams: map[string]float64{"dev": 46}, Agents: map[string]float64{"dev1@dev": 30}}
	ticket := Budgeted{Scope: Scope{Kind: KindTicket, ID: "T-1"}, LimitUSD: 2, SpentUSD: 1}

	gauges := Gauges(s, month, allTime, []Budgeted{ticket})

	want := []struct {
		kind      Kind
		spent     float64
		threshold int
		reached   bool
	}{
		{KindStaff, 20, 80, false}, // monthly: this month
		{KindTeam, 46, 90, true},   // cumulative: all time, its own threshold
		{KindAgent, 8, 80, true},   // 8 of 10 = 80 %
		{KindTicket, 1, 80, false},
	}

	if len(gauges) != len(want) {
		t.Fatalf("%d gauges, want %d: %+v", len(gauges), len(want), gauges)
	}

	for i, w := range want {
		g := gauges[i]
		if g.Scope.Kind != w.kind || g.SpentUSD != w.spent || g.Threshold != w.threshold || g.Reached() != w.reached {
			t.Fatalf("gauge %d = %+v (reached %v), want %+v", i, g, g.Reached(), w)
		}
	}

	s.Enabled = false
	if len(Gauges(s, month, allTime, []Budgeted{ticket})) != 0 {
		t.Fatal("no gauge while budgets are turned off")
	}
}
