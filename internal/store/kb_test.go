package store

import (
	"testing"

	"github.com/qwackididuck/nobodyinc/internal/kb"
)

func TestKBMatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		query, want string
	}{
		{query: "", want: ""},
		{query: "   ", want: ""},
		{query: "token", want: `"token"*`},
		{query: "token  refresh", want: `"token" "refresh"*`},
		{query: `say "hi"`, want: `"say" """hi"""*`},
		{query: "a OR b", want: `"a" "OR" "b"*`},
		{query: "NEAR(x", want: `"NEAR(x"*`},
	}

	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			t.Parallel()

			if got := kbMatch(tt.query); got != tt.want {
				t.Errorf("kbMatch(%q) = %q, want %q", tt.query, got, tt.want)
			}
		})
	}
}

func TestSyncKB(t *testing.T) {
	t.Parallel()

	s := openTestStore(t)
	ctx := t.Context()

	steps := []struct {
		name             string
		docs             []kb.Doc
		indexed, removed int
		search           string
		found            int
	}{
		{
			name:    "first sync indexes everything",
			docs:    []kb.Doc{{Path: "a.md", Digest: "1", Title: "A", Body: "alpha"}, {Path: "b.md", Digest: "1", Title: "B", Body: "beta"}},
			indexed: 2, search: "alpha", found: 1,
		},
		{
			name:    "an unchanged digest is not indexed again",
			docs:    []kb.Doc{{Path: "a.md", Digest: "1", Title: "A", Body: "alpha"}, {Path: "b.md", Digest: "2", Title: "B", Body: "gamma"}},
			indexed: 1, search: "beta", found: 0,
		},
		{
			name:    "a page gone is removed",
			docs:    []kb.Doc{{Path: "b.md", Digest: "2", Title: "B", Body: "gamma"}},
			removed: 1, search: "alpha", found: 0,
		},
		{
			name:    "an empty sync clears the index",
			removed: 1, search: "gamma", found: 0,
		},
	}

	for _, step := range steps {
		indexed, removed, err := s.SyncKB(ctx, step.docs)
		if err != nil {
			t.Fatalf("%s: %v", step.name, err)
		}

		if indexed != step.indexed || removed != step.removed {
			t.Errorf("%s: indexed %d, removed %d; want %d, %d", step.name, indexed, removed, step.indexed, step.removed)
		}

		hits, err := s.SearchKB(ctx, step.search, 10)
		if err != nil {
			t.Fatalf("%s: %v", step.name, err)
		}

		if len(hits) != step.found {
			t.Errorf("%s: %q found %+v, want %d", step.name, step.search, hits, step.found)
		}
	}
}
