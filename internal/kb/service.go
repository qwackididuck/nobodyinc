package kb

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode"
)

// Index is the full-text index of the knowledge base (implemented by
// internal/store, with SQLite FTS5): derived from the files, rebuilt from
// them at will.
type Index interface {
	// SyncKB makes the index hold exactly docs, indexing again only the
	// pages whose digest changed; it returns how many it indexed, then how
	// many it removed.
	SyncKB(ctx context.Context, docs []Doc) (int, int, error)
	// SearchKB finds the pages matching the words of query, best first.
	SearchKB(ctx context.Context, query string, limit int) ([]IndexHit, error)
}

// Doc is one page as the index holds it.
type Doc struct {
	Path, Digest, Title, Body string
}

// IndexHit is one result of the index: Snippet is a piece of the page's
// text around the matches, each between MatchStart and MatchEnd.
type IndexHit struct {
	Path, Title, Snippet string
}

// Markers of the matches in IndexHit.Snippet: control characters, which
// page text never holds.
const (
	MatchStart = "\x02"
	MatchEnd   = "\x03"
)

// DefaultMaxAge is how long a scan is reused before a read scans the folder
// again: reads in a row (the reader, an agent's tools) share one scan.
const DefaultMaxAge = 2 * time.Second

// Service is the knowledge base of a running daemon: the folder declared in
// org.yaml, scanned again when it is read and older than MaxAge (or by
// Run), and its full-text index kept in step with the files. There is no
// file watcher: a scan only stats the files, and reads again those whose
// size or modification time changed.
type Service struct {
	// ProjectDir is the project's main checkout.
	ProjectDir string
	// Declared returns the folder declared in org.yaml, "" when the project
	// has no knowledge base. It is read at each scan: the human may change
	// it at any time.
	Declared func() string
	// Index holds the full-text index.
	Index Index
	// OnChange, when set, is told after a scan that changed pages, or after
	// the folder itself changed (set up, moved or removed: Update.Reset).
	OnChange func(Update)
	// MaxAge overrides DefaultMaxAge (tests).
	MaxAge time.Duration
	Logger *slog.Logger

	mu      sync.Mutex
	base    *Base
	scanned time.Time
}

// Update is what a scan tells OnChange.
type Update struct {
	Change

	// Pages is the number of pages after the scan (0 without a knowledge
	// base).
	Pages int
	// Reset is true when the folder itself changed: the control room
	// reloads without announcing new pages.
	Reset bool
}

// Base returns the knowledge base, scanned again first when the last scan
// is older than MaxAge or was of another folder. It returns ErrNone when
// the project has no knowledge base.
func (s *Service) Base(ctx context.Context) (*Base, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	declared := s.Declared()
	if declared != "" && s.base != nil && s.base.Path == declared && time.Since(s.scanned) < s.maxAge() {
		return s.base, nil
	}

	return s.refreshLocked(ctx, declared)
}

// Refresh scans the folder now, whatever the age of the last scan.
func (s *Service) Refresh(ctx context.Context) (*Base, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.refreshLocked(ctx, s.Declared())
}

// Run scans the folder every interval until ctx ends, so that the control
// room hears of changed pages even when nobody reads them.
func (s *Service) Run(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if _, err := s.Refresh(ctx); err != nil && !errors.Is(err, ErrNone) && ctx.Err() == nil {
				s.logger().Warn("scan the knowledge base", "err", err)
			}
		}
	}
}

// Search finds the pages matching query, best first, at most limit.
func (s *Service) Search(ctx context.Context, query string, limit int) ([]Hit, error) {
	b, err := s.Base(ctx)
	if err != nil {
		return nil, err
	}

	found, err := s.Index.SearchKB(ctx, query, limit)
	if err != nil {
		return nil, err
	}

	hits := make([]Hit, 0, len(found))

	for _, f := range found {
		if b.page(f.Path) == nil {
			continue
		}

		hits = append(hits, Hit{Path: f.Path, Title: f.Title, Snippet: segments(f.Snippet)})
	}

	return hits, nil
}

// refreshLocked scans declared (or forgets the base when it is ""), keeps
// the index in step and tells OnChange. s.mu is held.
func (s *Service) refreshLocked(ctx context.Context, declared string) (*Base, error) {
	if declared == "" {
		return nil, s.forgetLocked(ctx)
	}

	root, err := Resolve(s.ProjectDir, declared)
	if err != nil {
		return nil, err
	}

	previous := s.base
	if previous != nil && previous.Path != declared {
		previous = nil
	}

	b, err := Scan(declared, root, previous)
	if err != nil {
		return nil, err
	}

	if _, _, err := s.Index.SyncKB(ctx, indexDocs(b)); err != nil {
		return nil, err
	}

	reset := previous == nil
	change := diff(previous, b)

	s.base, s.scanned = b, time.Now()

	if reset || change.Any() {
		s.notify(Update{Pages: len(b.Pages), Change: change, Reset: reset})
	}

	return b, nil
}

// forgetLocked forgets the knowledge base the project no longer has, and
// empties the index; it returns ErrNone. s.mu is held.
func (s *Service) forgetLocked(ctx context.Context) error {
	if s.base == nil {
		return ErrNone
	}

	if _, _, err := s.Index.SyncKB(ctx, nil); err != nil {
		return err
	}

	s.base = nil
	s.notify(Update{Reset: true})

	return ErrNone
}

// indexDocs is what the index should hold: every page that could be read.
func indexDocs(b *Base) []Doc {
	docs := make([]Doc, 0, len(b.Pages))

	for _, p := range b.Pages {
		if p.Err == "" {
			docs = append(docs, Doc{Path: p.Path, Digest: p.Digest, Title: p.Title, Body: p.Text})
		}
	}

	return docs
}

// Hit is one search result.
type Hit struct {
	Path, Title string
	// Snippet is a piece of the page's text around the matches.
	Snippet []Segment
}

// Segment is a piece of a snippet; Match is true for the words found.
type Segment struct {
	Text  string
	Match bool
}

// Plain renders a snippet as text, matches between « and ».
func (h Hit) Plain() string {
	var b strings.Builder

	for _, seg := range h.Snippet {
		if seg.Match {
			b.WriteString("«")
			b.WriteString(seg.Text)
			b.WriteString("»")
		} else {
			b.WriteString(seg.Text)
		}
	}

	return b.String()
}

// segments splits a snippet of the index at its match markers, with its
// runs of spaces and line breaks made one space.
func segments(snippet string) []Segment {
	var out []Segment

	for rest := collapseSpaces(snippet); rest != ""; {
		before, after, found := strings.Cut(rest, MatchStart)
		if before != "" {
			out = append(out, Segment{Text: before})
		}

		if !found {
			break
		}

		match, tail, _ := strings.Cut(after, MatchEnd)
		out = append(out, Segment{Text: match, Match: true})
		rest = tail
	}

	return out
}

// collapseSpaces makes every run of white space in s a single space.
func collapseSpaces(s string) string {
	var b strings.Builder

	space := false

	for _, r := range s {
		if unicode.IsSpace(r) {
			space = true

			continue
		}

		if space && b.Len() > 0 {
			b.WriteByte(' ')
		}

		space = false

		b.WriteRune(r)
	}

	if space && b.Len() > 0 {
		b.WriteByte(' ')
	}

	return b.String()
}

func (s *Service) notify(u Update) {
	if s.OnChange != nil {
		s.OnChange(u)
	}
}

func (s *Service) maxAge() time.Duration {
	if s.MaxAge > 0 {
		return s.MaxAge
	}

	return DefaultMaxAge
}

func (s *Service) logger() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}

	return slog.Default()
}
