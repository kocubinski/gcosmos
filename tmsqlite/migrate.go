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
  id INTEGER PRIMARY KEY CHECK (id = 0),
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
		// The mirror table tracks the mirror's perceived voting and committing heights and rounds.
		// We use the trick of forcing the primary key to zero,
		// to ensure all updates are manual and affect the single row.
		`
CREATE TABLE mirror(
  id INTEGER PRIMARY KEY CHECK (id = 0),
  vh INTEGER NOT NULL, vr INTEGER NOT NULL,
  ch INTEGER NOT NULL, cr INTEGER NOT NULL
);
INSERT INTO mirror VALUES(0, 0, 0, 0, 0);`+

			// Unique validator public keys.
			// The key field is encoded through a [gcrypto.Registry].
			`
CREATE TABLE validator_pub_keys(
  id INTEGER PRIMARY KEY NOT NULL,
  key BLOB NOT NULL UNIQUE
);`+

			// Hashes of collections of validator public keys.
			`
CREATE TABLE validator_pub_key_hashes(
  id INTEGER PRIMARY KEY NOT NULL,
  hash BLOB NOT NULL UNIQUE,
  n_keys INTEGER NOT NULL CHECK (n_keys > 0)
);`+

			// Many-to-many relationship of the validator_pub_key_hashes table
			// and individual keys.
			`
CREATE TABLE validator_pub_key_hash_entries(
  id INTEGER PRIMARY KEY NOT NULL,
  hash_id INTEGER NOT NULL,
  idx INTEGER NOT NULL,
  key_id INTEGER NOT NULL,
  FOREIGN KEY(key_id) REFERENCES validator_pub_keys(id),
  FOREIGN KEY(hash_id) REFERENCES validator_pub_key_hashes(id),
  UNIQUE (hash_id, idx),
  UNIQUE (hash_id, key_id)
);`+

			// View to simplify querying public keys by hash.
			`
CREATE VIEW validator_pub_keys_for_hash(
  hash_id, hash,
  idx, key
) AS
SELECT
  validator_pub_key_hashes.id, validator_pub_key_hashes.hash,
  idx, key FROM validator_pub_keys
  JOIN validator_pub_key_hash_entries ON validator_pub_keys.id = validator_pub_key_hash_entries.key_id
  JOIN validator_pub_key_hashes ON validator_pub_key_hash_entries.hash_id = validator_pub_key_hashes.id;`+

			// Hashes of collections of validator powers.
			`
CREATE TABLE validator_power_hashes(
  id INTEGER PRIMARY KEY NOT NULL,
  hash BLOB NOT NULL UNIQUE,
  n_powers INTEGER NOT NULL CHECK (n_powers > 0)
);`+

			// Many-to-many relationship between validator_power_hashes and power values.
			`
CREATE TABLE validator_power_hash_entries(
  id INTEGER PRIMARY KEY NOT NULL,
  hash_id INTEGER NOT NULL,
  idx INTEGER NOT NULL,
  power INTEGER NOT NULL,
  FOREIGN KEY(hash_id) REFERENCES validator_power_hashes(id),
  UNIQUE (hash_id, idx)
);`+

			// View to simplify querying powers by hash.
			`
CREATE VIEW validator_powers_for_hash(
  hash_id, hash,
  idx, power
) AS
SELECT
  validator_power_hashes.id, validator_power_hashes.hash,
  idx, power FROM validator_power_hash_entries
  JOIN validator_power_hashes ON validator_power_hashes.id = validator_power_hash_entries.hash_id;`+

			// Finalizations as reported by the state machine.
			`
CREATE TABLE finalizations(
  height INTEGER PRIMARY KEY NOT NULL,
  round INTEGER NOT NULL,
  block_hash BLOB NOT NULL,
  validator_pub_key_hash_id INTEGER NOT NULL,
  validator_power_hash_id INTEGER NOT NULL,
  app_state_hash BLOB NOT NULL,
  FOREIGN KEY(validator_pub_key_hash_id) REFERENCES validator_pub_key_hashes(id),
  FOREIGN KEY(validator_power_hash_id) REFERENCES validator_power_hashes(id)
);`+

			// A somewhat generic header table.
			// We allow both prev_block_hash and prev_commit_proof_id
			// to be null for the initial header.
			`
CREATE TABLE headers(
  id INTEGER PRIMARY KEY NOT NULL,
  hash BLOB NOT NULL,
  prev_block_hash BLOB,
  height INTEGER NOT NULL CHECK (height >= 0),
  prev_commit_proof_id INTEGER,
  validators_pub_key_hash_id INTEGER NOT NULL,
  validators_power_hash_id INTEGER NOT NULL,
  next_validators_pub_key_hash_id INTEGER NOT NULL,
  next_validators_power_hash_id INTEGER NOT NULL,
  data_id BLOB NOT NULL,
  prev_app_state_hash BLOB NOT NULL,
  user_annotations BLOB,
  driver_annotations BLOB,
  committed INTEGER NOT NULL CHECK (committed = 0 OR committed = 1),
  FOREIGN KEY(prev_commit_proof_id) REFERENCES prev_commit_proofs(id),
  FOREIGN KEY(validators_pub_key_hash_id) REFERENCES validator_pub_key_hashes(id),
  FOREIGN KEY(validators_power_hash_id) REFERENCES validator_power_hashes(id),
  FOREIGN KEY(next_validators_pub_key_hash_id) REFERENCES validator_pub_key_hashes(id),
  FOREIGN KEY(next_validators_power_hash_id) REFERENCES validator_power_hashes(id)
);`+

			// Table specifically for headers' previous commit proofs.
			`
CREATE TABLE prev_commit_proofs(
  id INTEGER PRIMARY KEY NOT NULL,
  round INTEGER NOT NULL CHECK (round >= 0),
  validators_pub_key_hash_id INTEGER NOT NULL,
  FOREIGN KEY(validators_pub_key_hash_id) REFERENCES validator_pub_key_hashes(id)
);`+

			// Which block hashes are present in a previous commit proof.
			// Null block hash indicates a vote for nil.
			`
CREATE TABLE prev_commit_proof_voted_blocks(
  id INTEGER PRIMARY KEY NOT NULL,
  prev_commit_proof_id INTEGER NOT NULL,
  block_hash BLOB,
  FOREIGN KEY(prev_commit_proof_id) REFERENCES prev_commit_proofs(id)
);`+

			// Many-to-many relationship indicating
			// which signatures are present for a particular block in a previous commit proof.
			`
CREATE TABLE prev_commit_proof_block_signatures(
  id INTEGER PRIMARY KEY NOT NULL,
  prev_commit_proof_voted_block_id INTEGER NOT NULL,
  sparse_signature_id INTEGER NOT NULL,
  FOREIGN KEY(prev_commit_proof_voted_block_id) REFERENCES prev_commit_proof_voted_blocks(id),
  FOREIGN KEY(sparse_signature_id) REFERENCES sparse_signatures(id)
);`+

			// Pairings of key IDs (whose format is dependent on the signature scheme)
			// and raw signature bytes.
			`
CREATE TABLE sparse_signatures(
  id INTEGER PRIMARY KEY NOT NULL,
  key_id BLOB NOT NULL,
  signature BLOB NOT NULL
);`+

			// A somewhat generic proposed header table.
			// A proposed header is just a reference to a header,
			// plus some extra metadata.
			`
CREATE TABLE proposed_headers(
  id INTEGER PRIMARY KEY NOT NULL,
  header_id INTEGER NOT NULL,
  round INTEGER NOT NULL CHECK (round >= 0),
  proposer_pub_key_id INTEGER NOT NULL,
  user_annotations BLOB,
  driver_annotations BLOB,
  FOREIGN KEY(header_id) REFERENCES headers(id),
  FOREIGN KEY(proposer_pub_key_id) REFERENCES validator_pub_keys(id)
); `+

			// Consistent end of long concatenated literal, to minimize diffs.
			"",
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
