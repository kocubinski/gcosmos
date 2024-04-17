package tmconsensus

import (
	"context"
	"fmt"

	"github.com/rollchains/gordian/gexchange"
)

// AcceptAllValidFeedbackMapper converts a [FineGrainedConsensusHandler]
// to a [ConsensusHandler], accepting any valid input,
// even if the input was already known.
type AcceptAllValidFeedbackMapper struct {
	Handler FineGrainedConsensusHandler
}

func (m AcceptAllValidFeedbackMapper) HandleProposedBlock(
	ctx context.Context, pb ProposedBlock,
) gexchange.Feedback {
	f := m.Handler.HandleProposedBlock(ctx, pb)
	switch f {
	case HandleProposedBlockAccepted,
		HandleProposedBlockAlreadyStored:
		return gexchange.FeedbackAccepted

	case HandleProposedBlockRoundTooOld,
		HandleProposedBlockInternalError:
		return gexchange.FeedbackIgnored

	case HandleProposedBlockSignerUnrecognized,
		HandleProposedBlockBadSignature,
		HandleProposedBlockBadBlockHash:
		return gexchange.FeedbackRejected

	default:
		panic(fmt.Errorf("BUG: no HandleProposedBlockResult mapping set for %s", f))
	}
}

func (m AcceptAllValidFeedbackMapper) HandlePrevoteProofs(
	ctx context.Context, p PrevoteSparseProof,
) gexchange.Feedback {
	f := m.Handler.HandlePrevoteProofs(ctx, p)
	return m.mapVoteResult(f, "HandlePrevoteProofs")
}

func (m AcceptAllValidFeedbackMapper) HandlePrecommitProofs(
	ctx context.Context, p PrecommitSparseProof,
) gexchange.Feedback {
	f := m.Handler.HandlePrecommitProofs(ctx, p)
	return m.mapVoteResult(f, "HandlePrecommitProofs")
}

func (m AcceptAllValidFeedbackMapper) mapVoteResult(
	f HandleVoteProofsResult, name string,
) gexchange.Feedback {
	switch f {
	case HandleVoteProofsNoNewSignatures,
		HandleVoteProofsAccepted:
		return gexchange.FeedbackAccepted

	case HandleVoteProofsRoundTooOld,
		HandleVoteProofsInternalError:
		return gexchange.FeedbackIgnored

	case HandleVoteProofsEmpty,
		HandleVoteProofsBadPubKeyHash:
		return gexchange.FeedbackRejected

	default:
		panic(fmt.Errorf("BUG: no %s mapping set for %s", name, f))
	}
}
