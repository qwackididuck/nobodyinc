package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"sort"
)

// migrations holds the schema changes, applied in file name order. Each file
// runs once: SQLite's user_version records how many have been applied. Never
// edit a migration that has shipped; add a new one.
//
//go:embed migrations/*.sql
var migrations embed.FS

// migrate applies the migrations the database has not seen yet, each in its
// own transaction.
func migrate(ctx context.Context, db *sql.DB) error {
	names, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return err
	}

	sort.Strings(names)

	var applied int
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&applied); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}

	for i := applied; i < len(names); i++ {
		script, err := migrations.ReadFile(names[i])
		if err != nil {
			return err
		}

		if err := applyMigration(ctx, db, string(script), i+1); err != nil {
			return fmt.Errorf("apply %s: %w", names[i], err)
		}
	}

	return nil
}

func applyMigration(ctx context.Context, db *sql.DB, script string, version int) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // no-op after a successful commit

	if _, err := tx.ExecContext(ctx, script); err != nil {
		return err
	}
	// PRAGMA does not accept placeholders; version is an int we computed.
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", version)); err != nil {
		return err
	}

	return tx.Commit()
}
