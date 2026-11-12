-- Messages exchanged between agents (and the human).
-- A message is "pending" in the recipient's tray until delivered_at is set.
CREATE TABLE IF NOT EXISTS messages (
    id            TEXT PRIMARY KEY,
    conversation  TEXT NOT NULL,
    in_reply_to   TEXT NOT NULL DEFAULT '',
    sender        TEXT NOT NULL,
    recipient     TEXT NOT NULL,
    act           TEXT NOT NULL,
    priority      TEXT NOT NULL,
    subject       TEXT NOT NULL,
    body          TEXT NOT NULL,
    hops          INTEGER NOT NULL,
    sent_at       TEXT NOT NULL, -- RFC 3339, UTC
    delivered_at  TEXT           -- NULL while pending
);

CREATE INDEX IF NOT EXISTS messages_tray
    ON messages (recipient, priority, delivered_at, sent_at);
