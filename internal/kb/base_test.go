package kb

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

// sampleDocs is a small knowledge folder: a home page, two folders, links
// between pages, a broken link, an orphan, an image, a hidden folder and a
// file that is not a page.
//
//nolint:gosec // G101: "auth" and "token" are page names here, not credentials
var sampleDocs = map[string]string{
	"README.md":              "# Docs\n\nStart with [auth](kb/auth.md) and [ADR-0003](adr/0003-isolate.md).\n",
	"kb/auth.md":             "---\nstatus: current\n---\n# Authentication\n\nSee [ADR](../adr/0003-isolate.md#context), [refresh](token-refresh.md), [old](../adr/0099.md).\n\n![flow](img/flow.png)\n",
	"kb/token-refresh.md":    "# Token refresh\n\nBack to [auth](auth.md#flow).\n",
	"kb/img/flow.png":        "PNG",
	"kb/orphan.md":           "# Orphan\n\nNobody links here.\n",
	"adr/0003-isolate.md":    "# ADR-0003 Isolate agents\n\n## Context\n\nAgents are isolated.\n",
	"adr/notes.txt":          "not a page",
	".obsidian/workspace.md": "# hidden\n",
	"kb/.draft.md":           "# hidden draft\n",
}

func scanSample(t *testing.T) *Base {
	t.Helper()

	root := t.TempDir()
	writeFiles(t, root, sampleDocs)

	b, err := Scan("docs", root, nil)
	if err != nil {
		t.Fatal(err)
	}

	return b
}

func TestScan(t *testing.T) {
	t.Parallel()

	b := scanSample(t)

	tests := []struct {
		name string
		got  any
		want any
	}{
		{name: "pages, sorted, hidden ones skipped", got: pagePaths(b),
			want: []string{"README.md", "adr/0003-isolate.md", "kb/auth.md", "kb/orphan.md", "kb/token-refresh.md"}},
		{name: "home page", got: b.Home(), want: "README.md"},
		{name: "backlinks of a page", got: b.Backlinks("adr/0003-isolate.md"), want: []string{"README.md", "kb/auth.md"}},
		{name: "backlinks of a page linking back", got: b.Backlinks("kb/auth.md"), want: []string{"README.md", "kb/token-refresh.md"}},
		{name: "orphans, home aside", got: b.Orphans(), want: []string{"kb/orphan.md"}},
		{name: "broken links", got: b.Broken(), want: []BrokenLink{{Page: "kb/auth.md", Target: "../adr/0099.md", Text: "old"}}},
		{name: "top-level folders", got: b.Folders(), want: []Folder{{Path: "adr", Pages: 1}, {Path: "kb", Pages: 3}}},
		{name: "assets are files", got: b.HasFile("kb/img/flow.png") && b.HasFile("adr/notes.txt"), want: true},
		{name: "hidden files are not", got: b.HasFile("kb/.draft.md"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if !reflect.DeepEqual(tt.got, tt.want) {
				t.Errorf("got %+v, want %+v", tt.got, tt.want)
			}
		})
	}
}

