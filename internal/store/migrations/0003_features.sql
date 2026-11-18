-- Features: a request ("add the order API") with its own git branch in the
-- project. At most one feature is active; new tickets belong to it.
CREATE TABLE features (
    id           INTEGER PRIMARY KEY AUTOINCREMENT, -- shown as "F-<id>"
    title        TEXT NOT NULL,
    description  TEXT NOT NULL DEFAULT '',
    slug         TEXT NOT NULL,
    base_branch  TEXT NOT NULL,
    branch       TEXT NOT NULL,
    active       INTEGER NOT NULL DEFAULT 0,
    created_by   TEXT NOT NULL,
    created_at   TEXT NOT NULL -- RFC 3339, UTC
);

-- The feature a ticket belongs to ('' for tickets created without one).
ALTER TABLE tickets ADD COLUMN feature TEXT NOT NULL DEFAULT '';
