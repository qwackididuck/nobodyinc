package kb

import (
	"slices"
	"strings"
	"testing"

	"github.com/qwackididuck/nobodyinc/internal/frontmatter"
)

func TestParsePage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		path, data string
		title      string
		header     []frontmatter.Field
		targets    []string
		text       []string // pieces the plain text holds
		notText    []string // pieces it does not
	}{
		{
			name:  "title from the first level-one heading",
			path:  "kb/auth.md",
			data:  "Intro line\n\n## Not this\n\n# Authentication\n\n# Not this either\n",
			title: "Authentication",
		},
		{
			name:  "title from the file name without a heading",
			path:  "kb/token-refresh.md",
			data:  "Just text.\n",
			title: "token-refresh",
		},
		{
			name:  "inline markup in the title",
			path:  "a.md",
			data:  "# The `kb_read` *tool*\n",
			title: "The kb_read tool",
		},
		{
			name:    "YAML header, kept in order, out of the text",
			path:    "a.md",
			data:    "---\nstatus: current\nowner: backend\ndescription: What it is\n---\n# A\n\nBody\n",
			title:   "A",
			header:  []frontmatter.Field{{Key: "description", Value: "What it is"}, {Key: "status", Value: "current"}, {Key: "owner", Value: "backend"}},
			text:    []string{"Body"},
			notText: []string{"status", "owner"},
		},
		{
			name:    "links, images and autolinks in order",
			path:    "a.md",
			data:    "See [ADR](../adr/0003.md#context), ![flow](img/flow.png) and <https://example.com>.\n",
			targets: []string{"../adr/0003.md#context", "img/flow.png", "https://example.com"},
			text:    []string{"See ADR", "https://example.com"},
		},
		{
			name:    "code blocks and tables are text, raw HTML is not",
			path:    "a.md",
			data:    "```go\nfunc main() {}\n```\n\n| Token | Where |\n|---|---|\n| GITLAB_TOKEN | env |\n\n<script>alert(1)</script>\n",
			text:    []string{"func main() {}", "GITLAB_TOKEN"},
			notText: []string{"alert"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := parsePage(tt.path, []byte(tt.data))

			if tt.title != "" && got.title != tt.title {
				t.Errorf("title = %q, want %q", got.title, tt.title)
			}

			if tt.header != nil && !slices.Equal(got.header, tt.header) {
				t.Errorf("header = %+v, want %+v", got.header, tt.header)
			}

			var targets []string
			for _, l := range got.links {
				targets = append(targets, l.Target)
			}

			if tt.targets != nil && !slices.Equal(targets, tt.targets) {
				t.Errorf("links = %q, want %q", targets, tt.targets)
			}

			for _, piece := range tt.text {
				if !strings.Contains(got.text, piece) {
					t.Errorf("text %q lacks %q", got.text, piece)
				}
			}

			for _, piece := range tt.notText {
				if strings.Contains(got.text, piece) {
					t.Errorf("text %q holds %q", got.text, piece)
				}
			}
		})
	}
}

func TestResolveLink(t *testing.T) {
	t.Parallel()

	files := map[string]bool{"kb/auth.md": true, "adr/0003.md": true, "kb/img/flow.png": true, "index.md": true}

	tests := []struct {
		name, from, target string
		want               Link
	}{
		{name: "relative page", from: "kb/auth.md", target: "../adr/0003.md",
			want: Link{Kind: LinkPage, Path: "adr/0003.md"}},
		{name: "page and anchor", from: "kb/auth.md", target: "../adr/0003.md#context",
			want: Link{Kind: LinkPage, Path: "adr/0003.md", Anchor: "context"}},
		{name: "from the root", from: "kb/auth.md", target: "/index.md",
			want: Link{Kind: LinkPage, Path: "index.md"}},
		{name: "escaped spaces", from: "index.md", target: "kb/au%74h.md",
			want: Link{Kind: LinkPage, Path: "kb/auth.md"}},
		{name: "upper-case extension", from: "index.md", target: "kb/auth.MD",
			want: Link{Kind: LinkPage, Path: "kb/auth.MD", Broken: true}},
		{name: "missing page", from: "kb/auth.md", target: "../adr/0099.md",
			want: Link{Kind: LinkPage, Path: "adr/0099.md", Broken: true}},
		{name: "anchor of the same page", from: "kb/auth.md", target: "#flow",
			want: Link{Kind: LinkAnchor, Path: "kb/auth.md", Anchor: "flow"}},
		{name: "image", from: "kb/auth.md", target: "img/flow.png",
			want: Link{Kind: LinkAsset, Path: "kb/img/flow.png"}},
		{name: "missing image", from: "kb/auth.md", target: "img/none.png",
			want: Link{Kind: LinkAsset, Path: "kb/img/none.png", Broken: true}},
		{name: "leaving the folder", from: "kb/auth.md", target: "../../README.md",
			want: Link{Kind: LinkPage, Broken: true}},
		{name: "external", from: "kb/auth.md", target: "https://example.com/a.md",
			want: Link{Kind: LinkExternal}},
		{name: "mail", from: "kb/auth.md", target: "mailto:a@b.c",
			want: Link{Kind: LinkExternal}},
		{name: "protocol-relative", from: "kb/auth.md", target: "//example.com/x",
			want: Link{Kind: LinkExternal}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := resolveLink(tt.from, Link{Target: tt.target}, files)
			tt.want.Target = tt.target

			if got != tt.want {
				t.Errorf("resolveLink(%q, %q) = %+v, want %+v", tt.from, tt.target, got, tt.want)
			}
		})
	}
}
