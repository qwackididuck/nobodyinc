-- Finishing a feature (ADR-0007): the human merges it into its base branch.
-- The merge commit is kept for the record ('' when there was nothing to merge).
ALTER TABLE features ADD COLUMN merge_commit TEXT NOT NULL DEFAULT '';
