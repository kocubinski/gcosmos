package gsi

import (
	"context"

	"cosmossdk.io/core/transaction"
	"github.com/rollchains/gordian/tm/tmconsensus"
)

type ConsensusStrategy[T transaction.Tx] struct {
	d *Driver[T]
}

func NewConsensusStrategy[T transaction.Tx](d *Driver[T]) *ConsensusStrategy[T] {
	return &ConsensusStrategy[T]{
		d: d,
	}
}

func (c *ConsensusStrategy[T]) EnterRound(
	ctx context.Context,
	rv tmconsensus.RoundView,
	proposalOut chan<- tmconsensus.Proposal,
) error {
	return nil
}

func (c *ConsensusStrategy[T]) ConsiderProposedBlocks(
	ctx context.Context,
	pbs []tmconsensus.ProposedBlock,
) (string, error) {
	return "", nil
}

func (c *ConsensusStrategy[T]) ChooseProposedBlock(
	ctx context.Context,
	pbs []tmconsensus.ProposedBlock,
) (string, error) {
	return "", nil
}

func (c *ConsensusStrategy[T]) DecidePrecommit(
	ctx context.Context,
	vs tmconsensus.VoteSummary,
) (string, error) {
	return "", nil
}
