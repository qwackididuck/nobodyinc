// The service is tested with the real index (internal/store, SQLite FTS5),
// which imports kb: hence an external test package.
package kb_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/qwackididuck/nobodyinc/internal/kb"
	"github.com/qwackididuck/nobodyinc/internal/store"
)

// testService is a Service on a throwaway project with a "docs" folder,
// recording what it tells OnChange.
type testService struct {
	*kb.Service

	project  string
	mu       sync.Mutex
	declared string
	updates  []kb.Update
}

func newTestService(t *testing.T, files map[string]string) *testService {
	t.Helper()

	project := t.TempDir()
	writeDocs(t, filepath.Join(project, "docs"), files)

	index, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "nobody.db"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { index.Close() })

	ts := &testService{project: project, declared: "docs"}
	ts.Service = &kb.Service{
		ProjectDir: project,
		Declared: func() string {
			ts.mu.Lock()
			defer ts.mu.Unlock()

			return ts.declared
		},
		Index: index,
		OnChange: func(u kb.Update) {
			ts.mu.Lock()
			defer ts.mu.Unlock()

			ts.updates = append(ts.updates, u)
		},
		MaxAge: time.Hour, // tests refresh by hand
	}

	return ts
}

func (ts *testService) setDeclared(p string) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	ts.declared = p
}

func (ts *testService) takeUpdates() []kb.Update {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	u := ts.updates
	ts.updates = nil

	return u
}

func TestServiceSearch(t *testing.T) {
	t.Parallel()

	//nolint:gosec // G101: "auth" and "token" are page names here, not credentials
	ts := newTestService(t, map[string]string{
		"kb/auth.md":          "# Authentication\n\nTokens are passed through environment variables.\n",
		"kb/token-refresh.md": "# Token refresh\n\nWhen a token expires, ask for a refresh.\n",
		"adr/decision.md":     "# Décision\n\nThe décision about agents.\n",
	})

	tests := []struct {
		query string
		want  []string // paths, best first; nil = no result
	}{
		{query: "refresh", want: []string{"kb/token-refresh.md"}},
		{query: "token", want: []string{"kb/token-refresh.md", "kb/auth.md"}},
		{query: "tok", want: []string{"kb/token-refresh.md", "kb/auth.md"}},
		{query: "token refresh", want: []string{"kb/token-refresh.md"}},
		{query: "decision", want: []string{"adr/decision.md"}},
		{query: `"unbalanced OR (`, want: nil},
		{query: "   ", want: nil},
		{query: "nothing-like-this", want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			t.Parallel()

			hits, err := ts.Search(t.Context(), tt.query, 10)
			if err != nil {
				t.Fatal(err)
			}

			var got []string
			for _, h := range hits {
				got = append(got, h.Path)
			}

			if !slices.Equal(got, tt.want) {
				t.Errorf("Search(%q) = %q, want %q", tt.query, got, tt.want)
			}
		})
	}

	t.Run("snippets mark the matches", func(t *testing.T) {
		t.Parallel()

		hits, err := ts.Search(t.Context(), "expires", 10)
		if err != nil || len(hits) != 1 {
			t.Fatalf("Search = %+v, %v", hits, err)
		}

		if got := hits[0].Plain(); got != "Token refresh When a token «expires», ask for a refresh." {
			t.Errorf("snippet = %q", got)
		}
	})
}

func TestServiceUpdates(t *testing.T) {
	t.Parallel()

	ts := newTestService(t, map[string]string{"a.md": "# A\n"})
	ctx := t.Context()

	steps := []struct {
		name string
		do   func(t *testing.T)
		want []kb.Update
	}{
		{
			name: "the first scan is a reset",
			want: []kb.Update{{Pages: 1, Added: 1, Reset: true}},
		},
		{
			name: "a scan with no change tells nothing",
		},
		{
			name: "new pages are told",
			do: func(t *testing.T) {
				t.Helper()
				writeDocs(t, filepath.Join(ts.project, "docs"), map[string]string{"b.md": "# B, about tokens\n", "c.md": "# C\n"})
			},
			want: []kb.Update{{Pages: 3, Added: 2}},
		},
		{
			name: "a removed page is told",
			do: func(t *testing.T) {
				t.Helper()

				if err := os.Remove(filepath.Join(ts.project, "docs", "b.md")); err != nil {
					t.Fatal(err)
				}
			},
			want: []kb.Update{{Pages: 2, Removed: 1}},
		},
	}

	for _, step := range steps {
		if step.do != nil {
			step.do(t)
		}

		if _, err := ts.Refresh(ctx); err != nil {
			t.Fatalf("%s: %v", step.name, err)
		}

		if got := ts.takeUpdates(); !slices.Equal(got, step.want) {
			t.Errorf("%s: told %+v, want %+v", step.name, got, step.want)
		}
	}

	if hits, _ := ts.Search(ctx, "tokens", 5); len(hits) != 0 {
		t.Errorf("a removed page is still found: %+v", hits)
	}

	ts.setDeclared("")

	if _, err := ts.Base(ctx); !errors.Is(err, kb.ErrNone) {
		t.Fatalf("Base without a knowledge base = %v, want ErrNone", err)
	}

	if got := ts.takeUpdates(); !slices.Equal(got, []kb.Update{{Reset: true}}) {
		t.Errorf("removing the knowledge base told %+v, want a reset", got)
	}

	if _, err := ts.Search(ctx, "C", 5); !errors.Is(err, kb.ErrNone) {
		t.Errorf("Search without a knowledge base = %v, want ErrNone", err)
	}

	ts.setDeclared("docs")

	if hits, _ := ts.Search(ctx, "C", 5); len(hits) != 1 {
		t.Errorf("setting the knowledge base up again does not index it: %+v", hits)
	}
}

func TestServiceRefusesBadPaths(t *testing.T) {
	t.Parallel()

	ts := newTestService(t, map[string]string{"a.md": "# A\n"})

	if err := os.Symlink(t.TempDir(), filepath.Join(ts.project, "outside")); err != nil {
		t.Fatal(err)
	}

	ts.setDeclared("outside")

	if _, err := ts.Base(t.Context()); !errors.Is(err, kb.ErrBadPath) {
		t.Fatalf("Base on a link leaving the project = %v, want ErrBadPath", err)
	}
}

// writeDocs writes files (path relative to root → content) under root.
func writeDocs(t *testing.T, root string, files map[string]string) {
	t.Helper()

	for name, content := range files {
		full := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}
