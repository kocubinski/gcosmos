package tmstore

import (
	"context"

	"github.com/rollchains/gordian/gcrypto"
	"github.com/rollchains/gordian/tm/tmconsensus"
)

// ActionStore stores the active actions the current state machine and application take;
// specifically, proposed blocks, prevotes, and precommits.
type ActionStore interface {
	SaveProposedHeader(context.Context, tmconsensus.ProposedHeader) error

	SavePrevote(ctx context.Context, pubKey gcrypto.PubKey, vt tmconsensus.VoteTarget, sig []byte) error
	SavePrecommit(ctx context.Context, pubKey gcrypto.PubKey, vt tmconsensus.VoteTarget, sig []byte) error

	// Load returns all actions recorded for this round.
	Load(ctx context.Context, height uint64, round uint32) (RoundActions, error)
}

// RoundActions contains all three possible actions the current validator
// may have taken for a single round.
type RoundActions struct {
	Height uint64
	Round  uint32

	ProposedHeader tmconsensus.ProposedHeader

	PubKey gcrypto.PubKey

	PrevoteTarget    string // Block hash or empty string for nil.
	PrevoteSignature string // Immutable signature.

	PrecommitTarget    string // Block hash or empty string for nil.
	PrecommitSignature string // Immutable signature.
}
