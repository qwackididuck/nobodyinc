package kb

import (
	"bytes"
	"fmt"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

// Prefixes of the links Render writes, which the control room's reader
// turns into navigation: "kb:adr/0003.md#context" opens a page of the
// knowledge base (an anchor of the same page included), "kb-asset:img/a.png"
// is a file served by GET /api/kb/assets/{path}. The daemon does not know
// the address the control room reaches it at (behind the launcher's proxy),
// so it cannot write the final URLs itself.
const (
	PageLinkPrefix  = "kb:"
	AssetLinkPrefix = "kb-asset:"
)

// Classes Render puts on links, for the control room's style.
const (
	classPage     = "kb-page"
	classAsset    = "kb-asset"
	classExternal = "kb-external"
	classBroken   = "kb-broken"
)

// Render returns the page at p as HTML for the control room, with the page
// it was read from. Raw HTML in the page is not rendered (goldmark leaves a
// comment in its place); links to pages and anchors become "kb:" links,
// images and other files "kb-asset:" ones, each with a class saying what it
// is (and kb-broken when it leads to nothing).
func Render(b *Base, p string) (string, *Page, error) {
	data, page, err := b.Source(p)
	if err != nil {
		return "", nil, err
	}

	body, _ := splitHeader(data)
	doc := markdown.Parser().Parse(text.NewReader(body))

	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}

		switch n := n.(type) {
		case *ast.Link:
			n.Destination = b.rewrite(page.Path, n, string(n.Destination), false)
		case *ast.Image:
			n.Destination = b.rewrite(page.Path, n, string(n.Destination), true)
		case *ast.AutoLink:
			n.SetAttributeString("class", []byte(classExternal))
		}

		return ast.WalkContinue, nil
	})

	var out bytes.Buffer
	if err := markdown.Renderer().Render(&out, body, doc); err != nil {
		return "", nil, fmt.Errorf("render %s: %w", page.Path, err)
	}

	return out.String(), page, nil
}

// rewrite returns the destination a link or image of the page at from gets
// in the HTML, and sets its class.
func (b *Base) rewrite(from string, n ast.Node, target string, image bool) []byte {
	l := resolveLink(from, Link{Target: target, Image: image}, b.files)

	class, dest := classExternal, target

	switch l.Kind {
	case LinkExternal:
	case LinkPage, LinkAnchor:
		class, dest = classPage, PageLinkPrefix+l.Path
		if l.Anchor != "" {
			dest += "#" + l.Anchor
		}
	case LinkAsset:
		class, dest = classAsset, AssetLinkPrefix+l.Path
	}

	if l.Broken {
		class += " " + classBroken
	}

	n.SetAttributeString("class", []byte(class))

	return []byte(dest)
}
