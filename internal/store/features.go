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
		INSERT INTO features (title, description, slug, base_branch, branch, created_by, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		f.Title, f.Description, f.Slug, f.BaseBranch, f.Branch, f.CreatedBy, formatTime(f.CreatedAt))
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

const selectFeatures = `
	SELECT id, title, description, slug, base_branch, branch, active, created_by, created_at
	FROM features`

func scanFeature(row scanner) (work.Feature, error) {
	var (
		f         work.Feature
		n         int64
		createdAt string
	)
	if err := row.Scan(&n, &f.Title, &f.Description, &f.Slug, &f.BaseBranch, &f.Branch, &f.Active,
		&f.CreatedBy, &createdAt); err != nil {
		return work.Feature{}, err
	}

	f.ID = work.FormatFeatureID(n)

	var err error

	f.CreatedAt, err = parseTime(createdAt)

	return f, err
}
