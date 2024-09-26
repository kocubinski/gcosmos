package tmsqlite

import (
	"context"
	"database/sql"
	"errors"
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
		`SELECT COUNT(hash) FROM validator_pub_key_hashes WHERE hash = ?;`,
		hash,
	).Scan(&count)
	if err != nil {
		return "", fmt.Errorf("failed to check public key hash existence: %w", err)
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

	if err := s.savePubKeysInTx(ctx, tx, hash, keys); err != nil {
		return "", err
	}

	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("failed to commit saving public key hash: %w", err)
	}

	return string(hash), nil
}

// savePubKeysInTx saves the set of public keys belonging to the given hash.
// This method assumes that the hash does not exist in the validator_pub_key_hashes table yet.
func (s *TMStore) savePubKeysInTx(
	ctx context.Context,
	tx *sql.Tx,
	pubKeyHash []byte,
	keys []gcrypto.PubKey,
) error {
	// Create the key hash first.
	res, err := tx.ExecContext(
		ctx,
		`INSERT INTO validator_pub_key_hashes(hash, n_keys) VALUES(?,?);`,
		pubKeyHash, len(keys),
	)
	if err != nil {
		return fmt.Errorf("failed to save new public key hash: %w", err)
	}

	hashID, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("failed to get last insert ID after saving new public key hash: %w", err)
	}

	for i, k := range keys {
		b := s.reg.Marshal(k)
		res, err := tx.ExecContext(
			ctx,
			`INSERT OR IGNORE INTO validator_pub_keys(key) VALUES(?);`,
			b,
		)
		if err != nil {
			return fmt.Errorf("failed to insert validator public key: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("failed to get rows affected after inserting validator public key: %w", err)
		}

		var keyID int64

		if n == 1 {
			keyID, err = res.LastInsertId()
			if err != nil {
				return fmt.Errorf("failed to get insert ID after inserting validator public key: %w", err)
			}
		} else {
			// No rows affected, so we need to query the ID.
			if err := tx.QueryRowContext(
				ctx,
				`SELECT id FROM validator_pub_keys WHERE key = ?;`,
				b,
			).Scan(&keyID); err != nil {
				return fmt.Errorf("failed to get key ID when querying: %w", err)
			}
		}

		// Now that we have a hash ID, key ID, and ordinal index,
		// we can update the pub key hash entries.
		if _, err = tx.ExecContext(
			ctx,
			`INSERT INTO validator_pub_key_hash_entries(hash_id, idx, key_id)
VALUES(?, ?, ?);`,
			hashID, i, keyID,
		); err != nil {
			return fmt.Errorf("failed to insert public key hash entry: %w", err)
		}
	}

	return nil
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
	hash, err := s.hs.VotePowers(powers)
	if err != nil {
		return "", fmt.Errorf("failed to calculate vote power hash: %w", err)
	}

	// First check if the hash is already in the database.
	// (Assuming we aren't racing in core Gordian to add this;
	// if we are racing, then this just needs to move inside the transaction.)
	var count int
	err = s.db.QueryRowContext(
		ctx,
		`SELECT COUNT(hash) FROM validator_power_hashes WHERE hash = ?;`,
		hash,
	).Scan(&count)
	if err != nil {
		return "", fmt.Errorf("failed to check vote power hash existence: %w", err)
	}

	if count > 0 {
		return string(hash), tmstore.VotePowersAlreadyExistError{
			ExistingHash: string(hash),
		}
	}

	// Otherwise the count was zero, so we need to do all the work.
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("failed to open transaction: %w", err)
	}
	defer tx.Rollback()

	if err := s.saveVotePowersInTx(ctx, tx, hash, powers); err != nil {
		return "", err
	}

	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("failed to commit saving vote power hash: %w", err)
	}

	return string(hash), nil
}

