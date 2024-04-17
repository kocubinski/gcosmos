package tmp2ptest

import (
	"context"
	"fmt"
	"time"

	"github.com/rollchains/gordian/tm/tmconsensus"
)

// ChannelBroadcaster satisfies the tmp2p.Broadcaster interface,
// emitting to exported channels.
// However, because this is meant to be used in tests,
// there are gracious 5-second timeouts associated with the channels.
// If a channel is blocked sending for that duration, ChannelBroadcaster panics.
type ChannelBroadcaster struct {
	pbInCh, pbOutCh chan tmconsensus.ProposedBlock

	prevoteInCh, prevoteOutCh chan tmconsensus.PrevoteSparseProof

	precommitInCh, precommitOutCh chan tmconsensus.PrecommitSparseProof
}

func NewChannelBroadcaster(ctx context.Context) *ChannelBroadcaster {
	cb := &ChannelBroadcaster{
		pbInCh:  make(chan tmconsensus.ProposedBlock, 1),
		pbOutCh: make(chan tmconsensus.ProposedBlock),

		prevoteInCh:  make(chan tmconsensus.PrevoteSparseProof, 1),
		prevoteOutCh: make(chan tmconsensus.PrevoteSparseProof),

		precommitInCh:  make(chan tmconsensus.PrecommitSparseProof, 1),
		precommitOutCh: make(chan tmconsensus.PrecommitSparseProof),
	}

	go cb.background(ctx)
	return cb
}

func (cb *ChannelBroadcaster) background(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case pb := <-cb.pbInCh:
			sendOrPanic(ctx, cb.pbOutCh, pb)
		case proof := <-cb.prevoteInCh:
			sendOrPanic(ctx, cb.prevoteOutCh, proof)
		case proof := <-cb.precommitInCh:
			sendOrPanic(ctx, cb.precommitOutCh, proof)
		}
	}
}

func (cb *ChannelBroadcaster) OutgoingProposedBlocks() chan<- tmconsensus.ProposedBlock {
	return cb.pbInCh
}

// ProposedBlocks is the channel for the test to read,
// to inspect proposed blocks that have been broadcast.
func (cb *ChannelBroadcaster) ProposedBlocks() <-chan tmconsensus.ProposedBlock {
	return cb.pbOutCh
}

func (cb *ChannelBroadcaster) OutgoingPrevoteProofs() chan<- tmconsensus.PrevoteSparseProof {
	return cb.prevoteInCh
}

// PrevoteProofs is the channel for the test to read,
// to inspect prevote proofs that have been broadcast.
func (cb *ChannelBroadcaster) PrevoteProofs() <-chan tmconsensus.PrevoteSparseProof {
	return cb.prevoteOutCh
}

func (cb *ChannelBroadcaster) OutgoingPrecommitProofs() chan<- tmconsensus.PrecommitSparseProof {
	return cb.precommitInCh
}

// PrecommitProofs is the channel for the test to read,
// to inspect precommit proofs that have been broadcast.
func (cb *ChannelBroadcaster) PrecommitProofs() <-chan tmconsensus.PrecommitSparseProof {
	return cb.precommitOutCh
}

func sendOrPanic[T any](ctx context.Context, ch chan<- T, val T) {
	tick := time.NewTimer(5 * time.Second)
	defer tick.Stop()

	select {
	case <-ctx.Done():
	case ch <- val:
	case <-tick.C:
		panic(fmt.Errorf("channel of type %T not read within 5 seconds", val))
	}
}
