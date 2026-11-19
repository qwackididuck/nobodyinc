package kb

import (
	"strings"
	"testing"
)

func TestRender(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"kb/auth.md": "---\nstatus: current\n---\n# Authentication\n\n" +
			"[ADR](../adr/0003.md#context) [flow](#flow) [old](../adr/0099.md) [site](https://example.com) " +
			"[pdf](files/spec.pdf) <https://auto.example>\n\n" +
			"![diagram](img/flow.png) ![gone](img/none.png)\n\n" +
			"## Flow\n\n<script>alert(1)</script>\n\n<b onclick=\"x()\">bold</b>\n\n[js](javascript:alert(1))\n",
		"adr/0003.md":       "# ADR\n",
		"kb/img/flow.png":   "PNG",
		"kb/files/spec.pdf": "PDF",
	})

	b, err := Scan("docs", root, nil)
	if err != nil {
		t.Fatal(err)
	}

	html, page, err := Render(b, "kb/auth.md")
	if err != nil {
		t.Fatal(err)
	}

	if page.Title != "Authentication" {
		t.Errorf("page title = %q", page.Title)
	}

	tests := []struct {
		name   string
		want   string
		absent bool
	}{
		{name: "page link", want: `<a href="kb:adr/0003.md#context" class="kb-page">ADR</a>`},
		{name: "anchor of the same page", want: `<a href="kb:kb/auth.md#flow" class="kb-page">flow</a>`},
		{name: "broken page link", want: `<a href="kb:adr/0099.md" class="kb-page kb-broken">old</a>`},
		{name: "external link", want: `<a href="https://example.com" class="kb-external">site</a>`},
		{name: "other file", want: `<a href="kb-asset:kb/files/spec.pdf" class="kb-asset">pdf</a>`},
		{name: "autolink", want: `class="kb-external">https://auto.example</a>`},
		{name: "image", want: `<img src="kb-asset:kb/img/flow.png" alt="diagram" class="kb-asset">`},
		{name: "broken image", want: `<img src="kb-asset:kb/img/none.png" alt="gone" class="kb-asset kb-broken">`},
		{name: "heading id for anchors", want: `<h2 id="flow">Flow</h2>`},
		{name: "header left out", want: "status: current", absent: true},
		{name: "no script", want: "<script", absent: true},
		{name: "no event handler", want: "onclick", absent: true},
		{name: "no javascript URL", want: `href="javascript:`, absent: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if strings.Contains(html, tt.want) == tt.absent {
				t.Errorf("HTML (absent=%v) %q:\n%s", tt.absent, tt.want, html)
			}
		})
	}
}
