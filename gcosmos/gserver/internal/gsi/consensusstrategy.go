package gsi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"cosmossdk.io/core/transaction"
	"github.com/rollchains/gordian/internal/gchan"
	"github.com/rollchains/gordian/tm/tmconsensus"
)

type ConsensusStrategy[T transaction.Tx] struct {
	log *slog.Logger

	d *Driver[T]

	curH uint64
	curR uint32
}

func NewConsensusStrategy[T transaction.Tx](
	log *slog.Logger,
	d *Driver[T],
) *ConsensusStrategy[T] {
	return &ConsensusStrategy[T]{
		log: log,
		d:   d,
	}
}

func (c *ConsensusStrategy[T]) EnterRound(
	ctx context.Context,
	rv tmconsensus.RoundView,
	proposalOut chan<- tmconsensus.Proposal,
) error {
	// Track the current height and round for later when we get to voting.
	c.curH = rv.Height
	c.curR = rv.Round

	// TODO: a better system of determining whether we should propose this block.
	const weShouldPropose = true
	if !weShouldPropose {
		return nil
	}

	if proposalOut == nil {
		panic(errors.New(
			"BUG: proposalOut channel was nil when we were supposed to propose",
		))
	}

	if !gchan.SendC(
		ctx, c.log,
		proposalOut, tmconsensus.Proposal{
			AppDataID: fmt.Sprintf("%d/%d", rv.Height, rv.Round),
		},
		"sending proposal to engine",
	) {
		return context.Cause(ctx)
	}
	return nil
}

func (c *ConsensusStrategy[T]) ConsiderProposedBlocks(
	ctx context.Context,
	pbs []tmconsensus.ProposedBlock,
) (string, error) {
	expAppDataID := fmt.Sprintf("%d/%d", c.curH, c.curR)
	for _, pb := range pbs {
		if pb.Block.Height != c.curH {
			continue
		}
		if pb.Round != c.curR {
			continue
		}

		if string(pb.Block.DataID) != expAppDataID {
			continue
		}

		return string(pb.Block.Hash), nil
	}

	return "", tmconsensus.ErrProposedBlockChoiceNotReady
}

func (c *ConsensusStrategy[T]) ChooseProposedBlock(
	ctx context.Context,
	pbs []tmconsensus.ProposedBlock,
) (string, error) {
	h, err := c.ConsiderProposedBlocks(ctx, pbs)
	if err == tmconsensus.ErrProposedBlockChoiceNotReady {
		return "", nil
	}

	return h, nil
}

func (c *ConsensusStrategy[T]) DecidePrecommit(
	ctx context.Context,
	vs tmconsensus.VoteSummary,
) (string, error) {
	maj := tmconsensus.ByzantineMajority(vs.AvailablePower)
	if pow := vs.PrevoteBlockPower[vs.MostVotedPrevoteHash]; pow >= maj {
		return vs.MostVotedPrevoteHash, nil
	}

	// Didn't reach consensus on one block; automatically precommit nil.
	return "", nil
}
