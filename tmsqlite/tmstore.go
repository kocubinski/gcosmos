package tmsqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/rollchains/gordian/gcrypto"
	"github.com/rollchains/gordian/tm/tmconsensus"
	"github.com/rollchains/gordian/tm/tmstore"
)

// TMStore is a single type satisfying all the [tmstore] interfaces.
type TMStore struct {
	// The string "purego" or "cgo" depending on build tags.
	BuildType string

	db *sql.DB

	hs  tmconsensus.HashScheme
	reg *gcrypto.Registry
}

func NewTMStore(
	ctx context.Context,
	dbPath string,
	hashScheme tmconsensus.HashScheme,
	reg *gcrypto.Registry,
) (*TMStore, error) {
	// The driver type comes from the sqlitedriver_*.go file
	// chosen based on build tags.
	db, err := sql.Open(sqliteDriverType, dbPath)
	if err != nil {
		return nil, fmt.Errorf("error opening database: %w", err)
	}

	if err := pragmas(ctx, db); err != nil {
		return nil, err
	}

	if err := migrate(ctx, db); err != nil {
		return nil, err
	}

	return &TMStore{
		BuildType: sqliteBuildType,

		db: db,

		hs:  hashScheme,
		reg: reg,
	}, nil
}

func (s *TMStore) Close() error {
	return s.db.Close()
}

func (s *TMStore) SetNetworkHeightRound(
	ctx context.Context,
	votingHeight uint64, votingRound uint32,
	committingHeight uint64, committingRound uint32,
) error {
	_, err := s.db.ExecContext(
		ctx,
		`UPDATE mirror SET vh = ?, vr = ?, ch = ?, cr = ? WHERE id=0`,
		votingHeight, votingRound, committingHeight, committingRound,
	)
	return err
}

func (s *TMStore) NetworkHeightRound(ctx context.Context) (
	votingHeight uint64, votingRound uint32,
	committingHeight uint64, committingRound uint32,
	err error,
) {
	err = s.db.QueryRowContext(
		ctx,
		`SELECT vh, vr, ch, cr FROM mirror WHERE id=0`,
	).Scan(
		&votingHeight, &votingRound,
		&committingHeight, &committingRound,
	)
	if err == nil &&
		votingHeight == 0 && votingRound == 0 &&
		committingHeight == 0 && committingRound == 0 {
		return 0, 0, 0, 0, tmstore.ErrStoreUninitialized
	}
	return
}

func (s *TMStore) SavePubKeys(ctx context.Context, keys []gcrypto.PubKey) (string, error) {
	hash, err := s.hs.PubKeys(keys)
	if err != nil {
		return "", fmt.Errorf("failed to calculate public key hash: %w", err)
	}

	// First check if the hash is already in the database.
	// (Assuming we aren't racing in core Gordian to add this;
	// if we are racing, then this just needs to move inside the transaction.)
	var count int
	err = s.db.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM validator_pub_key_hashes WHERE hash = ?;`,
		hash,
	).Scan(&count)
	if err != nil {
		return "", fmt.Errorf("failed to check hash existence: %w", err)
	}

	if count > 0 {
		return string(hash), tmstore.PubKeysAlreadyExistError{
			ExistingHash: string(hash),
		}
	}

	// Otherwise the count was zero, so we need to do all the work.
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("failed to open transaction: %w", err)
	}
	defer tx.Rollback()

	// Create the key hash first.
	res, err := tx.ExecContext(
		ctx,
		`INSERT INTO validator_pub_key_hashes(hash) VALUES(?);`,
		hash,
	)
	if err != nil {
		return "", fmt.Errorf("failed to save new hash: %w", err)
	}

	hashID, err := res.LastInsertId()
	if err != nil {
		return "", fmt.Errorf("failed to get last insert ID after saving new hash: %w", err)
	}

	for i, k := range keys {
		b := s.reg.Marshal(k)
		res, err := tx.ExecContext(
			ctx,
			`INSERT OR IGNORE INTO validator_pub_keys(key) VALUES(?);`,
			b,
		)
		if err != nil {
			return "", fmt.Errorf("failed to insert validator public key: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return "", fmt.Errorf("failed to get rows affected after inserting validator public key: %w", err)
		}

		var keyID int64

		if n == 1 {
			keyID, err = res.LastInsertId()
			if err != nil {
				return "", fmt.Errorf("failed to get insert ID after inserting validator public key: %w", err)
			}
		} else {
			// No rows affected, so we need to query the ID.
			if err := tx.QueryRowContext(
				ctx,
				`SELECT id FROM validator_pub_keys WHERE key = ?;`,
				b,
			).Scan(&keyID); err != nil {
				return "", fmt.Errorf("failed to get key ID when querying: %w", err)
			}
		}

		// Now that we have a hash ID, key ID, and ordinal index,
		// we can update the pub key hash entries.
		res, err = tx.ExecContext(
			ctx,
			`INSERT INTO validator_pub_key_hash_entries(hash_id, idx, key_id)
VALUES(?, ?, ?);`,
			hashID, i, keyID,
		)
		if err != nil {
			return "", fmt.Errorf("failed to insert public key hash entry: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("failed to commit saving public key hash: %w", err)
	}

	return string(hash), nil
}

func (s *TMStore) LoadPubKeys(ctx context.Context, hash string) ([]gcrypto.PubKey, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`
SELECT validator_pub_keys.key FROM validator_pub_keys
  JOIN validator_pub_key_hash_entries ON validator_pub_keys.id = validator_pub_key_hash_entries.key_id
  JOIN validator_pub_key_hashes ON validator_pub_key_hash_entries.hash_id = validator_pub_key_hashes.id
  WHERE validator_pub_key_hashes.hash = ? ORDER BY validator_pub_key_hash_entries.idx ASC;
`,
		// This is annoying: if you leave the hash as a string,
		// the string type apparently won't match the blob type,
		// and so you get a misleading empty result.
		[]byte(hash),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query public keys by hash: %w", err)
	}
	defer rows.Close()

	var keys []gcrypto.PubKey
	var encKey []byte
	for rows.Next() {
		encKey = encKey[:0]
		if err := rows.Scan(&encKey); err != nil {
			return nil, fmt.Errorf("failed to scan validator hash row: %w", err)
		}
		key, err := s.reg.Unmarshal(encKey)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal validator key %x: %w", encKey, err)
		}
		keys = append(keys, key)
	}
	if len(keys) == 0 {
		return nil, tmstore.NoPubKeyHashError{Want: hash}
	}

	return keys, nil
}

func (s *TMStore) SaveVotePowers(ctx context.Context, powers []uint64) (string, error) {
	panic("TODO")
}

func (s *TMStore) LoadVotePowers(ctx context.Context, hash string) ([]uint64, error) {
	panic("TODO")
}

func (s *TMStore) LoadValidators(ctx context.Context, keyHash, powHash string) ([]tmconsensus.Validator, error) {
	panic("TODO")
}

func pragmas(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `PRAGMA foreign_keys = ON;`)
	if err != nil {
		return fmt.Errorf("failed to set foreign keys on: %w", err)
	}
	return nil
}
