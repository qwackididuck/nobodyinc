package server

import (
	"context"
	"errors"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/qwackididuck/nobodyinc/internal/frontmatter"
	"github.com/qwackididuck/nobodyinc/internal/kb"
)

// The knowledge base (ADR-0019, docs/knowledge.md): a folder of the project
// declared in org.yaml, read through kb.Service. Setting it up, moving it or
// removing it goes through staffing (the agents restart with or without
// their knowledge tools); everything else only reads.

// routeKnowledge mounts the knowledge base endpoints.
func (s *server) routeKnowledge(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/kb", s.getKnowledge)
	mux.HandleFunc("PUT /api/kb", s.setKnowledge)
	mux.HandleFunc("GET /api/kb/tree", s.getKnowledgeTree)
	mux.HandleFunc("GET /api/kb/pages/{path...}", s.getKnowledgePage)
	mux.HandleFunc("GET /api/kb/search", s.searchKnowledge)
	mux.HandleFunc("GET /api/kb/assets/{path...}", s.getKnowledgeAsset)
}

// recentPages is how many pages GET /api/kb lists as changed last.
const recentPages = 8

// knowledgeOverview is GET /api/kb.
type knowledgeOverview struct {
	Path      string               `json:"path"`
	Suggested string               `json:"suggested"`
	Exists    bool                 `json:"exists"`
	Error     string               `json:"error,omitempty"`
	Pages     int                  `json:"pages"`
	Home      string               `json:"home"`
	Folders   []knowledgeFolder    `json:"folders"`
	Recent    []knowledgePageRef   `json:"recent"`
	Broken    []knowledgeBroken    `json:"broken"`
	Orphans   []knowledgePageRef   `json:"orphans"`
	Errors    []knowledgePageError `json:"errors"`
}

type knowledgeFolder struct {
	Path  string `json:"path"`
	Pages int    `json:"pages"`
}

type knowledgePageRef struct {
	Path     string     `json:"path"`
	Title    string     `json:"title"`
	Modified *time.Time `json:"modified,omitempty"`
}

type knowledgeBroken struct {
	Page   string `json:"page"`
	Target string `json:"target"`
	Text   string `json:"text"`
}

type knowledgePageError struct {
	Path  string `json:"path"`
	Error string `json:"error"`
}

// knowledgeSettings is the body of PUT /api/kb.
type knowledgeSettings struct {
	Path string `json:"path"`
}

// knowledgeTree is GET /api/kb/tree.
type knowledgeTree struct {
	Path string         `json:"path"`
	Home string         `json:"home"`
	Root *knowledgeNode `json:"root"`
}

type knowledgeNode struct {
	Name     string           `json:"name"`
	Path     string           `json:"path"`
	Dir      bool             `json:"dir"`
	Title    string           `json:"title,omitempty"`
	Error    string           `json:"error,omitempty"`
	Pages    int              `json:"pages,omitempty"`
	Children []*knowledgeNode `json:"children,omitempty"`
}

// knowledgePage is GET /api/kb/pages/{path}.
type knowledgePage struct {
	Path      string             `json:"path"`
	Title     string             `json:"title"`
	Header    []frontmatterField `json:"header"`
	HTML      string             `json:"html"`
	Size      int64              `json:"size"`
	Modified  time.Time          `json:"modified"`
	Links     []knowledgeLink    `json:"links"`
	Backlinks []knowledgePageRef `json:"backlinks"`
}

type frontmatterField struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type knowledgeLink struct {
	Target string      `json:"target"`
	Text   string      `json:"text"`
	Kind   kb.LinkKind `json:"kind"`
	Path   string      `json:"path,omitempty"`
	Anchor string      `json:"anchor,omitempty"`
	Title  string      `json:"title,omitempty"`
	Image  bool        `json:"image,omitempty"`
	Broken bool        `json:"broken"`
}

// knowledgeSearch is GET /api/kb/search.
type knowledgeSearch struct {
	Query   string            `json:"query"`
	Results []knowledgeResult `json:"results"`
}

type knowledgeResult struct {
	Path    string             `json:"path"`
	Title   string             `json:"title"`
	Snippet []knowledgeSegment `json:"snippet"`
}

