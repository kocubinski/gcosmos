package gsi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/rollchains/gordian/gcrypto"
	"github.com/rollchains/gordian/internal/gchan"
	"github.com/rollchains/gordian/internal/glog"
	"github.com/rollchains/gordian/tm/tmconsensus"
)

type ConsensusStrategy struct {
	log *slog.Logger

	d *Driver

	signer gcrypto.Signer

	curH uint64
	curR uint32
}

func NewConsensusStrategy(
	log *slog.Logger,
	d *Driver,
	signer gcrypto.Signer,
) *ConsensusStrategy {
	return &ConsensusStrategy{
		log:    log,
		d:      d,
		signer: signer,
	}
}

type BlockAnnotation struct {
	BlockTime string `json:"block_time"`
}

func NewBlockAnnotation(blockTime time.Time) ([]byte, error) {
	ba := BlockAnnotation{
		BlockTime: blockTime.Format(time.RFC3339Nano),
	}
	return json.Marshal(ba)
}

func BlockAnnotationFromBytes(b []byte) (BlockAnnotation, error) {
	var ba BlockAnnotation
	err := json.Unmarshal(b, &ba)
	return ba, err
}

func (ba BlockAnnotation) BlockTimeAsTime() (time.Time, error) {
	return time.Parse(time.RFC3339, ba.BlockTime)
}

func (c *ConsensusStrategy) EnterRound(
	ctx context.Context,
	rv tmconsensus.RoundView,
	proposalOut chan<- tmconsensus.Proposal,
) error {
	// Track the current height and round for later when we get to voting.
	c.curH = rv.Height
	c.curR = rv.Round

	// Very naive round-robin-ish proposer selection.
	proposerIdx := (int(rv.Height) + int(rv.Round)) % len(rv.Validators)

	proposingVal := rv.Validators[proposerIdx]
	weShouldPropose := proposingVal.PubKey.Equal(c.signer.PubKey())
	if !weShouldPropose {
		return nil
	}

	if proposalOut == nil {
		panic(errors.New(
			"BUG: proposalOut channel was nil when we were supposed to propose",
		))
	}

	ba, err := NewBlockAnnotation(time.Now())
	if err != nil {
		return fmt.Errorf("failed to create block annotation: %w", err)
	}

	if !gchan.SendC(
		ctx, c.log,
		proposalOut, tmconsensus.Proposal{
			AppDataID: AppDataID(rv.Height, rv.Round, nil),
			BlockAnnotations: tmconsensus.Annotations{
				Driver: ba,
			},
		},
		"sending proposal to engine",
	) {
		return context.Cause(ctx)
	}
	return nil
}

func (c *ConsensusStrategy) ConsiderProposedBlocks(
	ctx context.Context,
	pbs []tmconsensus.ProposedBlock,
) (string, error) {
	for _, pb := range pbs {
		if pb.Block.Height != c.curH {
			c.log.Debug(
				"Ignoring proposed block due to height mismatch",
				"want", c.curH, "got", pb.Block.Height,
			)
			continue
		}
		if pb.Round != c.curR {
			c.log.Debug(
				"Ignoring proposed block due to round mismatch",
				"h", c.curH,
				"want", c.curR, "got", pb.Round,
			)
			continue
		}

		h, r, nTxs, _, err := ParseAppDataID(string(pb.Block.DataID))
		if err != nil {
			c.log.Debug(
				"Ignoring proposed block due to unparseable app data ID",
				"h", c.curH, "r", c.curR,
				"block_hash", glog.Hex(pb.Block.Hash),
				"err", err,
			)
			continue
		}
		if h != c.curH {
			c.log.Debug(
				"Ignoring proposed block due to wrong height in app data ID",
				"h", c.curH, "r", c.curR,
				"got_h", h,
			)
			continue
		}
		if r != c.curR {
			c.log.Debug(
				"Ignoring proposed block due to wrong round in app data ID",
				"h", c.curH, "r", c.curR,
				"got_r", r,
			)
			continue
		}
		if nTxs != 0 {
			panic(errors.New(
				"TODO: handle app data IDs that indicate presence of at least one transaction",
			))
		}

		ba, err := BlockAnnotationFromBytes(pb.Block.Annotations.Driver)
		if err != nil {
			c.log.Debug(
				"Ignoring proposed block due to error extracting block annotation",
				"h", c.curH, "r", c.curR, "err", err,
			)
			continue
		}

		bt, err := ba.BlockTimeAsTime()
		if err != nil {
			c.log.Debug(
				"Ignoring proposed block due to error extracting block time from annotation",
				"h", c.curH, "r", c.curR, "err", err,
			)
			continue
		}

		if bt.After(time.Now()) {
			c.log.Debug(
				"Ignoring proposed block due to block time in the future",
				"h", c.curH, "r", c.curR, "err", err,
			)
			continue
		}

		return string(pb.Block.Hash), nil
	}

	return "", tmconsensus.ErrProposedBlockChoiceNotReady
}

func (c *ConsensusStrategy) ChooseProposedBlock(
	ctx context.Context,
	pbs []tmconsensus.ProposedBlock,
) (string, error) {
	h, err := c.ConsiderProposedBlocks(ctx, pbs)
	if err == tmconsensus.ErrProposedBlockChoiceNotReady {
		return "", nil
	}
	if err != nil {
		return "", err
	}

	return h, nil
}

func (c *ConsensusStrategy) DecidePrecommit(
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