func (s *TMStore) saveVotePowersInTx(
	ctx context.Context,
	tx *sql.Tx,
	votePowerHash []byte,
	powers []uint64,
) error {
	// Create the key hash first.
	res, err := tx.ExecContext(
		ctx,
		`INSERT INTO validator_power_hashes(hash, n_powers) VALUES(?,?);`,
		votePowerHash, len(powers),
	)
	if err != nil {
		return fmt.Errorf("failed to save new vote power hash: %w", err)
	}

	hashID, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("failed to get last insert ID after saving new vote power hash: %w", err)
	}

	for i, power := range powers {
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO validator_power_hash_entries(hash_id, idx, power)
VALUES(?, ?, ?);`,
			hashID, i, power,
		); err != nil {
			return fmt.Errorf("failed to insert vote power hash entry: %w", err)
		}
	}

	return nil
}

func (s *TMStore) LoadVotePowers(ctx context.Context, hash string) ([]uint64, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`
SELECT validator_power_hash_entries.power FROM validator_power_hash_entries
  JOIN validator_power_hashes ON validator_power_hashes.id = validator_power_hash_entries.hash_id
  WHERE validator_power_hashes.hash = ? ORDER BY validator_power_hash_entries.idx ASC;`,
		// This is annoying: if you leave the hash as a string,
		// the string type apparently won't match the blob type,
		// and so you get a misleading empty result.
		[]byte(hash),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query vote powers by hash: %w", err)
	}
	defer rows.Close()

	var powers []uint64
	for rows.Next() {
		var pow uint64
		if err := rows.Scan(&pow); err != nil {
			return nil, fmt.Errorf("failed to scan validator power row: %w", err)
		}
		powers = append(powers, pow)
	}

	if len(powers) == 0 {
		return nil, tmstore.NoVotePowerHashError{Want: hash}
	}

	return powers, nil
}

func (s *TMStore) LoadValidators(ctx context.Context, keyHash, powHash string) ([]tmconsensus.Validator, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT keys.key, powers.power FROM
(
  SELECT validator_pub_keys.key, validator_pub_key_hash_entries.idx FROM validator_pub_keys
    JOIN validator_pub_key_hash_entries ON validator_pub_keys.id = validator_pub_key_hash_entries.key_id
    JOIN validator_pub_key_hashes ON validator_pub_key_hash_entries.hash_id = validator_pub_key_hashes.id
    WHERE validator_pub_key_hashes.hash = ? ORDER BY validator_pub_key_hash_entries.idx ASC
) as keys
JOIN
(
  SELECT validator_power_hash_entries.power, validator_power_hash_entries.idx FROM validator_power_hash_entries
    JOIN validator_power_hashes ON validator_power_hashes.id = validator_power_hash_entries.hash_id
    WHERE validator_power_hashes.hash = ? ORDER BY validator_power_hash_entries.idx ASC
) as powers ON keys.idx = powers.idx
`,
		[]byte(keyHash), []byte(powHash),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query for validators: %w", err)
	}
	defer rows.Close()

	var vals []tmconsensus.Validator
	var encKey []byte
	for rows.Next() {
		encKey = encKey[:0]
		var pow uint64
		if err := rows.Scan(&encKey, &pow); err != nil {
			return nil, fmt.Errorf("failed to scan validator row: %w", err)
		}
		key, err := s.reg.Unmarshal(encKey)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal validator key %x: %w", encKey, err)
		}

		vals = append(vals, tmconsensus.Validator{
			PubKey: key,
			Power:  pow,
		})
	}
	rows.Close()

	if len(vals) > 0 {
		// One more check that the key count is correct.
		// There is probably some way to push this into the prior query.
		row := s.db.QueryRowContext(
			ctx,
			`SELECT keys.n_keys, powers.n_powers FROM
(SELECT n_keys FROM validator_pub_key_hashes WHERE hash = ?) AS keys,
(SELECT n_powers FROM validator_power_hashes WHERE hash = ?) AS powers`,
			[]byte(keyHash), []byte(powHash),
		)
		var nKeys, nPows int
		if err := row.Scan(&nKeys, &nPows); err != nil {
			return nil, fmt.Errorf("failed to scan count results: %w", err)
		}
		if nKeys != nPows {
			return nil, tmstore.PubKeyPowerCountMismatchError{
				NPubKeys:   nKeys,
				NVotePower: nPows,
			}
		}
		if nKeys != len(vals) {
			panic(fmt.Errorf(
				"BUG: expected %d vals, but queried %d", nKeys, len(vals),
			))
		}

		return vals, nil
	}

	// We are missing at least one hash.
	// Run another query to determine which we are missing.
	row := s.db.QueryRowContext(
		ctx,
		`SELECT * FROM
(SELECT COUNT(hash) FROM validator_pub_key_hashes WHERE hash = ?),
(SELECT COUNT(hash) FROM validator_power_hashes WHERE hash = ?)`,
		[]byte(keyHash), []byte(powHash),
	)

	var nKeys, nPows int
	if err := row.Scan(&nKeys, &nPows); err != nil {
		return nil, fmt.Errorf("failed to scan hash counts: %w", err)
	}

	var hashErr error
	if nKeys == 0 {
		hashErr = tmstore.NoPubKeyHashError{Want: keyHash}
	}
	if nPows == 0 {
		hashErr = errors.Join(hashErr, tmstore.NoVotePowerHashError{Want: powHash})
	}

	if hashErr == nil {
		panic(fmt.Errorf(
			"DATA RACE: hashes (pub keys: %x; power: %x) were missing and then appeared",
			keyHash, powHash,
		))
	}
	return nil, hashErr
}