func TestTree(t *testing.T) {
	t.Parallel()

	tree := scanSample(t).Tree()
	lines := treeLines(tree, 0)

	want := []string{
		"adr/ *",
		"  0003-isolate.md = ADR-0003 Isolate agents",
		"kb/ ***",
		"  auth.md = Authentication",
		"  orphan.md = Orphan",
		"  token-refresh.md = Token refresh",
		"README.md = Docs",
	}

	if tree.Pages != 5 || !slices.Equal(lines, want) {
		t.Errorf("tree (%d pages):\n%s\nwant:\n%s", tree.Pages, strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
}

func TestList(t *testing.T) {
	t.Parallel()

	b := scanSample(t)

	tests := []struct {
		folder      string
		dirs, pages []string
		err         bool
	}{
		{folder: "", dirs: []string{"adr", "kb"}, pages: []string{"README.md"}},
		{folder: "kb", pages: []string{"kb/auth.md", "kb/orphan.md", "kb/token-refresh.md"}},
		{folder: "/kb/", pages: []string{"kb/auth.md", "kb/orphan.md", "kb/token-refresh.md"}},
		{folder: "nope", err: true},
		{folder: "../kb", pages: []string{"kb/auth.md", "kb/orphan.md", "kb/token-refresh.md"}},
	}

	for _, tt := range tests {
		t.Run(tt.folder, func(t *testing.T) {
			t.Parallel()

			dirs, pages, err := b.List(tt.folder)
			if tt.err != (err != nil) {
				t.Fatalf("List(%q) error = %v", tt.folder, err)
			}

			if got := nodePaths(dirs); !slices.Equal(got, tt.dirs) {
				t.Errorf("dirs = %q, want %q", got, tt.dirs)
			}

			if got := nodePaths(pages); !slices.Equal(got, tt.pages) {
				t.Errorf("pages = %q, want %q", got, tt.pages)
			}
		})
	}
}

func TestScanEdgeCases(t *testing.T) {
	t.Parallel()

	t.Run("a missing folder is an empty base", func(t *testing.T) {
		t.Parallel()

		b, err := Scan("docs", filepath.Join(t.TempDir(), "docs"), nil)
		if err != nil || !b.Missing || len(b.Pages) != 0 || b.Home() != "" {
			t.Fatalf("Scan = %+v, %v; want a missing, empty base", b, err)
		}
	})

	t.Run("a page too large is listed with its error", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		writeFiles(t, root, map[string]string{"big.md": "# Big\n" + strings.Repeat("x", MaxPageBytes)})

		b, err := Scan("docs", root, nil)
		if err != nil {
			t.Fatal(err)
		}

		if p := b.page("big.md"); p == nil || !strings.Contains(p.Err, "limit") || p.Digest != "" {
			t.Fatalf("big.md = %+v, want an error and no digest", p)
		}

		if errs := b.Errors(); len(errs) != 1 {
			t.Errorf("Errors() = %d pages, want 1", len(errs))
		}
	})

	t.Run("a symbolic link leaving the folder is not read", func(t *testing.T) {
		t.Parallel()

		outside := t.TempDir()
		writeFiles(t, outside, map[string]string{"secret.md": "# Secret\n"})

		root := t.TempDir()
		mustSymlink(t, filepath.Join(outside, "secret.md"), filepath.Join(root, "secret.md"))

		b, err := Scan("docs", root, nil)
		if err != nil {
			t.Fatal(err)
		}

		p := b.page("secret.md")
		if p == nil || p.Err == "" || p.Title == "Secret" {
			t.Fatalf("secret.md = %+v, want an error and no content read", p)
		}

		if _, _, err := b.Source("secret.md"); err == nil {
			t.Error("Source(secret.md) read a file outside the folder")
		}
	})

	t.Run("an unchanged page is reused, a changed one read again", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		writeFiles(t, root, map[string]string{"a.md": "# A\n", "b.md": "# B\n"})

		first, err := Scan("docs", root, nil)
		if err != nil {
			t.Fatal(err)
		}

		writeFiles(t, root, map[string]string{"b.md": "# B changed\n", "c.md": "# C\n"})

		later := time.Now().Add(time.Minute)
		if err := os.Chtimes(filepath.Join(root, "b.md"), later, later); err != nil {
			t.Fatal(err)
		}

		if err := os.Remove(filepath.Join(root, "a.md")); err != nil {
			t.Fatal(err)
		}

		second, err := Scan("docs", root, first)
		if err != nil {
			t.Fatal(err)
		}

		if got := second.page("b.md").Title; got != "B changed" {
			t.Errorf("b.md title = %q, want it read again", got)
		}

		if got := diff(first, second); got != (Change{Added: 1, Removed: 1, Changed: 1}) {
			t.Errorf("diff = %+v, want one added, one removed, one changed", got)
		}
	})

	t.Run("index.md wins over README.md", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		writeFiles(t, root, map[string]string{"index.md": "# I\n", "README.md": "# R\n"})

		b, err := Scan("docs", root, nil)
		if err != nil {
			t.Fatal(err)
		}

		if got := b.Home(); got != "index.md" {
			t.Errorf("Home() = %q, want index.md", got)
		}
	})
}

func TestAsset(t *testing.T) {
	t.Parallel()

	b := scanSample(t)

	tests := []struct {
		path string
		ok   bool
	}{
		{path: "kb/img/flow.png", ok: true},
		{path: "kb/img/none.png"},
		{path: "../outside.png"},
		{path: "kb/.draft.md"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()

			data, err := b.Asset(tt.path)
			if tt.ok != (err == nil) {
				t.Fatalf("Asset(%q) = %q, %v", tt.path, data, err)
			}
		})
	}
}

// A page reused by the next scan has its links resolved again (a broken
// link is mended when its page appears), and the previous snapshot, which
// may still be read, is left as it was.
func TestRescanResolvesReusedLinksOnACopy(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFiles(t, root, map[string]string{"a.md": "# A\n\n[b](b.md)\n"})

	first, err := Scan("docs", root, nil)
	if err != nil {
		t.Fatal(err)
	}

	writeFiles(t, root, map[string]string{"b.md": "# B\n"})

	second, err := Scan("docs", root, first)
	if err != nil {
		t.Fatal(err)
	}

	if !first.page("a.md").Links[0].Broken {
		t.Error("the previous snapshot's link changed")
	}

	if second.page("a.md").Links[0].Broken {
		t.Error("the link to the new page is still broken")
	}

	if got := second.Backlinks("b.md"); !slices.Equal(got, []string{"a.md"}) {
		t.Errorf("backlinks of b.md = %q", got)
	}
}

func pagePaths(b *Base) []string {
	var paths []string
	for _, p := range b.Pages {
		paths = append(paths, p.Path)
	}

	return paths
}

func nodePaths(nodes []*Node) []string {
	var paths []string
	for _, n := range nodes {
		paths = append(paths, n.Path)
	}

	return paths
}

// treeLines draws a tree, one line per node: "name/ ***" for a folder (one
// star per page), "name = title" for a page.
func treeLines(n *Node, depth int) []string {
	var lines []string

	for _, c := range n.Children {
		line := strings.Repeat("  ", depth) + c.Name
		if c.Dir {
			line += "/ " + strings.Repeat("*", c.Pages)
		} else {
			line += " = " + c.Title
		}

		lines = append(lines, line)
		lines = append(lines, treeLines(c, depth+1)...)
	}

	return lines
}