type knowledgeSegment struct {
	Text  string `json:"text"`
	Match bool   `json:"match,omitempty"`
}

func (s *server) getKnowledge(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, http.StatusOK, s.knowledgeOverview(r.Context()))
}

// knowledgeOverview describes the knowledge base, or why it cannot be read.
func (s *server) knowledgeOverview(ctx context.Context) knowledgeOverview {
	v := knowledgeOverview{
		Path: s.Org.Current().KnowledgePath(), Suggested: kb.DefaultPath,
		Folders: []knowledgeFolder{}, Recent: []knowledgePageRef{}, Broken: []knowledgeBroken{},
		Orphans: []knowledgePageRef{}, Errors: []knowledgePageError{},
	}

	if v.Path == "" || s.Knowledge == nil {
		return v
	}

	b, err := s.Knowledge.Base(ctx)
	if err != nil {
		v.Error = err.Error()

		return v
	}

	v.Exists, v.Pages, v.Home = !b.Missing, len(b.Pages), b.Home()

	for _, f := range b.Folders() {
		v.Folders = append(v.Folders, knowledgeFolder{Path: f.Path, Pages: f.Pages})
	}

	for _, p := range b.Recent(recentPages) {
		v.Recent = append(v.Recent, pageRef(p))
	}

	for _, l := range b.Broken() {
		v.Broken = append(v.Broken, knowledgeBroken{Page: l.Page, Target: l.Target, Text: l.Text})
	}

	for _, o := range b.Orphans() {
		if p, err := b.Page(o); err == nil {
			v.Orphans = append(v.Orphans, pageRef(p))
		}
	}

	for _, p := range b.Errors() {
		v.Errors = append(v.Errors, knowledgePageError{Path: p.Path, Error: p.Err})
	}

	return v
}

func (s *server) setKnowledge(w http.ResponseWriter, r *http.Request) {
	if !s.hasStaff(w) {
		return
	}

	var body knowledgeSettings
	if err := decodeJSON(w, r, &body, true); err != nil {
		s.writeError(w, http.StatusBadRequest, err)

		return
	}

	// The agents' processes must outlive this request.
	if err := s.Staff.SetKnowledge(context.WithoutCancel(r.Context()), strings.TrimSpace(body.Path)); err != nil {
		s.staffError(w, err)

		return
	}

	// Scan the new folder now: the control room hears of it at once.
	if s.Knowledge != nil {
		if _, err := s.Knowledge.Refresh(r.Context()); err != nil && !errors.Is(err, kb.ErrNone) {
			s.Logger.Warn("scan the knowledge base", "err", err)
		}
	}

	s.writeJSON(w, http.StatusOK, s.knowledgeOverview(r.Context()))
}

func (s *server) getKnowledgeTree(w http.ResponseWriter, r *http.Request) {
	b, ok := s.knowledgeBase(w, r)
	if !ok {
		return
	}

	s.writeJSON(w, http.StatusOK, knowledgeTree{Path: b.Path, Home: b.Home(), Root: treeNode(b.Tree())})
}

func (s *server) getKnowledgePage(w http.ResponseWriter, r *http.Request) {
	b, ok := s.knowledgeBase(w, r)
	if !ok {
		return
	}

	html, page, err := kb.Render(b, r.PathValue("path"))
	if err != nil {
		s.knowledgeError(w, err)

		return
	}

	v := knowledgePage{
		Path: page.Path, Title: page.Title, Header: []frontmatterField{}, HTML: html, Size: page.Size, Modified: page.Modified,
		Links: []knowledgeLink{}, Backlinks: []knowledgePageRef{},
	}

	for _, f := range page.Header {
		v.Header = append(v.Header, fieldView(f))
	}

	for _, l := range page.Links {
		link := knowledgeLink{
			Target: l.Target, Text: l.Text, Kind: l.Kind, Path: l.Path, Anchor: l.Anchor, Image: l.Image, Broken: l.Broken,
		}

		if l.Kind == kb.LinkPage && !l.Broken {
			if target, err := b.Page(l.Path); err == nil {
				link.Title = target.Title
			}
		}

		v.Links = append(v.Links, link)
	}

	for _, from := range b.Backlinks(page.Path) {
		if p, err := b.Page(from); err == nil {
			v.Backlinks = append(v.Backlinks, knowledgePageRef{Path: p.Path, Title: p.Title})
		}
	}

	s.writeJSON(w, http.StatusOK, v)
}

