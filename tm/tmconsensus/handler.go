package tmconsensus

import (
	"context"

	"github.com/rollchains/gordian/gexchange"
)

// ConsensusHandler is the interface to handle the set of consensus messages.
//
// In production this will be a [tmgossip.Strategy] value.
type ConsensusHandler interface {
	HandleProposedBlock(context.Context, ProposedBlock) gexchange.Feedback
	HandlePrevoteProofs(context.Context, PrevoteSparseProof) gexchange.Feedback
	HandlePrecommitProofs(context.Context, PrecommitSparseProof) gexchange.Feedback
}

// FineGrainedConsensusHandler is the preferred interface (over [ConsensusHandler])
// for handling incoming p2p consensus messages.
//
// Use one of the FeedbackMapper types to convert a FineGrainedConsensusHandler into a ConsensusHandler.
type FineGrainedConsensusHandler interface {
	HandleProposedBlock(context.Context, ProposedBlock) HandleProposedBlockResult
	HandlePrevoteProofs(context.Context, PrevoteSparseProof) HandleVoteProofsResult
	HandlePrecommitProofs(context.Context, PrecommitSparseProof) HandleVoteProofsResult
}

// HandleProposedBlockResult is a set of constants
// to be returned from a FineGrainedConsensusHandler's HandleProposedBlock method.
type HandleProposedBlockResult uint8

//go:generate go run golang.org/x/tools/cmd/stringer -type HandleProposedBlockResult -trimprefix=HandleProposedBlock .
const (
	// Keep zero value invalid, so return 0 is somewhat meaningful.
	_ HandleProposedBlockResult = iota

	// Everything checked out -- this was a new proposed block added to our store.
	HandleProposedBlockAccepted

	// We already stored a copy of this proposed block.
	HandleProposedBlockAlreadyStored

	// The signer of the proposed block did not match a validator in the current round.
	HandleProposedBlockSignerUnrecognized

	// Our calculation of the block hash was different from what the block reported.
	HandleProposedBlockBadBlockHash

	// Signature verification on the proposed block failed.
	HandleProposedBlockBadSignature

	// Proposed block had older height or round than our current view of the world.
	HandleProposedBlockRoundTooOld

	// Proposed block is beyond our NextHeight and/or NextRound handlers.
	HandleProposedBlockRoundTooFarInFuture

	// Internal error not necessarily correlated with the actual proposed block.
	HandleProposedBlockInternalError
)

// HandleVoteProofsResult is a set of constants
// to be returned from a FineGrainedConsensusHandler's HandlePrevoteProofs and HandlePrecommitProofs methods.
type HandleVoteProofsResult uint8

//go:generate go run golang.org/x/tools/cmd/stringer -type HandleVoteProofsResult -trimprefix=HandleVoteProofs .
const (
	_ HandleVoteProofsResult = iota

	// Proofs were added to the store.
	HandleVoteProofsAccepted

	// We already had all the signatures in the given proof.
	HandleVoteProofsNoNewSignatures

	// There were no proofs in the message.
	// (This should only happen on messages from a misbehaving peer.)
	HandleVoteProofsEmpty

	// The public key hash did not match what we expected for the given height and round.
	HandleVoteProofsBadPubKeyHash

	// Votes had older height or round than our current view of the world.
	HandleVoteProofsRoundTooOld

	// Vote is beyond our NextHeight and/or NextRound handlers.
	HandleVoteProofsTooFarInFuture

	// Internal error not necessarily correlated with the actual prevote proof.
	HandleVoteProofsInternalError
)
