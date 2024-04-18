package tmconsensus

import (
	"context"
	"errors"
)

// Proposal is the data an application needs to provide,
// for the engine to compose a [ProposedBlock].
type Proposal struct {
	AppDataID string // TODO: this should switch back to []byte.

	// If set, this will populate the [ProposedBlock.AppAnnotation] field.
	Annotation []byte
}

// ConsensusStrategy determines how a state machine proposes blocks
// and what blocks to prevote or precommit.
type ConsensusStrategy interface {
	// The state machine calls this method synchronously when it begins a round.
	// It is possible that the state machine is lagging behind the network,
	// in which case there may be existing proposed blocks or votes in rv.
	//
	// The state machine does not call EnterRound if it is catching up
	// on blocks that are already committed to the chain.
	//
	// If the application is going to propose a block for this round,
	// it must publish the proposal information to the proposalOut channel;
	// the state machine will compose that information into a proposed block.
	EnterRound(ctx context.Context, rv RoundView, proposalOut chan<- Proposal) error

	// ConsiderProposedBlocks is called for each proposed block the state machine receives
	// while awaiting proposed blocks and before the proposed block timeout has elapsed.
	//
	// If the returned error is nil, the returned string is assumed to be the block hash to prevote.
	// The empty string indicates a prevote for nil.
	//
	// If the consensus strategy wants to wait longer before making a selection,
	// it must return ErrProposedBlockChoiceNotReady.
	// Any other error is fatal.
	ConsiderProposedBlocks(ctx context.Context, pbs []ProposedBlock) (string, error)

	// ChooseProposedBlock is called when the state machine's proposal delay has elapsed.
	// The pbs slice may be empty.
	//
	// ChooseProposedBlock must return the hash of the block to vote for.
	// Under certain circumstances (like Proof of Lock),
	// the returned hash may not be present in the slice of proposed blocks.
	//
	// The state machine calls this in a background goroutine,
	// so the method may block as long as necessary.
	// Nonetheless, the method must respect context cancellation.
	ChooseProposedBlock(ctx context.Context, pbs []ProposedBlock) (string, error)

	// DecidePrecommit is called when prevoting is finished
	// and the state machine needs to submit a precommit.
	//
	// The returned string value is the block hash to precommit.
	// The empty string indicates a precommit for nil.
	// A returned error is fatal.
	//
	// NOTE: this method will likely change with vote extensions.
	DecidePrecommit(ctx context.Context, vs VoteSummary) (string, error)
}

// ErrProposedBlockChoiceNotReady is a sentinel error the [ConsensusStrategy] must return
// from its ConsiderProposedBlocks method, if it is not ready to choose a proposed block.
var ErrProposedBlockChoiceNotReady = errors.New("not ready to choose proposed block")