// searchLimits bound GET /api/kb/search's limit.
const (
	defaultSearchLimit = 30
	maxSearchLimit     = 100
)

func (s *server) searchKnowledge(w http.ResponseWriter, r *http.Request) {
	if s.Knowledge == nil || s.Org.Current().KnowledgePath() == "" {
		s.writeError(w, http.StatusNotFound, kb.ErrNone)

		return
	}

	limit := defaultSearchLimit

	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > maxSearchLimit {
			s.writeError(w, http.StatusBadRequest, errors.New("limit: a number from 1 to 100"))

			return
		}

		limit = n
	}

	query := r.URL.Query().Get("q")

	hits, err := s.Knowledge.Search(r.Context(), query, limit)
	if err != nil {
		s.knowledgeError(w, err)

		return
	}

	v := knowledgeSearch{Query: query, Results: []knowledgeResult{}}

	for _, h := range hits {
		result := knowledgeResult{Path: h.Path, Title: h.Title, Snippet: []knowledgeSegment{}}
		for _, seg := range h.Snippet {
			result.Snippet = append(result.Snippet, knowledgeSegment(seg))
		}

		v.Results = append(v.Results, result)
	}

	s.writeJSON(w, http.StatusOK, v)
}

// imageTypes are the files GET /api/kb/assets serves, by extension.
var imageTypes = map[string]string{
	".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".gif": "image/gif",
	".webp": "image/webp", ".svg": "image/svg+xml", ".avif": "image/avif",
}

func (s *server) getKnowledgeAsset(w http.ResponseWriter, r *http.Request) {
	b, ok := s.knowledgeBase(w, r)
	if !ok {
		return
	}

	p := r.PathValue("path")

	contentType, ok := imageTypes[strings.ToLower(path.Ext(p))]
	if !ok {
		s.writeError(w, http.StatusUnsupportedMediaType, errors.New("only images are served"))

		return
	}

	data, err := b.Asset(p)
	if err != nil {
		s.knowledgeError(w, err)

		return
	}

	h := w.Header()
	h.Set("Content-Type", contentType)
	// An SVG opened on its own runs no script, and nothing is sniffed.
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)

	// G705: an image of the knowledge folder, sent as such (above: its type
	// from an allow-list, no sniffing, and a sandbox for an SVG opened on its
	// own), never inserted into a page of ours as HTML.
	if _, err := w.Write(data); err != nil { //nolint:gosec // see above
		s.Logger.Debug("write a knowledge asset", "err", err)
	}
}

// knowledgeBase returns the knowledge base, or answers the error.
func (s *server) knowledgeBase(w http.ResponseWriter, r *http.Request) (*kb.Base, bool) {
	if s.Knowledge == nil {
		s.writeError(w, http.StatusNotFound, kb.ErrNone)

		return nil, false
	}

	b, err := s.Knowledge.Base(r.Context())
	if err != nil {
		s.knowledgeError(w, err)

		return nil, false
	}

	return b, true
}

// knowledgeError answers an error of the knowledge base. Page contents are
// never logged; paths are.
func (s *server) knowledgeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, kb.ErrNone), errors.Is(err, kb.ErrNotFound):
		s.writeError(w, http.StatusNotFound, err)
	default:
		s.Logger.Warn("read the knowledge base", "err", err)
		s.writeError(w, http.StatusInternalServerError, err)
	}
}

func pageRef(p *kb.Page) knowledgePageRef {
	ref := knowledgePageRef{Path: p.Path, Title: p.Title}
	if !p.Modified.IsZero() {
		modified := p.Modified.UTC()
		ref.Modified = &modified
	}

	return ref
}

func fieldView(f frontmatter.Field) frontmatterField {
	return frontmatterField{Key: f.Key, Value: f.Value}
}

func treeNode(n *kb.Node) *knowledgeNode {
	v := &knowledgeNode{Name: n.Name, Path: n.Path, Dir: n.Dir, Title: n.Title, Error: n.Err, Pages: n.Pages}

	for _, c := range n.Children {
		v.Children = append(v.Children, treeNode(c))
	}

	return v
}
