-- The knowledge base's full-text index (ADR-0019, internal/kb): derived from
-- the project's documentation files, rebuilt from them at will, never a
-- source of truth. kb_docs remembers which content of each page is indexed,
-- so that only changed pages are indexed again.
CREATE TABLE kb_docs (
    path   TEXT PRIMARY KEY, -- the page's path in the knowledge folder: "adr/0003.md"
    digest TEXT NOT NULL     -- SHA-256 of the indexed content
);

-- Accents are ignored: "decision" finds "décision".
CREATE VIRTUAL TABLE kb_pages USING fts5(
    path UNINDEXED,
    title,
    body,
    tokenize = 'unicode61 remove_diacritics 2'
);
