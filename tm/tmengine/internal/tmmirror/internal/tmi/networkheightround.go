package tmi

import "context"

// NetworkHeightRound is the Mirror's view of the different rounds in the network.
//
// This is a convenience type for now, and it may change as the Mirror evolves.
type NetworkHeightRound struct {
	VotingHeight uint64
	VotingRound  uint32

	CommittingHeight uint64 // Is this field necessary? Would CommittingHeight ever differ from VotingHeight-1?
	CommittingRound  uint32
}

// NetworkHeightRoundFromStore parses the outputs of [tmstore.MirrorStore.NetworkHeightRoundFromStore]
// such that you can call:
//
//	nhr, err := NetworkHeightRoundFromStore(s.NetworkHeightRound(ctx))
func NetworkHeightRoundFromStore(
	votingHeight uint64, votingRound uint32,
	committingHeight uint64, committingRound uint32,
	e error,
) (NetworkHeightRound, error) {
	return NetworkHeightRound{
		VotingHeight:     votingHeight,
		VotingRound:      votingRound,
		CommittingHeight: committingHeight,
		CommittingRound:  committingRound,
	}, e
}

// ForStore explodes nhr into positional arguments for [tmstore.MirrorStore.SetNetworkHeightRound]
// such that you can call:
//
//	err := s.SetNetworkHeightRound(nhr.ForStore(ctx))
func (nhr NetworkHeightRound) ForStore(ctx context.Context) (
	context.Context, uint64, uint32, uint64, uint32,
) {
	return ctx,
		nhr.VotingHeight, nhr.VotingRound,
		nhr.CommittingHeight, nhr.CommittingRound
}
