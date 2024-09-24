package tmsqlite

import (
	"context"
	"database/sql"
	"fmt"
)

func migrate(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(
		ctx,
		`CREATE TABLE IF NOT EXISTS migrations(version INTEGER PRIMARY KEY);`,
	); err != nil {
		return fmt.Errorf("error getting initial migrations table: %w", err)
	}

	rows, err := tx.QueryContext(ctx, `SELECT version FROM migrations;`)
	if err != nil {
		return fmt.Errorf("failed to select from migrations table: %w", err)
	}
	defer rows.Close()

	var migrationVersion int
	if rows.Next() {
		if err := rows.Scan(&migrationVersion); err != nil {
			return fmt.Errorf("failed to scan migration version: %w", err)
		}
	}

	if err := migrateFrom(ctx, tx, migrationVersion); err != nil {
		return fmt.Errorf("migration failed: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit failed: %w", err)
	}

	return nil
}

func migrateFrom(ctx context.Context, tx *sql.Tx, version int) error {
	switch version {
	case 0:
		if err := migrateInitial(ctx, tx); err != nil {
			return fmt.Errorf("initial migration: %w", err)
		}
	default:
		return fmt.Errorf("unknown migration version %d", version)
	}

	return nil
}

func migrateInitial(ctx context.Context, tx *sql.Tx) error {
	// Pattern for limiting table to a single row:
	// https://stackoverflow.com/a/33104119
	_, err := tx.ExecContext(
		ctx,
		`CREATE TABLE mirror(
id INTEGER PRIMARY KEY CHECK ( id = 0 ),
vh INTEGER NOT NULL, vr INTEGER NOT NULL,
ch INTEGER NOT NULL, cr INTEGER NOT NULL
);
INSERT INTO mirror VALUES(0, 0, 0, 0, 0);
`,
	)
	return err
}
