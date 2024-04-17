package tmstore

import (
	"context"

	"github.com/rollchains/gordian/gcrypto"
	"github.com/rollchains/gordian/tm/tmconsensus"
)

// RoundStore stores and retrieves the proposed blocks, prevotes, and precommits
// observed during each round.
type RoundStore interface {
	// SaveProposedBlock saves the given proposed block
	// as a candidate proposed block in the given height and round.
	SaveProposedBlock(ctx context.Context, pb tmconsensus.ProposedBlock) error

	// The overwrite proofs methods overwrite existing entries
	// for the corresponding proof at the given height and round.
	OverwritePrevoteProofs(
		ctx context.Context,
		height uint64,
		round uint32,
		proofs map[string]gcrypto.CommonMessageSignatureProof,
	) error
	OverwritePrecommitProofs(
		ctx context.Context,
		height uint64,
		round uint32,
		proofs map[string]gcrypto.CommonMessageSignatureProof,
	) error

	// LoadRoundState returns the saved proposed blocks and votes
	// for the given height and round.
	// The order of the proposed blocks in the pbs slice is undefined
	// and may differ from one call to another.
	//
	// If there are no proposed blocks or votes at the given height and round,
	// [tmconsensus.RoundUnknownError] is returned.
	// If at least one proposed block, prevote, or precommit exists at the height and round,
	// a nil error is returned.
	LoadRoundState(ctx context.Context, height uint64, round uint32) (
		pbs []tmconsensus.ProposedBlock,
		prevotes, precommits map[string]gcrypto.CommonMessageSignatureProof,
		err error,
	)
}