func (s *TMStore) SaveFinalization(
	ctx context.Context,
	height uint64, round uint32,
	blockHash string,
	valSet tmconsensus.ValidatorSet,
	appStateHash string,
) error {
	// We're going to need to touch a couple tables, so do this all within a transaction.
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to open transaction: %w", err)
	}
	defer tx.Rollback()

	// Check if the validator hashes already exist.
	row := tx.QueryRowContext(
		ctx,
		`SELECT * FROM
(SELECT COUNT(hash) FROM validator_pub_key_hashes WHERE hash = ?),
(SELECT COUNT(hash) FROM validator_power_hashes WHERE hash = ?)`,
		valSet.PubKeyHash, valSet.VotePowerHash,
	)

	var nKeys, nPows int
	if err := row.Scan(&nKeys, &nPows); err != nil {
		return fmt.Errorf("failed to scan hash counts: %w", err)
	}

	if nKeys == 0 {
		if err := s.savePubKeysInTx(ctx, tx, valSet.PubKeyHash, tmconsensus.ValidatorsToPubKeys(valSet.Validators)); err != nil {
			return fmt.Errorf("failed to save new public key hash: %w", err)
		}
	}

	if nPows == 0 {
		if err := s.saveVotePowersInTx(ctx, tx, valSet.VotePowerHash, tmconsensus.ValidatorsToVotePowers(valSet.Validators)); err != nil {
			return fmt.Errorf("failed to save new vote power hash: %w", err)
		}
	}

	// The power hashes exist.
	// Now we should be able to write the finalization record.
	res, err := tx.ExecContext(
		ctx,
		`INSERT INTO
finalizations(height, round, block_hash, validator_pub_key_id, validator_power_id, app_state_hash)
SELECT ?, ?, ?, keys.id, powers.id, ? FROM
(SELECT id FROM validator_pub_key_hashes WHERE hash = ?) AS keys,
(SELECT id FROM validator_power_hashes WHERE hash = ?) AS powers
`,
		height, round, []byte(blockHash), []byte(appStateHash), valSet.PubKeyHash, valSet.VotePowerHash,
	)
	if err != nil {
		if isPrimaryKeyConstraintError(err) {
			return tmstore.FinalizationOverwriteError{Height: height}
		}
		return fmt.Errorf("failed to write finalization: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check rows affected: %w", err)
	}
	if n != 1 {
		return fmt.Errorf("expected 1 affected row, got %d", n)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

func (s *TMStore) LoadFinalizationByHeight(ctx context.Context, height uint64) (
	round uint32,
	blockHash string,
	valSet tmconsensus.ValidatorSet,
	appStateHash string,
	err error,
) {
	// This two-stage query seems like a prime candidate for NextResultSet,
	// but it appears neither SQLite driver supports it.
	// So use a read-only transaction instead.
	// (Which is apparently not even enforced: see
	// https://github.com/mattn/go-sqlite3/issues/685 and
	// https://gitlab.com/cznic/sqlite/-/issues/193 .)
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		err = fmt.Errorf("failed to begin read-only transaction: %w", err)
		return
	}
	defer tx.Rollback()

	// TODO: this query could also retrieve the n_validators and n_powers in order to
	// validate the hashes refer to the same number of powers and validators.
	var blockHashBytes, appStateHashBytes []byte
	if err = tx.QueryRowContext(
		ctx,
		`SELECT
  f.round,
  f.block_hash, f.app_state_hash,
  keys.hash, powers.hash
FROM finalizations AS f
JOIN validator_pub_key_hashes AS keys ON keys.id = f.validator_pub_key_id
JOIN validator_power_hashes AS powers ON powers.id = f.validator_power_id
WHERE f.height = ?`,
		height,
	).Scan(
		&round,
		&blockHashBytes, &appStateHashBytes,
		&valSet.PubKeyHash, &valSet.VotePowerHash,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// The compliance tests require HeightUnknownError when no finalization matches the height.
			err = tmconsensus.HeightUnknownError{Want: height}
			return
		}

		// Otherwise, just wrap that error.
		err = fmt.Errorf("failed to scan finalization primitive data: %w", err)
		return
	}
	blockHash = string(blockHashBytes)
	appStateHash = string(appStateHashBytes)

	// Then extract the validators.
	rows, err := tx.QueryContext(
		ctx,
		`SELECT keys.key, powers.power FROM
(
  SELECT validator_pub_keys.key, validator_pub_key_hash_entries.idx FROM validator_pub_keys
    JOIN validator_pub_key_hash_entries ON validator_pub_keys.id = validator_pub_key_hash_entries.key_id
    JOIN validator_pub_key_hashes ON validator_pub_key_hash_entries.hash_id = validator_pub_key_hashes.id
    JOIN finalizations ON finalizations.validator_pub_key_id = validator_pub_key_hash_entries.hash_id
    WHERE finalizations.height = ?1 ORDER BY validator_pub_key_hash_entries.idx ASC
) as keys
JOIN
(
  SELECT validator_power_hash_entries.power, validator_power_hash_entries.idx FROM validator_power_hash_entries
    JOIN validator_power_hashes ON validator_power_hashes.id = validator_power_hash_entries.hash_id
    JOIN finalizations ON finalizations.validator_power_id = validator_power_hash_entries.hash_id
    WHERE finalizations.height = ?1 ORDER BY validator_power_hash_entries.idx ASC
) as powers ON keys.idx = powers.idx
		`,
		height,
	)
	if err != nil {
		err = fmt.Errorf("failed to query validators for finalization: %w", err)
		return
	}
	defer rows.Close()

	var encKey []byte
	for rows.Next() {
		encKey = encKey[:0]
		var pow uint64
		if err = rows.Scan(&encKey, &pow); err != nil {
			err = fmt.Errorf("failed to scan validator row: %w", err)
			return
		}

		var key gcrypto.PubKey
		key, err = s.reg.Unmarshal(encKey)
		if err != nil {
			err = fmt.Errorf("failed to unmarshal validator key %x: %w", encKey, err)
			return
		}

		valSet.Validators = append(valSet.Validators, tmconsensus.Validator{
			PubKey: key,
			Power:  pow,
		})
	}

	return
}

func pragmas(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `PRAGMA foreign_keys = ON;`)
	if err != nil {
		return fmt.Errorf("failed to set foreign keys on: %w", err)
	}
	return nil
}
