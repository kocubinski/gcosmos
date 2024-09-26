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
		`CREATE TABLE IF NOT EXISTS migrations(
  id INTEGER PRIMARY KEY CHECK ( id = 0 ),
  version INTEGER
);`,
	); err != nil {
		return fmt.Errorf("error getting initial migrations table: %w", err)
	}

	if _, err := tx.ExecContext(
		ctx,
		`INSERT OR IGNORE INTO migrations(id, version) VALUES (0, 0)`,
	); err != nil {
		return fmt.Errorf("error setting initial migration version: %w", err)
	}

	row := tx.QueryRowContext(ctx, `SELECT version FROM migrations WHERE id=0;`)
	if err != nil {
		return fmt.Errorf("failed to select from migrations table: %w", err)
	}

	var migrationVersion int
	if err := row.Scan(&migrationVersion); err != nil {
		return fmt.Errorf("failed to scan migration version: %w", err)
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
		if err := setMigrationVersion(ctx, tx, 1); err != nil {
			return err
		}
	case 1:
		// Up to date.
		return nil
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
		`
CREATE TABLE mirror(
  id INTEGER PRIMARY KEY CHECK ( id = 0 ),
  vh INTEGER NOT NULL, vr INTEGER NOT NULL,
  ch INTEGER NOT NULL, cr INTEGER NOT NULL
);
INSERT INTO mirror VALUES(0, 0, 0, 0, 0);

CREATE TABLE validator_pub_keys(
  id INTEGER PRIMARY KEY NOT NULL,
  key BLOB NOT NULL UNIQUE
);

CREATE TABLE validator_pub_key_hashes(
  id INTEGER PRIMARY KEY NOT NULL,
  hash BLOB NOT NULL UNIQUE,
  n_keys INTEGER NOT NULL CHECK (n_keys > 0)
);

CREATE TABLE validator_pub_key_hash_entries(
  id INTEGER PRIMARY KEY NOT NULL,
  hash_id INTEGER NOT NULL,
  idx INTEGER NOT NULL,
  key_id INTEGER NOT NULL,
  FOREIGN KEY(key_id) REFERENCES validator_pub_keys(id),
  FOREIGN KEY(hash_id) REFERENCES validator_pub_key_hashes(id),
  UNIQUE (hash_id, idx),
  UNIQUE (hash_id, key_id)
);

CREATE TABLE validator_power_hashes(
  id INTEGER PRIMARY KEY NOT NULL,
  hash BLOB NOT NULL UNIQUE,
  n_powers INTEGER NOT NULL CHECK (n_powers > 0)
);

CREATE TABLE validator_power_hash_entries(
  id INTEGER PRIMARY KEY NOT NULL,
  hash_id INTEGER NOT NULL,
  idx INTEGER NOT NULL,
  power INTEGER NOT NULL,
  FOREIGN KEY(hash_id) REFERENCES validator_power_hashes(id),
  UNIQUE (hash_id, idx)
);

CREATE TABLE finalizations(
  height INTEGER PRIMARY KEY NOT NULL,
	round INTEGER NOT NULL,
	block_hash BLOB NOT NULL,
	validator_pub_key_hash_id INTEGER NOT NULL,
	validator_power_hash_id INTEGER NOT NULL,
	app_state_hash BLOB NOT NULL,
  FOREIGN KEY(validator_pub_key_hash_id) REFERENCES validator_pub_key_hashes(id),
  FOREIGN KEY(validator_power_hash_id) REFERENCES validator_power_hashes(id)
);
`,
	)

	return err
}

func setMigrationVersion(ctx context.Context, tx *sql.Tx, version int) error {
	if _, err := tx.ExecContext(
		ctx,
		`UPDATE migrations SET version = ? WHERE id = 0`,
		version,
	); err != nil {
		return fmt.Errorf("error setting migration version to %d: %w", version, err)
	}

	return nil
}
