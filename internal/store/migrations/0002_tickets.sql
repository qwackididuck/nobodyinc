-- Tickets: units of work owned by a team, optionally assigned to one agent.
CREATE TABLE tickets (
    id                   INTEGER PRIMARY KEY AUTOINCREMENT, -- shown as "T-<id>"
    title                TEXT NOT NULL,
    description          TEXT NOT NULL DEFAULT '',
    acceptance_criteria  TEXT NOT NULL DEFAULT '',
    team                 TEXT NOT NULL,
    assignee             TEXT NOT NULL DEFAULT '',  -- '' while in the team's queue
    status               TEXT NOT NULL,             -- open, in_progress, done, canceled
    created_by           TEXT NOT NULL,
    created_at           TEXT NOT NULL,             -- RFC 3339, UTC
    updated_at           TEXT NOT NULL
);

CREATE INDEX tickets_queue ON tickets (team, status, assignee, id);

-- A ticket cannot be claimed while a ticket it depends on is not done.
CREATE TABLE ticket_dependencies (
    ticket      INTEGER NOT NULL REFERENCES tickets (id),
    depends_on  INTEGER NOT NULL REFERENCES tickets (id),
    PRIMARY KEY (ticket, depends_on)
);

-- What an agent produced for a ticket: a file, a commit, a report, a note.
CREATE TABLE deliverables (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    ticket      INTEGER NOT NULL REFERENCES tickets (id),
    author      TEXT NOT NULL,
    kind        TEXT NOT NULL,
    reference   TEXT NOT NULL DEFAULT '',
    summary     TEXT NOT NULL,
    created_at  TEXT NOT NULL
);

CREATE TABLE ticket_comments (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    ticket      INTEGER NOT NULL REFERENCES tickets (id),
    author      TEXT NOT NULL,
    body        TEXT NOT NULL,
    created_at  TEXT NOT NULL
);

-- Messages can be about a ticket ('' when they are not).
ALTER TABLE messages ADD COLUMN ticket TEXT NOT NULL DEFAULT '';
CREATE INDEX messages_ticket ON messages (ticket);
