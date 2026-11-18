-- The tickets of a finished feature are archived (ADR-0007): out of the
-- Kanban and the queues, still readable from the archives.
ALTER TABLE tickets ADD COLUMN archived INTEGER NOT NULL DEFAULT 0;
