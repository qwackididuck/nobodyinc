package kb

import (
	"bytes"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"

	"github.com/qwackididuck/nobodyinc/internal/frontmatter"
)

// markdown is the one parser of the knowledge base, for the links, the
// index, the agents' tools and the HTML alike (ADR-0019): what the control
// room shows as a link is what the index counts as one. GitHub's flavor
// (tables, task lists, strikethrough, autolinks), headings with ids for the
// anchors. Raw HTML is never rendered (goldmark's default).
var markdown = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
	goldmark.WithParserOptions(parser.WithAutoHeadingID()),
)

// Page is one markdown file of the knowledge base.
type Page struct {
	// Path is the page's path in the knowledge folder, with forward
	// slashes: "adr/0003-isolate-agents.md".
	Path string
	// Title is its first level-one heading, else its file name without .md.
	Title string
	// Header is its YAML header, in order ("status: current").
	Header []frontmatter.Field
	Size   int64
	// Modified is the file's modification time.
	Modified time.Time
	// Digest identifies its content, so that only changed pages are indexed
	// again.
	Digest string
	// Links are its links, in order, resolved against the knowledge folder
	// (see Base.resolve).
	Links []Link
	// Text is its plain text, for the full-text index.
	Text string
	// Err is why it could not be read or parsed (too large, unreadable, a
	// symbolic link leaving the folder); the page is then listed, not
	// indexed.
	Err string
}

// LinkKind says where a link leads.
type LinkKind string

// Kinds of links.
const (
	// LinkPage leads to a page of the knowledge base (Link.Path).
	LinkPage LinkKind = "page"
	// LinkAsset leads to another file of the knowledge folder (an image).
	LinkAsset LinkKind = "asset"
	// LinkExternal leads out of the project (http, mailto...).
	LinkExternal LinkKind = "external"
	// LinkAnchor leads to a heading of the same page.
	LinkAnchor LinkKind = "anchor"
)

// Link is one link of a page.
type Link struct {
	// Target is the destination as written: "../adr/0003.md#context".
	Target string
	// Text is the link's text (an image's alternative text).
	Text string
	Kind LinkKind
	// Path is where a page or asset link leads in the knowledge folder.
	Path string
	// Anchor is the part after "#", without it.
	Anchor string
	// Image is true for an image (![alt](src)).
	Image bool
	// Broken is true when a page or asset link leads to nothing, or out of
	// the knowledge folder.
	Broken bool
}

// parsed is what parsePage reads from a file, before its links are resolved
// against the whole folder.
type parsed struct {
	title  string
	header []frontmatter.Field
	body   []byte
	links  []Link
	text   string
}

// parsePage reads a page's content: its header (via internal/frontmatter;
// a header that cannot be read is left in the body), its title, its raw
// links and its plain text.
func parsePage(pagePath string, data []byte) parsed {
	p := parsed{}
	p.body, p.header = splitHeader(data)

	doc := markdown.Parser().Parse(text.NewReader(p.body))

	var b strings.Builder

	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			if n.Type() == ast.TypeBlock {
				b.WriteByte('\n')
			}

			return ast.WalkContinue, nil
		}

		p.visit(n)
		writeLeafText(&b, n, p.body)

		return ast.WalkContinue, nil
	})

	p.text = strings.TrimSpace(b.String())
	if p.title == "" {
		p.title = strings.TrimSuffix(path.Base(pagePath), ".md")
	}

	return p
}

// splitHeader separates a page's YAML header, as fields in order, from its
// body. A header that cannot be read stays in the body.
func splitHeader(data []byte) ([]byte, []frontmatter.Field) {
	f, err := frontmatter.Parse(data)
	if err != nil {
		return data, nil
	}

	var header []frontmatter.Field

	if f.Description != "" {
		header = append(header, frontmatter.Field{Key: "description", Value: f.Description})
	}

	for _, key := range f.Keys() {
		header = append(header, frontmatter.Field{Key: key, Value: f.Value(key)})
	}

	return []byte(f.Body), header
}

// visit records what a node of the page brings: the title, a link.
func (p *parsed) visit(n ast.Node) {
	switch n := n.(type) {
	case *ast.Heading:
		if n.Level == 1 && p.title == "" {
			p.title = plainText(n, p.body)
		}
	case *ast.Link:
		p.links = append(p.links, Link{Target: string(n.Destination), Text: plainText(n, p.body)})
	case *ast.Image:
		p.links = append(p.links, Link{Target: string(n.Destination), Text: plainText(n, p.body), Image: true})
	case *ast.AutoLink:
		p.links = append(p.links, Link{Target: string(n.URL(p.body)), Text: string(n.Label(p.body))})
	}
}

// writeLeafText adds the text a node carries itself (not its children's)
// to b: words, and the lines of code blocks.
func writeLeafText(b *strings.Builder, n ast.Node, source []byte) {
	switch n := n.(type) {
	case *ast.Text:
		b.Write(n.Value(source))

		if n.SoftLineBreak() || n.HardLineBreak() {
			b.WriteByte(' ')
		}
	case *ast.String:
		b.Write(n.Value)
	case *ast.AutoLink:
		b.Write(n.Label(source))
	case *ast.CodeBlock, *ast.FencedCodeBlock:
		lines := n.Lines()
		for i := range lines.Len() {
			seg := lines.At(i)
			b.Write(seg.Value(source))
		}
	}
}

// plainText is the text of a node's children, without markup.
func plainText(n ast.Node, source []byte) string {
	var b strings.Builder

	_ = ast.Walk(n, func(c ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering && c != n {
			writeLeafText(&b, c, source)
		}

		return ast.WalkContinue, nil
	})

	return strings.TrimSpace(b.String())
}

// resolveLink tells where a raw link of the page at from leads, given the
// knowledge folder's files: an external URL, an anchor of the same page, a
// page or another file. A leading "/" is the knowledge folder's root. A
// link leaving the folder is broken.
func resolveLink(from string, l Link, files map[string]bool) Link {
	target := strings.TrimSpace(l.Target)

	if u, err := url.Parse(target); err == nil && (u.Scheme != "" || strings.HasPrefix(target, "//")) {
		l.Kind = LinkExternal

		return l
	}

	rest, anchor, _ := strings.Cut(target, "#")
	rest, _, _ = strings.Cut(rest, "?")
	l.Anchor = anchor

	if rest == "" {
		l.Kind, l.Path = LinkAnchor, from

		return l
	}

	joined, inside := joinTarget(from, rest)

	l.Kind = LinkAsset
	if isPage(joined) {
		l.Kind = LinkPage
	}

	if !inside {
		l.Broken = true

		return l
	}

	l.Path = joined
	l.Broken = !files[joined]

	return l
}

// joinTarget joins a link's path (escapes decoded) to the folder of the
// page at from, or to the knowledge folder's root when it starts with "/",
// and tells whether the result stays inside the knowledge folder.
func joinTarget(from, target string) (string, bool) {
	if unescaped, err := url.PathUnescape(target); err == nil {
		target = unescaped
	}

	var joined string
	if fromRoot, ok := strings.CutPrefix(target, "/"); ok {
		joined = path.Clean(fromRoot)
	} else {
		joined = path.Join(path.Dir(from), target)
	}

	inside := joined != "." && joined != ".." && !strings.HasPrefix(joined, "../")

	return joined, inside
}

// isPage reports whether a file name is a page.
func isPage(name string) bool {
	return strings.EqualFold(path.Ext(name), ".md")
}

// trimBOM drops a UTF-8 byte order mark, which some editors write.
func trimBOM(data []byte) []byte {
	return bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
}
