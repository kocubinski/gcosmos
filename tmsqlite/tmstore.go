package tmsqlite

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"

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

	if _, err := s.createPubKeysInTx(ctx, tx, hash, keys); err != nil {
		return "", err
	}

	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("failed to commit saving public key hash: %w", err)
	}

	return string(hash), nil
}

// createPubKeysInTx saves the set of public keys belonging to the given hash.
// This method assumes that the hash does not exist in the validator_pub_key_hashes table yet.
func (s *TMStore) createPubKeysInTx(
	ctx context.Context,
	tx *sql.Tx,
	pubKeyHash []byte,
	keys []gcrypto.PubKey,
) (hashID int64, err error) {
	// Create the key hash first.
	res, err := tx.ExecContext(
		ctx,
		`INSERT INTO validator_pub_key_hashes(hash, n_keys) VALUES(?,?);`,
		pubKeyHash, len(keys),
	)
	if err != nil {
		return -1, fmt.Errorf("failed to save new public key hash: %w", err)
	}

	hashID, err = res.LastInsertId()
	if err != nil {
		return -1, fmt.Errorf("failed to get last insert ID after saving new public key hash: %w", err)
	}

	args := make([]any, 0, 3*len(keys))
	for i, k := range keys {
		b := s.reg.Marshal(k)
		res, err := tx.ExecContext(
			ctx,
			`INSERT OR IGNORE INTO validator_pub_keys(key) VALUES(?);`,
			b,
		)
		if err != nil {
			return -1, fmt.Errorf("failed to insert validator public key: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return -1, fmt.Errorf("failed to get rows affected after inserting validator public key: %w", err)
		}

		var keyID int64

		if n == 1 {
			keyID, err = res.LastInsertId()
			if err != nil {
				return -1, fmt.Errorf("failed to get insert ID after inserting validator public key: %w", err)
			}
		} else {
			// No rows affected, so we need to query the ID.
			if err := tx.QueryRowContext(
				ctx,
				`SELECT id FROM validator_pub_keys WHERE key = ?;`,
				b,
			).Scan(&keyID); err != nil {
				return -1, fmt.Errorf("failed to get key ID when querying: %w", err)
			}
		}

		// Now that we have a hash ID, key ID, and ordinal index,
		// we can update args we will insert into the pub key hash entries.
		args = append(args, hashID, i, keyID)
	}

	q := `INSERT INTO validator_pub_key_hash_entries(hash_id, idx, key_id) VALUES (?,?,?)` +
		strings.Repeat(", (?,?,?)", len(keys)-1)
	if _, err := tx.ExecContext(ctx, q, args...); err != nil {
		return -1, fmt.Errorf("failed to insert public key hash entry: %w", err)
	}

	return hashID, nil
}

func (s *TMStore) LoadPubKeys(ctx context.Context, hash string) ([]gcrypto.PubKey, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT key FROM validator_pub_keys_for_hash WHERE hash = ? ORDER BY idx ASC`,
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

	if _, err := s.createVotePowersInTx(ctx, tx, hash, powers); err != nil {
		return "", err
	}

	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("failed to commit saving vote power hash: %w", err)
	}

	return string(hash), nil
}

// createVotePowersInTx saves the set of vote powers belonging to the given hash.
// This method assumes that the hash does not exist in the validator_vote_power_hashes table yet.
func (s *TMStore) createVotePowersInTx(
	ctx context.Context,
	tx *sql.Tx,
	votePowerHash []byte,
	powers []uint64,
) (hashID int64, err error) {
	// Create the key hash first.
	res, err := tx.ExecContext(
		ctx,
		`INSERT INTO validator_power_hashes(hash, n_powers) VALUES(?,?);`,
		votePowerHash, len(powers),
	)
	if err != nil {
		return -1, fmt.Errorf("failed to save new vote power hash: %w", err)
	}

	hashID, err = res.LastInsertId()
	if err != nil {
		return -1, fmt.Errorf("failed to get last insert ID after saving new vote power hash: %w", err)
	}

	args := make([]any, 0, 3*len(powers))
	// TODO: condense this into one larger insert.
	for i, power := range powers {
		args = append(args, hashID, i, power)
	}

	q := `INSERT INTO validator_power_hash_entries(hash_id, idx, power)
VALUES (?,?,?)` +
		strings.Repeat(", (?,?,?)", len(powers)-1)
	if _, err := tx.ExecContext(ctx, q, args...); err != nil {
		return -1, fmt.Errorf("failed to insert vote power hash entry: %w", err)
	}

	return hashID, nil
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
  SELECT key, idx FROM validator_pub_keys_for_hash WHERE hash = ? ORDER BY idx ASC
) as keys
JOIN
(
  SELECT power, idx FROM validator_powers_for_hash WHERE hash = ? ORDER BY idx ASC
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
		if _, err := s.createPubKeysInTx(ctx, tx, valSet.PubKeyHash, tmconsensus.ValidatorsToPubKeys(valSet.Validators)); err != nil {
			return fmt.Errorf("failed to save new public key hash: %w", err)
		}
	}

	if nPows == 0 {
		if _, err := s.createVotePowersInTx(ctx, tx, valSet.VotePowerHash, tmconsensus.ValidatorsToVotePowers(valSet.Validators)); err != nil {
			return fmt.Errorf("failed to save new vote power hash: %w", err)
		}
	}

	// The power hashes exist.
	// Now we should be able to write the finalization record.
	res, err := tx.ExecContext(
		ctx,
		`INSERT INTO
finalizations(height, round, block_hash, validator_pub_key_hash_id, validator_power_hash_id, app_state_hash)
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
	var nKeys, nPowers int
	if err = tx.QueryRowContext(
		ctx,
		`SELECT
  f.round,
  f.block_hash, f.app_state_hash,
  key_hashes.hash, power_hashes.hash,
  key_hashes.n_keys, power_hashes.n_powers
FROM finalizations AS f
JOIN validator_pub_key_hashes AS key_hashes ON key_hashes.id = f.validator_pub_key_hash_id
JOIN validator_power_hashes AS power_hashes ON power_hashes.id = f.validator_power_hash_id
WHERE f.height = ?`,
		height,
	).Scan(
		&round,
		&blockHashBytes, &appStateHashBytes,
		&valSet.PubKeyHash, &valSet.VotePowerHash,
		&nKeys, &nPowers,
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

	if nKeys != nPowers {
		// We already know they are > 0 due to checks on the respective tables.
		panic(fmt.Errorf(
			"DATABASE CORRUPTION: finalization for height %d references pubkey hash %x (with %d keys) and power hash %x (with %d powers)",
			height, valSet.PubKeyHash, nKeys, valSet.VotePowerHash, nPowers,
		))
	}

	blockHash = string(blockHashBytes)
	appStateHash = string(appStateHashBytes)

	// Then extract the validators.
	rows, err := tx.QueryContext(
		ctx,
		// It seems like there should be a view for this,
		// but maybe not because we are joining on the finalizations table?
		`SELECT keys.key, powers.power FROM
(
  SELECT key, idx FROM validator_pub_keys_for_hash
    JOIN finalizations ON finalizations.validator_pub_key_hash_id = validator_pub_keys_for_hash.hash_id
    WHERE finalizations.height = ?1 ORDER BY idx ASC
) as keys
JOIN
(
  SELECT power, idx FROM validator_powers_for_hash
    JOIN finalizations ON finalizations.validator_power_hash_id = validator_powers_for_hash.hash_id
    WHERE finalizations.height = ?1 ORDER BY idx ASC
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

// selectOrInsertPubKeysByHash looks up the database ID of the given pubKeyHash.
// If the record exists, the ID is returned.
// If the record does not exist, a new record is created
// for the given hash and the given public keys in the provided order.
func (s *TMStore) selectOrInsertPubKeysByHash(
	ctx context.Context,
	tx *sql.Tx,
	pubKeyHash []byte,
	pubKeys []gcrypto.PubKey,
) (hashID int64, err error) {
	row := tx.QueryRowContext(
		ctx,
		`SELECT id FROM validator_pub_key_hashes WHERE hash = ?`,
		pubKeyHash,
	)
	err = row.Scan(&hashID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return s.createPubKeysInTx(ctx, tx, pubKeyHash, pubKeys)
		}

		// Not sql.ErrNoRows, so nothing else we can do.
		return -1, fmt.Errorf("failed to scan pub key hash ID: %w", err)
	}

	return hashID, nil
}

// selectOrInsertVotePowersByHash looks up the database ID of the given votePowerHash.
// If the record exists, the ID is returned.
// If the record does not exist, a new record is created
// for the given hash and the given powers in the provided order.
func (s *TMStore) selectOrInsertVotePowersByHash(
	ctx context.Context,
	tx *sql.Tx,
	votePowerHash []byte,
	votePowers []uint64,
) (hashID int64, err error) {
	row := tx.QueryRowContext(
		ctx,
		`SELECT id FROM validator_power_hashes WHERE hash = ?`,
		votePowerHash,
	)
	err = row.Scan(&hashID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return s.createVotePowersInTx(ctx, tx, votePowerHash, votePowers)
		}

		// Not sql.ErrNoRows, so nothing else we can do.
		return -1, fmt.Errorf("failed to scan vote power hash ID: %w", err)
	}

	return hashID, nil
}

func (s *TMStore) SaveHeader(ctx context.Context, ch tmconsensus.CommittedHeader) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	h := ch.Header

	pubKeyHashID, err := s.selectOrInsertPubKeysByHash(
		ctx, tx,
		h.ValidatorSet.PubKeyHash,
		tmconsensus.ValidatorsToPubKeys(h.ValidatorSet.Validators),
	)
	if err != nil {
		return fmt.Errorf("failed to save validator public keys: %w", err)
	}

	votePowerHashID, err := s.selectOrInsertVotePowersByHash(
		ctx, tx,
		h.ValidatorSet.VotePowerHash,
		tmconsensus.ValidatorsToVotePowers(h.ValidatorSet.Validators),
	)
	if err != nil {
		return fmt.Errorf("failed to save validator vote powers: %w", err)
	}

	nextPubKeyHashID := pubKeyHashID
	if !bytes.Equal(h.ValidatorSet.PubKeyHash, h.NextValidatorSet.PubKeyHash) {
		nextPubKeyHashID, err = s.selectOrInsertPubKeysByHash(
			ctx, tx,
			h.NextValidatorSet.PubKeyHash,
			tmconsensus.ValidatorsToPubKeys(h.NextValidatorSet.Validators),
		)
		if err != nil {
			return fmt.Errorf("failed to save next validator public keys: %w", err)
		}
	}

	nextVotePowerHashID := votePowerHashID
	if !bytes.Equal(h.ValidatorSet.VotePowerHash, h.NextValidatorSet.VotePowerHash) {
		nextVotePowerHashID, err = s.selectOrInsertVotePowersByHash(
			ctx, tx,
			h.NextValidatorSet.VotePowerHash,
			tmconsensus.ValidatorsToVotePowers(h.NextValidatorSet.Validators),
		)
		if err != nil {
			return fmt.Errorf("failed to save next validator public keys: %w", err)
		}
	}

	// Insert the previous commit proof, if one exists.
	var prevCommitProofID sql.NullInt64
	if h.PrevCommitProof.PubKeyHash != "" {
		res, err := tx.ExecContext(
			ctx,
			`INSERT INTO commit_proofs(round, validators_pub_key_hash_id) VALUES
(?, (SELECT id FROM validator_pub_key_hashes WHERE hash = ?))`,
			h.PrevCommitProof.Round, []byte(h.PrevCommitProof.PubKeyHash),
		)
		if err != nil {
			return fmt.Errorf("failed to create prev commit proof row: %w", err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			return fmt.Errorf("failed to get ID of prev commit proof's row: %w", err)
		}
		prevCommitProofID = sql.NullInt64{Int64: id, Valid: true}

		if err := s.saveCommitProofs(ctx, tx, id, h.PrevCommitProof.Proofs); err != nil {
			return fmt.Errorf("failed to save previous commit proofs: %w", err)
		}
	}

	res, err := tx.ExecContext(
		ctx,
		`INSERT INTO headers(
hash, prev_block_hash, height,
validators_pub_key_hash_id,
validators_power_hash_id,
next_validators_pub_key_hash_id,
next_validators_power_hash_id,
prev_commit_proof_id,
data_id, prev_app_state_hash,
user_annotations, driver_annotations,
committed
) VALUES (
$hash, $prev_block_hash, $height,
$pub_key_hash_id,
$vote_power_hash_id,
$next_pub_key_hash_id,
$next_vote_power_hash_id,
$prev_commit_proof_id,
$data_id, $prev_app_state_hash,
$user_annotations, $driver_annotations,
1)`,
		sql.Named("hash", h.Hash), sql.Named("prev_block_hash", h.PrevBlockHash), sql.Named("height", h.Height),
		sql.Named("pub_key_hash_id", pubKeyHashID),
		sql.Named("prev_commit_proof_id", prevCommitProofID),
		sql.Named("data_id", h.DataID), sql.Named("prev_app_state_hash", h.PrevAppStateHash),
		sql.Named("user_annotations", h.Annotations.User), sql.Named("driver_annotations", h.Annotations.Driver),
		sql.Named("pub_key_hash_id", pubKeyHashID),
		sql.Named("vote_power_hash_id", votePowerHashID),
		sql.Named("next_pub_key_hash_id", nextPubKeyHashID),
		sql.Named("next_vote_power_hash_id", nextVotePowerHashID),
	)
	if err != nil {
		return fmt.Errorf("failed to store header: %w", err)
	}

	// Need the header ID for the committed_headers insert coming up.
	headerID, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("failed to get ID of inserted header: %w", err)
	}

	// Now store the proof.
	res, err = tx.ExecContext(
		ctx,
		`INSERT INTO commit_proofs(round, validators_pub_key_hash_id) VALUES (?,?)`,
		ch.Proof.Round, pubKeyHashID,
	)
	if err != nil {
		return fmt.Errorf("failed to insert commit proof: %w", err)
	}
	commitProofID, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("failed to get ID of inserted commit proof: %w", err)
	}

	if err := s.saveCommitProofs(ctx, tx, commitProofID, ch.Proof.Proofs); err != nil {
		return fmt.Errorf("failed to save header's commit proof: %w", err)
	}

	// Finally, insert the committed header record.
	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO committed_headers(header_id, proof_id) VALUES(?, ?)`,
		headerID, commitProofID,
	); err != nil {
		return fmt.Errorf("failed to insert into commit_headers: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction to save header: %w", err)
	}

	return nil
}

// saveCommitProofs accepts a commitProofID and the sparse proof to store,
// and creates all the child entries for the commit proof.
// This is applicable to both a header's previous commit proof
// or to a subjective commit proof for a header.
func (s *TMStore) saveCommitProofs(
	ctx context.Context,
	tx *sql.Tx,
	commitProofID int64,
	proofs map[string][]gcrypto.SparseSignature,
) error {
	// First create the voted blocks.
	args := make([]any, 0, 2*len(proofs))
	q := `INSERT INTO commit_proof_voted_blocks(commit_proof_id, block_hash) VALUES (?,?)` +
		// We can safely assume there is at least one proof,
		// which simplifies the comma joining.
		strings.Repeat(", (?,?)", len(proofs)-1) +
		// We iterate the map in arbitrary order,
		// so it's simpler to just get back the hash-ID pairings.
		` RETURNING id, block_hash`
	for hash := range proofs {
		args = append(args, commitProofID, []byte(hash))
	}
	rows, err := tx.QueryContext(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("failed to insert voted blocks: %w", err)
	}
	defer rows.Close()

	// Possible earlier GC.
	q = ""
	clear(args)

	// Keep a running map of database details.
	type proofTemp struct {
		votedBlockID int64
		sigIDs       []int64
	}
	votedBlocks := make(map[string]proofTemp, len(proofs))

	var tempBlockHash []byte
	for rows.Next() {
		tempBlockHash = tempBlockHash[:0]
		var pt proofTemp
		if err := rows.Scan(&pt.votedBlockID, &tempBlockHash); err != nil {
			return fmt.Errorf("failed to scan voted block ID: %w", err)
		}
		votedBlocks[string(tempBlockHash)] = pt
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("failure when scanning result from inserting voted blocks: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("failed to close result rows from inserting voted blocks: %w", err)
	}

	// Now insert the sparse signatures.
	for blockHash, sigs := range proofs {
		// Insert all the signatures by their block hash.
		// This is easier to manage mapping the returned IDs,
		// compared to doing one bulk insert.
		q = `INSERT INTO sparse_signatures(key_id, signature) VALUES (?,?)` +
			strings.Repeat(", (?,?)", len(sigs)-1) +
			` RETURNING id`

		// We created 2 args per proof before, and we are doing it again now,
		// so this shouldn't grow the args slice.
		args = args[:0]
		for _, sig := range sigs {
			args = append(args, sig.KeyID, sig.Sig)
		}

		rows, err := tx.QueryContext(ctx, q, args...)
		if err != nil {
			return fmt.Errorf("failed to insert sparse signatures: %w", err)
		}
		defer rows.Close() // TODO: this loop could move to a new method, to avoid this looped defer.

		sigIDs := make([]int64, 0, len(sigs))
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				return fmt.Errorf("failed to scan sparse signature ID: %w", err)
			}
			sigIDs = append(sigIDs, id)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("error iterating sparse signatures: %w", err)
		}
		if err := rows.Close(); err != nil {
			return fmt.Errorf("error closing sparse signature iterator: %w", err)
		}

		pt := votedBlocks[blockHash]
		pt.sigIDs = sigIDs
		votedBlocks[blockHash] = pt
	}

	// Store the relationship between the voted blocks and the signatures.
	nSigs := 0
	for _, pt := range votedBlocks {
		nSigs += len(pt.sigIDs)
	}

	if cap(args) < 2*nSigs {
		args = make([]any, 0, 2*nSigs)
	} else {
		clear(args)
		args = args[:0]
	}

	q = `INSERT INTO commit_proof_block_signatures(
commit_proof_voted_block_id, sparse_signature_id
) VALUES (?,?)` +
		strings.Repeat(", (?,?)", nSigs-1)
	for _, pt := range votedBlocks {
		for _, sigID := range pt.sigIDs {
			args = append(args, pt.votedBlockID, sigID)
		}
	}

	if _, err := tx.ExecContext(ctx, q, args...); err != nil {
		return fmt.Errorf("failed to insert into commit_proof_block_signatures: %w", err)
	}

	return nil
}

func (s *TMStore) LoadHeader(ctx context.Context, height uint64) (tmconsensus.CommittedHeader, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return tmconsensus.CommittedHeader{}, fmt.Errorf(
			"failed to begin read-only transaction: %w", err,
		)
	}
	defer tx.Rollback()

	h := tmconsensus.Header{Height: height}
	var headerID, pkhID, npkhID, vphID, nvphID int64
	var pcpID sql.NullInt64
	if err := tx.QueryRowContext(
		ctx,
		`SELECT
id,
hash, prev_block_hash,
data_id, prev_app_state_hash,
user_annotations, driver_annotations,
prev_commit_proof_id,
validators_pub_key_hash_id, next_validators_pub_key_hash_id,
validators_power_hash_id, next_validators_power_hash_id
FROM headers
WHERE
committed = 1 AND height = ?`,
		height,
	).Scan(
		&headerID,
		&h.Hash, &h.PrevBlockHash,
		&h.DataID, &h.PrevAppStateHash,
		&h.Annotations.User, &h.Annotations.Driver,
		&pcpID,
		&pkhID, &npkhID,
		&vphID, &nvphID,
	); err != nil {
		return tmconsensus.CommittedHeader{}, fmt.Errorf(
			"failed to scan header values from database: %w", err,
		)
	}

	// Assuming for now that it's better to do an extra query to get the hashes,
	// since we don't know up front whether the current and next validator sets
	// have the same hashes.
	var nvs, nnvs, nps, nnps int
	if err := tx.QueryRowContext(
		ctx,
		`SELECT * FROM
(SELECT hash, n_keys FROM validator_pub_key_hashes WHERE id = ?),
(SELECT hash, n_keys FROM validator_pub_key_hashes WHERE id = ?),
(SELECT hash, n_powers FROM validator_power_hashes WHERE id = ?),
(SELECT hash, n_powers FROM validator_power_hashes WHERE id = ?)`,
		pkhID, npkhID,
		vphID, nvphID,
	).Scan(
		&h.ValidatorSet.PubKeyHash, &nvs,
		&h.NextValidatorSet.PubKeyHash, &nnvs,
		&h.ValidatorSet.VotePowerHash, &nps,
		&h.NextValidatorSet.VotePowerHash, &nnps,
	); err != nil {
		return tmconsensus.CommittedHeader{}, fmt.Errorf(
			"failed to scan validator hashes from database: %w", err,
		)
	}

	if nvs != nps {
		panic(fmt.Errorf(
			"DATABASE CORRUPTION: attempted to load header at height %d with val hash %x/%d, pow hash %x/%d",
			height,
			h.ValidatorSet.PubKeyHash, nvs,
			h.ValidatorSet.VotePowerHash, nps,
		))
	}
	if nnvs != nnps {
		panic(fmt.Errorf(
			"DATABASE CORRUPTION: attempted to load header at height %d with next val hash %x/%d, next pow hash %x/%d",
			height,
			h.NextValidatorSet.PubKeyHash, nnvs,
			h.NextValidatorSet.VotePowerHash, nnps,
		))
	}

	h.ValidatorSet.Validators = make([]tmconsensus.Validator, nvs)

	// Now we can get the validator public keys.
	rows, err := tx.QueryContext(
		ctx,
		`SELECT idx, key FROM validator_pub_keys_for_hash WHERE hash_id = ?`,
		pkhID,
	)
	if err != nil {
		return tmconsensus.CommittedHeader{}, fmt.Errorf(
			"failed to query validator public keys: %w", err,
		)
	}
	defer rows.Close()
	var encKey []byte
	for rows.Next() {
		encKey = encKey[:0]
		var idx int
		if err := rows.Scan(&idx, &encKey); err != nil {
			return tmconsensus.CommittedHeader{}, fmt.Errorf(
				"failed to scan validator public key: %w", err,
			)
		}
		key, err := s.reg.Unmarshal(encKey)
		if err != nil {
			return tmconsensus.CommittedHeader{}, fmt.Errorf(
				"failed to unmarshal validator key %x: %w", encKey, err,
			)
		}

		h.ValidatorSet.Validators[idx].PubKey = key
	}
	if rows.Err() != nil {
		return tmconsensus.CommittedHeader{}, fmt.Errorf(
			"failed to iterate validator keys: %w", rows.Err(),
		)
	}
	_ = rows.Close()

	// Same for the powers.
	rows, err = tx.QueryContext(
		ctx,
		`SELECT idx, power FROM validator_powers_for_hash WHERE hash_id = ?`,
		vphID,
	)
	if err != nil {
		return tmconsensus.CommittedHeader{}, fmt.Errorf(
			"failed to query validator powers: %w", err,
		)
	}
	defer rows.Close()
	for rows.Next() {
		var idx int
		var pow uint64
		if err := rows.Scan(&idx, &pow); err != nil {
			return tmconsensus.CommittedHeader{}, fmt.Errorf(
				"failed to scan validator power: %w", err,
			)
		}

		h.ValidatorSet.Validators[idx].Power = pow
	}
	if rows.Err() != nil {
		return tmconsensus.CommittedHeader{}, fmt.Errorf(
			"failed to iterate validator powers: %w", rows.Err(),
		)
	}
	if err := rows.Close(); err != nil {
		return tmconsensus.CommittedHeader{}, fmt.Errorf(
			"failed to close validator power iterator: %w", rows.Err(),
		)
	}

	if bytes.Equal(h.ValidatorSet.PubKeyHash, h.NextValidatorSet.PubKeyHash) &&
		bytes.Equal(h.ValidatorSet.VotePowerHash, h.NextValidatorSet.VotePowerHash) {
		h.NextValidatorSet.Validators = slices.Clone(h.ValidatorSet.Validators)
	} else {
		panic("TODO: handle different next validator set")
	}

	if pcpID.Valid {
		// Populate the previous commit proof.

		// TODO: We might be able to do this query as a join on the earlier header query?
		pubKeyHash := make([]byte, 0, len(h.ValidatorSet.PubKeyHash))
		if err := tx.QueryRowContext(
			ctx,
			`SELECT round, hashes.hash FROM commit_proofs
JOIN validator_pub_key_hashes AS hashes ON hashes.id = commit_proofs.validators_pub_key_hash_id
WHERE commit_proofs.id = ?`,
			pcpID.Int64,
		).Scan(&h.PrevCommitProof.Round, &pubKeyHash); err != nil {
			return tmconsensus.CommittedHeader{}, fmt.Errorf(
				"failed to retrieve scan previous commit round and pub key hash: %w", err,
			)
		}
		h.PrevCommitProof.PubKeyHash = string(pubKeyHash)

		rows, err := tx.QueryContext(
			ctx,
			`SELECT block_hash, key_id, signature FROM proof_signatures
WHERE commit_proof_id = ?
ORDER BY key_id`, // Order not strictly necessary, but convenient for tests.
			pcpID.Int64,
		)
		if err != nil {
			return tmconsensus.CommittedHeader{}, fmt.Errorf(
				"failed to retrieve previous commit proof signatures: %w", err,
			)
		}
		defer rows.Close()

		// We are going to populate the previous commit proof map.
		// We are assuming that most of the time, there are only votes for the main block,
		// and maybe some votes for nil.
		h.PrevCommitProof.Proofs = make(map[string][]gcrypto.SparseSignature, 2)

		var blockHash, keyID, sig []byte
		for rows.Next() {
			// Reset and reuse each slice,
			// so the destinations are reused repeatedly.
			// We're going to allocate when we create the SparseSignature anyway,
			// but those will be right-sized at creation.
			blockHash = blockHash[:0]
			keyID = keyID[:0]
			sig = sig[:0]
			if err := rows.Scan(&blockHash, &keyID, &sig); err != nil {
				return tmconsensus.CommittedHeader{}, fmt.Errorf(
					"failed to scan signature row: %w", err,
				)
			}
			h.PrevCommitProof.Proofs[string(blockHash)] = append(
				h.PrevCommitProof.Proofs[string(blockHash)],
				gcrypto.SparseSignature{
					KeyID: bytes.Clone(keyID),
					Sig:   bytes.Clone(sig),
				},
			)
		}

		if err := rows.Close(); err != nil {
			return tmconsensus.CommittedHeader{}, fmt.Errorf(
				"failed to close previous commit proof signatures iterator: %w", err,
			)
		}
	}
	// The header should be fully populated now,
	// so we can load the commit proofs for the header,

	// First get the outer commit proof value.
	// TODO: there is probably a way to do this in a single query.
	var commitProofID int64
	proof := tmconsensus.CommitProof{
		PubKeyHash: string(h.ValidatorSet.PubKeyHash),
		Proofs:     map[string][]gcrypto.SparseSignature{},
	}
	if err := tx.QueryRowContext(
		ctx,
		`SELECT commit_proofs.id, round FROM commit_proofs
JOIN committed_headers ON committed_headers.proof_id = commit_proofs.id
WHERE committed_headers.header_id = ?`,
		headerID,
	).Scan(&commitProofID, &proof.Round); err != nil {
		return tmconsensus.CommittedHeader{}, fmt.Errorf(
			"failed to retrieve commit proof: %w", err,
		)
	}

	// Use the commit proof ID to get the actual signatures.
	rows, err = tx.QueryContext(
		ctx,
		`SELECT block_hash, key_id, signature FROM proof_signatures
WHERE commit_proof_id = ?
ORDER BY key_id`, // Order not strictly necessary, but convenient for tests.
		commitProofID,
	)
	if err != nil {
		return tmconsensus.CommittedHeader{}, fmt.Errorf(
			"failed to retrieve signatures: %w", err,
		)
	}
	defer rows.Close()

	var blockHash, keyID, sig []byte
	for rows.Next() {
		// Reset and reuse each slice,
		// so the destinations are reused repeatedly.
		// We're going to allocate when we create the SparseSignature anyway,
		// but those will be right-sized at creation.
		blockHash = blockHash[:0]
		keyID = keyID[:0]
		sig = sig[:0]
		if err := rows.Scan(&blockHash, &keyID, &sig); err != nil {
			return tmconsensus.CommittedHeader{}, fmt.Errorf(
				"failed to scan signature row: %w", err,
			)
		}
		proof.Proofs[string(blockHash)] = append(
			proof.Proofs[string(blockHash)],
			gcrypto.SparseSignature{
				KeyID: bytes.Clone(keyID),
				Sig:   bytes.Clone(sig),
			},
		)
	}
	if err := rows.Err(); err != nil {
		return tmconsensus.CommittedHeader{}, fmt.Errorf(
			"failed to iterate signature rows: %w", err,
		)
	}

	return tmconsensus.CommittedHeader{
		Header: h,
		Proof:  proof,
	}, nil
}

func pragmas(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `PRAGMA foreign_keys = ON;`)
	if err != nil {
		return fmt.Errorf("failed to set foreign keys on: %w", err)
	}
	return nil
}
