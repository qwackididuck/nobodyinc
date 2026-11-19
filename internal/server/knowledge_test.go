package server

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qwackididuck/nobodyinc/internal/kb"
	"github.com/qwackididuck/nobodyinc/internal/org"
	"github.com/qwackididuck/nobodyinc/internal/store"
)

// newKnowledgeEnv serves a project whose knowledge base is declared as
// declared ("" for none), with a docs folder.
func newKnowledgeEnv(t *testing.T, declared string) testEnv {
	t.Helper()

	project := t.TempDir()
	for name, content := range map[string]string{
		"docs/README.md":       "# Docs\n\nRead [auth](kb/auth.md).\n",
		"docs/kb/auth.md":      "---\nstatus: current\n---\n# Authentication\n\nTokens: see [refresh](refresh.md#when), [gone](gone.md).\n\n![flow](img/flow.svg)\n",
		"docs/kb/refresh.md":   "# Token refresh\n\n## When\n\nBefore it expires.\n",
		"docs/kb/img/flow.svg": `<svg xmlns="http://www.w3.org/2000/svg"/>`,
		"docs/kb/notes.txt":    "not an image",
		"docs/orphan.md":       "# Orphan\n",
		"secret.md":            "# Secret\n",
	} {
		full := filepath.Join(project, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	st, err := store.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { st.Close() })

	o := (&org.Org{ProjectDir: project}).WithKnowledge(declared)
	knowledge := &kb.Service{ProjectDir: project, Declared: o.KnowledgePath, Index: st}

	return testEnv{handler: New(Deps{Logger: slog.New(slog.DiscardHandler), Org: o, Store: st, Knowledge: knowledge}), store: st}
}

func TestKnowledgeEndpoints(t *testing.T) {
	t.Parallel()

	env := newKnowledgeEnv(t, "docs")

	tests := []struct {
		name, method, path, body string
		code                     int
		want, notWant            []string
	}{
		{
			name: "overview", method: http.MethodGet, path: "/api/kb", code: http.StatusOK,
			want: []string{
				`"path":"docs"`, `"suggested":"docs"`, `"exists":true`, `"pages":4`, `"home":"README.md"`,
				`{"path":"kb","pages":2}`, `"broken":[{"page":"kb/auth.md","target":"gone.md","text":"gone"}]`,
				`"orphans":[{"path":"orphan.md","title":"Orphan"`,
			},
		},
		{
			name: "tree", method: http.MethodGet, path: "/api/kb/tree", code: http.StatusOK,
			want: []string{`"home":"README.md"`, `{"name":"kb","path":"kb","dir":true,"pages":2,`, `"title":"Token refresh"`},
		},
		{
			name: "page", method: http.MethodGet, path: "/api/kb/pages/kb/auth.md", code: http.StatusOK,
			want: []string{
				`"title":"Authentication"`, `"header":[{"key":"status","value":"current"}]`,
				`href=\"kb:kb/refresh.md#when\"`, `src=\"kb-asset:kb/img/flow.svg\"`,
				`"kind":"page","path":"kb/refresh.md","anchor":"when","title":"Token refresh","broken":false`,
				`"path":"kb/gone.md","broken":true`, `"backlinks":[{"path":"README.md","title":"Docs"}]`,
			},
		},
		{name: "missing page", method: http.MethodGet, path: "/api/kb/pages/kb/none.md", code: http.StatusNotFound},
		{
			name: "a page outside the folder", method: http.MethodGet, path: "/api/kb/pages/..%2Fsecret.md", code: http.StatusNotFound,
			notWant: []string{"Secret"},
		},
		{
			name: "search", method: http.MethodGet, path: "/api/kb/search?q=expires", code: http.StatusOK,
			want: []string{`"query":"expires"`, `"path":"kb/refresh.md"`, `{"text":"expires","match":true}`},
		},
		{name: "search with a bad limit", method: http.MethodGet, path: "/api/kb/search?q=a&limit=0", code: http.StatusBadRequest},
		{name: "image", method: http.MethodGet, path: "/api/kb/assets/kb/img/flow.svg", code: http.StatusOK, want: []string{"<svg"}},
		{name: "not an image", method: http.MethodGet, path: "/api/kb/assets/kb/notes.txt", code: http.StatusUnsupportedMediaType},
		{name: "missing image", method: http.MethodGet, path: "/api/kb/assets/kb/img/none.png", code: http.StatusNotFound},
		{name: "change without staff", method: http.MethodPut, path: "/api/kb", body: `{"path":"doc"}`, code: http.StatusNotImplemented},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := env.do(t, tt.method, tt.path, tt.body)
			if rec.Code != tt.code {
				t.Fatalf("%s %s = %d, want %d: %s", tt.method, tt.path, rec.Code, tt.code, rec.Body)
			}

			body := rec.Body.String()
			for _, piece := range tt.want {
				if !strings.Contains(body, piece) {
					t.Errorf("answer lacks %s:\n%s", piece, body)
				}
			}

			for _, piece := range tt.notWant {
				if strings.Contains(body, piece) {
					t.Errorf("answer holds %s:\n%s", piece, body)
				}
			}
		})
	}
}

func TestKnowledgeAssetIsSandboxed(t *testing.T) {
	t.Parallel()

	rec := newKnowledgeEnv(t, "docs").do(t, http.MethodGet, "/api/kb/assets/kb/img/flow.svg", "")

	if got := rec.Header().Get("Content-Type"); got != "image/svg+xml" {
		t.Errorf("Content-Type = %q", got)
	}

	if got := rec.Header().Get("Content-Security-Policy"); !strings.Contains(got, "sandbox") {
		t.Errorf("Content-Security-Policy = %q, want a sandbox", got)
	}
}

func TestKnowledgeEndpointsWithoutAKnowledgeBase(t *testing.T) {
	t.Parallel()

	env := newKnowledgeEnv(t, "")

	if rec := env.do(t, http.MethodGet, "/api/kb", ""); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"path":"","suggested":"docs"`) {
		t.Errorf("GET /api/kb = %d %s", rec.Code, rec.Body)
	}

	for _, p := range []string{"/api/kb/tree", "/api/kb/pages/README.md", "/api/kb/search?q=docs", "/api/kb/assets/kb/img/flow.svg"} {
		if rec := env.do(t, http.MethodGet, p, ""); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404: %s", p, rec.Code, rec.Body)
		}
	}
}
