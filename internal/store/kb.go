package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/qwackididuck/nobodyinc/internal/kb"
)

// The knowledge base's full-text index (ADR-0019): a table derived from the
// documentation files, kept in step with them by digest (kb.Index).

// SyncKB makes the index hold exactly docs: pages gone are removed, pages
// new or whose digest changed are (re)indexed, the others are left as they
// are. It returns how many pages it (re)indexed, then how many it removed.
func (s *Store) SyncKB(ctx context.Context, docs []kb.Doc) (int, int, error) {
	indexed, removed := 0, 0

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback() //nolint:errcheck // no-op after a successful commit

	known, err := indexedDigests(ctx, tx)
	if err != nil {
		return 0, 0, err
	}

	keep := map[string]bool{}

	for _, d := range docs {
		keep[d.Path] = true

		if digest, ok := known[d.Path]; ok && digest == d.Digest {
			continue
		}

		if err := indexKBDoc(ctx, tx, d); err != nil {
			return 0, 0, err
		}

		indexed++
	}

	for path := range known {
		if keep[path] {
			continue
		}

		if err := removeKBDoc(ctx, tx, path); err != nil {
			return 0, 0, err
		}

		removed++
	}

	return indexed, removed, tx.Commit()
}

// indexedDigests is the digest of each page the index holds, by path.
func indexedDigests(ctx context.Context, tx *sql.Tx) (map[string]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT path, digest FROM kb_docs`)
	if err != nil {
		return nil, fmt.Errorf("read the knowledge index: %w", err)
	}
	defer rows.Close()

	known := map[string]string{}

	for rows.Next() {
		var path, digest string
		if err := rows.Scan(&path, &digest); err != nil {
			return nil, err
		}

		known[path] = digest
	}

	return known, rows.Err()
}

func indexKBDoc(ctx context.Context, tx *sql.Tx, d kb.Doc) error {
	if err := removeKBDoc(ctx, tx, d.Path); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `INSERT INTO kb_pages (path, title, body) VALUES (?, ?, ?)`, d.Path, d.Title, d.Body); err != nil {
		return fmt.Errorf("index %s: %w", d.Path, err)
	}

	if _, err := tx.ExecContext(ctx, `INSERT INTO kb_docs (path, digest) VALUES (?, ?)`, d.Path, d.Digest); err != nil {
		return fmt.Errorf("index %s: %w", d.Path, err)
	}

	return nil
}

func removeKBDoc(ctx context.Context, tx *sql.Tx, path string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM kb_pages WHERE path = ?`, path); err != nil {
		return fmt.Errorf("unindex %s: %w", path, err)
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM kb_docs WHERE path = ?`, path); err != nil {
		return fmt.Errorf("unindex %s: %w", path, err)
	}

	return nil
}

// SearchKB finds the pages matching the words of query, best first (FTS5's
// rank, the title weighing more than the body), at most limit. Every word
// must be found; the last one may be the beginning of a word, so that
// results follow typing. FTS5's own syntax is not offered: the words are
// quoted, so no query is ever a syntax error.
func (s *Store) SearchKB(ctx context.Context, query string, limit int) ([]kb.IndexHit, error) {
	match := kbMatch(query)
	if match == "" {
		return []kb.IndexHit{}, nil
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT path, title, snippet(kb_pages, 2, ?, ?, '…', 16)
		FROM kb_pages
		WHERE kb_pages MATCH ?
		ORDER BY bm25(kb_pages, 0, 5, 1)
		LIMIT ?`, kb.MatchStart, kb.MatchEnd, match, limit)
	if err != nil {
		return nil, fmt.Errorf("search the knowledge base: %w", err)
	}
	defer rows.Close()

	hits := []kb.IndexHit{}

	for rows.Next() {
		var h kb.IndexHit
		if err := rows.Scan(&h.Path, &h.Title, &h.Snippet); err != nil {
			return nil, err
		}

		hits = append(hits, h)
	}

	return hits, rows.Err()
}

// kbMatch turns what the human or an agent typed into an FTS5 query: each
// word quoted (a quote inside doubled), all required, the last one a prefix.
func kbMatch(query string) string {
	words := strings.Fields(query)
	for i, w := range words {
		words[i] = `"` + strings.ReplaceAll(w, `"`, `""`) + `"`
	}

	if len(words) == 0 {
		return ""
	}

	words[len(words)-1] += "*"

	return strings.Join(words, " ")
}
