package tmdebug

import (
	"context"
	"log/slog"

	"github.com/rollchains/gordian/gexchange"
	"github.com/rollchains/gordian/internal/glog"
	"github.com/rollchains/gordian/tm/tmconsensus"
)

// LoggingConsensusHandler logs the inputs to the [tmconsensus.ConsensusHandler] interface
// and delegates to the configured Handler.
type LoggingConsensusHandler struct {
	Log *slog.Logger

	Handler tmconsensus.ConsensusHandler
}

func (h LoggingConsensusHandler) HandleProposedBlock(ctx context.Context, pb tmconsensus.ProposedBlock) gexchange.Feedback {
	log := h.Log.With(
		"height", pb.Block.Height,
		"round", pb.Round,
		"block_hash", glog.Hex(pb.Block.Hash),
	)

	log.Info("Handling proposed block")

	f := h.Handler.HandleProposedBlock(ctx, pb)

	log.Info("Handled proposed block", "feedback", f.String())

	return f
}

func (h LoggingConsensusHandler) HandlePrevoteProofs(ctx context.Context, p tmconsensus.PrevoteSparseProof) gexchange.Feedback {
	log := h.Log.With(
		"height", p.Height,
		"round", p.Round,
		"pubkey_hash", glog.Hex(p.PubKeyHash),
		"n_blocks", len(p.Proofs),
	)

	log.Info("Handling prevote proofs")

	f := h.Handler.HandlePrevoteProofs(ctx, p)

	log.Info("Handled prevote proofs", "feedback", f.String())

	return f
}

func (h LoggingConsensusHandler) HandlePrecommitProofs(ctx context.Context, p tmconsensus.PrecommitSparseProof) gexchange.Feedback {
	log := h.Log.With(
		"height", p.Height,
		"round", p.Round,
		"pubkey_hash", glog.Hex(p.PubKeyHash),
		"n_blocks", len(p.Proofs),
	)

	log.Info("Handling precommit proofs")

	f := h.Handler.HandlePrecommitProofs(ctx, p)

	log.Info("Handled precommit proofs", "feedback", f.String())

	return f
}

// LoggingFineGrainedConsensusHandler logs the inputs
// to the [tmconsensus.FineGrainedConsensusHandler] interface
// and delegates to the configured Handler.
type LoggingFineGrainedConsensusHandler struct {
	Log *slog.Logger

	Handler tmconsensus.FineGrainedConsensusHandler
}

func (h LoggingFineGrainedConsensusHandler) HandleProposedBlock(ctx context.Context, pb tmconsensus.ProposedBlock) tmconsensus.HandleProposedBlockResult {
	log := h.Log.With(
		"height", pb.Block.Height,
		"round", pb.Round,
		"block_hash", glog.Hex(pb.Block.Hash),
	)

	log.Info("Handling proposed block")

	r := h.Handler.HandleProposedBlock(ctx, pb)

	log.Info("Handled proposed block", "result", r.String())

	return r
}

func (h LoggingFineGrainedConsensusHandler) HandlePrevoteProofs(ctx context.Context, p tmconsensus.PrevoteSparseProof) tmconsensus.HandleVoteProofsResult {
	log := h.Log.With(
		"height", p.Height,
		"round", p.Round,
		"pubkey_hash", glog.Hex(p.PubKeyHash),
		"n_blocks", len(p.Proofs),
	)

	log.Info("Handling prevote proofs")

	r := h.Handler.HandlePrevoteProofs(ctx, p)

	log.Info("Handled prevote proofs", "result", r.String())

	return r
}

func (h LoggingFineGrainedConsensusHandler) HandlePrecommitProofs(ctx context.Context, p tmconsensus.PrecommitSparseProof) tmconsensus.HandleVoteProofsResult {
	log := h.Log.With(
		"height", p.Height,
		"round", p.Round,
		"pubkey_hash", glog.Hex(p.PubKeyHash),
		"n_blocks", len(p.Proofs),
	)

	log.Info("Handling precommit proofs")

	r := h.Handler.HandlePrecommitProofs(ctx, p)

	log.Info("Handled precommit proofs", "result", r.String())

	return r
}
