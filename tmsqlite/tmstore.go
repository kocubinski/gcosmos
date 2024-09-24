package tmsqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/rollchains/gordian/tm/tmstore"
)

// TMStore is a single type satisfying all the [tmstore] interfaces.
type TMStore struct {
	// The string "purego" or "cgo" depending on build tags.
	BuildType string

	db *sql.DB
}

func NewTMStore(ctx context.Context, dbPath string) (*TMStore, error) {
	// The driver type comes from the sqlitedriver_*.go file
	// chosen based on build tags.
	db, err := sql.Open(sqliteDriverType, dbPath)
	if err != nil {
		return nil, fmt.Errorf("error opening database: %w", err)
	}

	if err := migrate(ctx, db); err != nil {
		return nil, err
	}

	return &TMStore{
		BuildType: sqliteBuildType,

		db: db,
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
