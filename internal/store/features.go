package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/qwackididuck/nobodyinc/internal/work"
)

// InsertFeature stores a new feature and returns it with its ID.
func (s *Store) InsertFeature(ctx context.Context, f work.Feature) (work.Feature, error) {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO features (title, description, acceptance_criteria, slug, base_branch, branch, status, created_by, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		f.Title, f.Description, f.AcceptanceCriteria, f.Slug, f.BaseBranch, f.Branch, f.Status, f.CreatedBy, formatTime(f.CreatedAt))
	if err != nil {
		return work.Feature{}, fmt.Errorf("insert feature: %w", err)
	}

	n, err := res.LastInsertId()
	if err != nil {
		return work.Feature{}, err
	}

	f.ID = work.FormatFeatureID(n)

	return f, nil
}

// GetFeature returns one feature.
func (s *Store) GetFeature(ctx context.Context, id string) (work.Feature, error) {
	n, err := work.ParseFeatureID(id)
	if err != nil {
		return work.Feature{}, err
	}

	f, err := scanFeature(s.db.QueryRowContext(ctx, selectFeatures+` WHERE id = ?`, n))
	if errors.Is(err, sql.ErrNoRows) {
		return work.Feature{}, fmt.Errorf("%w: feature %s", work.ErrNotFound, work.FormatFeatureID(n))
	}

	return f, err
}

// ListFeatures returns every feature, oldest first.
func (s *Store) ListFeatures(ctx context.Context) ([]work.Feature, error) {
	rows, err := s.db.QueryContext(ctx, selectFeatures+` ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list features: %w", err)
	}
	defer rows.Close()

	var features []work.Feature

	for rows.Next() {
		f, err := scanFeature(rows)
		if err != nil {
			return nil, err
		}

		features = append(features, f)
	}

	return features, rows.Err()
}

// UpdateFeature saves the fields that change after a feature is created:
// its branches and status when it starts, the reason it was refused, the
// message that proposed it, the merge commit when it is finished.
func (s *Store) UpdateFeature(ctx context.Context, f work.Feature) error {
	n, err := work.ParseFeatureID(f.ID)
	if err != nil {
		return err
	}

	_, err = s.db.ExecContext(ctx, `
		UPDATE features SET slug = ?, base_branch = ?, branch = ?, status = ?, proposal_message = ?, refusal_reason = ?, merge_commit = ?
		WHERE id = ?`,
		f.Slug, f.BaseBranch, f.Branch, f.Status, f.ProposalMessage, f.RefusalReason, f.MergeCommit, n)
	if err != nil {
		return fmt.Errorf("update feature %s: %w", f.ID, err)
	}

	return nil
}

// ActivateFeature makes one feature the active one.
func (s *Store) ActivateFeature(ctx context.Context, id string) error {
	n, err := work.ParseFeatureID(id)
	if err != nil {
		return err
	}

	if _, err := s.db.ExecContext(ctx, `UPDATE features SET active = (id = ?)`, n); err != nil {
		return fmt.Errorf("activate feature %s: %w", id, err)
	}

	return nil
}

// DeactivateFeature leaves no feature active, if id is the active one.
func (s *Store) DeactivateFeature(ctx context.Context, id string) error {
	n, err := work.ParseFeatureID(id)
	if err != nil {
		return err
	}

	if _, err := s.db.ExecContext(ctx, `UPDATE features SET active = 0 WHERE id = ?`, n); err != nil {
		return fmt.Errorf("deactivate feature %s: %w", id, err)
	}

	return nil
}

const selectFeatures = `
	SELECT id, title, description, acceptance_criteria, slug, base_branch, branch, active, status,
		proposal_message, refusal_reason, merge_commit, created_by, created_at, ` + featureSpent + `
	FROM features`

func scanFeature(row scanner) (work.Feature, error) {
	var (
		f         work.Feature
		n         int64
		createdAt string
	)
	if err := row.Scan(&n, &f.Title, &f.Description, &f.AcceptanceCriteria, &f.Slug, &f.BaseBranch, &f.Branch, &f.Active,
		&f.Status, &f.ProposalMessage, &f.RefusalReason, &f.MergeCommit, &f.CreatedBy, &createdAt, &f.SpentUSD); err != nil {
		return work.Feature{}, err
	}

	f.ID = work.FormatFeatureID(n)

	var err error

	f.CreatedAt, err = parseTime(createdAt)

	return f, err
}
