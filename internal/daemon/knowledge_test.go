package daemon

import (
	"encoding/json"
	"testing"

	"github.com/qwackididuck/nobodyinc/internal/hook"
)

func TestKnowledgeFile(t *testing.T) {
	t.Parallel()

	const (
		project  = "/work/shop"
		worktree = "/home/me/.nobody/projects/shop-1/worktrees/F-1/dev"
	)

	tests := []struct {
		name     string
		cwd      string
		input    map[string]string
		want     string
		wantFile bool
	}{
		{name: "a page of the main checkout", cwd: project, input: map[string]string{"file_path": project + "/docs/kb/auth.md"}, want: "kb/auth.md", wantFile: true},
		{name: "a page of the agent's worktree", cwd: worktree, input: map[string]string{"file_path": worktree + "/docs/adr/0003.md"}, want: "adr/0003.md", wantFile: true},
		{name: "a relative path, from the working directory", cwd: worktree, input: map[string]string{"file_path": "docs/README.md"}, want: "README.md", wantFile: true},
		{name: "an image of the knowledge base", cwd: project, input: map[string]string{"file_path": project + "/docs/img/flow.png"}, want: "img/flow.png", wantFile: true},
		{name: "a notebook", cwd: project, input: map[string]string{"notebook_path": project + "/docs/analysis.ipynb"}, want: "analysis.ipynb", wantFile: true},
		{name: "code, not documentation", cwd: project, input: map[string]string{"file_path": project + "/internal/kb/kb.go"}},
		{name: "a folder named like it elsewhere", cwd: project, input: map[string]string{"file_path": project + "/web/docs/x.md"}},
		{name: "leaving the folder", cwd: project, input: map[string]string{"file_path": project + "/docs/../secret.md"}},
		{name: "the folder itself", cwd: project, input: map[string]string{"file_path": project + "/docs"}},
		{name: "a hidden file", cwd: project, input: map[string]string{"file_path": project + "/docs/.obsidian/app.json"}},
		{name: "another worktree's file", cwd: worktree, input: map[string]string{"file_path": "/elsewhere/docs/a.md"}},
		{name: "no file named", cwd: project, input: map[string]string{"pattern": "TODO"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			raw, err := json.Marshal(tt.input)
			if err != nil {
				t.Fatal(err)
			}

			ev := hook.Event{Name: hook.EventPreToolUse, Cwd: tt.cwd, ToolName: "Edit", ToolInput: raw}

			got, ok := knowledgeFile("docs", ev, worktree, project)
			if ok != tt.wantFile || got != tt.want {
				t.Errorf("knowledgeFile = %q, %v; want %q, %v", got, ok, tt.want, tt.wantFile)
			}
		})
	}
}
