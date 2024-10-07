package tmstore

import (
	"context"
)

// Store is the storage engine for values that a [Mirror] needs to read and write.
type MirrorStore interface {
	// Set and get the "network height and round", which is what the Mirror
	// believes to be the current round for voting and the current round
	// for a block being committed.
	//
	// While an ephemeral copy of this value lives in the Mirror,
	// it needs to be persisted to disk in order to resume upon process restart.
	SetNetworkHeightRound(
		ctx context.Context,
		votingHeight uint64, votingRound uint32,
		committingHeight uint64, committingRound uint32,
	) error

	NetworkHeightRound(context.Context) (
		votingHeight uint64, votingRound uint32,
		committingHeight uint64, committingRound uint32,
		err error,
	)
}
