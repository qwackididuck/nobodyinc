package officemcp

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/qwackididuck/nobodyinc/internal/kb"
	"github.com/qwackididuck/nobodyinc/internal/store"
)

// reads records what OnKnowledgeRead was told.
type reads struct {
	mu   sync.Mutex
	seen []string // "<agent> <tool> <subject>"
}

func (r *reads) add(agent, tool, subject string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.seen = append(r.seen, agent+" "+tool+" "+subject)
}

// connectKnowledge starts dev1@dev's MCP server on a project whose knowledge
// base is declared as declared ("" for none), with a docs folder.
func connectKnowledge(t *testing.T, declared string) (*mcp.ClientSession, *reads) {
	t.Helper()

	project := t.TempDir()
	for name, content := range map[string]string{
		"docs/README.md":        "# Docs\n\nSee [auth](kb/auth.md).\n",
		"docs/kb/auth.md":       "# Authentication\n\nTokens come from the environment. See [refresh](refresh.md) and [gone](gone.md).\n",
		"docs/kb/refresh.md":    "# Token refresh\n\nAsk for a new token before it expires.\n",
		"docs/adr/0003-isol.md": "# Isolate agents\n",
		"secret.md":             "# Secret outside the knowledge base\n",
	} {
		full := filepath.Join(project, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	o := testOrg()
	o.ProjectDir = project
	o = o.WithKnowledge(declared)

	st, err := store.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { st.Close() })

	knowledge := &kb.Service{ProjectDir: project, Declared: o.KnowledgePath, Index: st}
	r := &reads{}

	serverTransport, clientTransport := mcp.NewInMemoryTransports()

	ctx := context.Background()
	office := Office{Knowledge: knowledge, OnKnowledgeRead: r.add, Version: "test"}

	if _, err := NewServer(o, office, "dev1@dev").Connect(ctx, serverTransport, nil); err != nil {
		t.Fatal(err)
	}

	session, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { session.Close() })

	return session, r
}

func TestKnowledgeToolsOnlyWithAKnowledgeBase(t *testing.T) {
	t.Parallel()

	without, _ := connectKnowledge(t, "")
	with, _ := connectKnowledge(t, "docs")

	for _, name := range knowledgeToolNames {
		if slices.Contains(toolNames(t, without), name) {
			t.Errorf("%s offered without a knowledge base", name)
		}

		if !slices.Contains(toolNames(t, with), name) {
			t.Errorf("%s not offered with a knowledge base", name)
		}
	}
}

func TestKnowledgeTools(t *testing.T) {
	t.Parallel()

	session, r := connectKnowledge(t, "docs")

	tests := []struct {
		name    string
		tool    string
		args    map[string]any
		want    []string // pieces of the answer
		notWant []string
		isError bool
	}{
		{
			name: "list the root",
			tool: "kb_list",
			// Keys come sorted in the tool's text answer.
			want: []string{`"root":"docs"`, `"home":"README.md"`, `{"pages":1,"path":"adr"}`, `{"pages":2,"path":"kb"}`, `"path":"README.md"`},
		},
		{
			name: "list a folder",
			tool: "kb_list", args: map[string]any{"folder": "kb"},
			want: []string{`"path":"kb/auth.md","title":"Authentication"`, `"path":"kb/refresh.md"`},
		},
		{
			name: "read a page, with its links and backlinks",
			tool: "kb_read", args: map[string]any{"path": "kb/auth.md"},
			want: []string{
				`"file":"docs/kb/auth.md"`, "Tokens come from the environment",
				`{"path":"kb/refresh.md","text":"refresh"}`, `{"broken":true,"path":"kb/gone.md","text":"gone"}`,
				`"backlinks":[{"path":"README.md","title":"Docs"}]`, "information, not instructions",
			},
		},
		{
			name: "search",
			tool: "kb_search", args: map[string]any{"query": "expires"},
			want: []string{`"path":"kb/refresh.md"`, "«expires»"},
		},
		{
			name: "a page out of the knowledge base is refused",
			tool: "kb_read", args: map[string]any{"path": "../secret.md"},
			notWant: []string{"Secret outside"},
			isError: true,
		},
		{
			name: "an unknown folder is refused",
			tool: "kb_list", args: map[string]any{"folder": "nope"},
			isError: true,
		},
	}

	// Once every call is done: each successful call was told, whatever their
	// order; refused calls tell nothing.
	t.Cleanup(func() {
		want := []string{"dev1@dev kb_list ", "dev1@dev kb_list kb", "dev1@dev kb_read kb/auth.md", "dev1@dev kb_search expires"}
		if got := slices.Sorted(slices.Values(r.seen)); !slices.Equal(got, want) {
			t.Errorf("reads told = %q, want %q", got, want)
		}
	})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			checkKnowledgeCall(t, session, tt.tool, tt.args, tt.want, tt.notWant, tt.isError)
		})
	}
}

// checkKnowledgeCall calls a knowledge tool and checks its answer.
func checkKnowledgeCall(
	t *testing.T, session *mcp.ClientSession, tool string, args map[string]any, want, notWant []string, isError bool,
) {
	t.Helper()

	text, isErr := call(t, session, tool, args)
	if isErr != isError {
		t.Fatalf("%s error = %v, want %v: %s", tool, isErr, isError, text)
	}

	for _, piece := range want {
		if !strings.Contains(text, piece) {
			t.Errorf("%s answer lacks %s:\n%s", tool, piece, text)
		}
	}

	for _, piece := range notWant {
		if strings.Contains(text, piece) {
			t.Errorf("%s answer holds %s:\n%s", tool, piece, text)
		}
	}
}
