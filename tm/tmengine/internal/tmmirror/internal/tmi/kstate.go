package tmi

import (
	"context"
	"fmt"

	"github.com/rollchains/gordian/tm/tmconsensus"
)

// kState holds the kernel's mutable state.
type kState struct {
	Committing, Voting View

	// The NextRound view is straightforward because we can be certain of the validator set,
	// and we somewhat anticipate receiving messages for that round
	// before we orphan the current voting view.
	NextRound View

	// When the kernel transitions from one voting round to another,
	// we need to emit the nil-committed round to the gossip strategy.
	// This field holds that value until it is sent to the gossip strategy.
	NilVotedRound *tmconsensus.VersionedRoundView

	// The kernel makes a fetch request if a block reaches >1/3
	// prevotes or precommits, and we don't have the actual proposed block.
	// If a request is outstanding and we switch views,
	// we need to cancel those outstanding requests.
	InFlightFetchPBs map[string]context.CancelFunc

	// Certain operations on the Voting view require knowledge
	// of which block in the Committing view, is being committed.
	// The block will be the zero value if the mirror does not yet have a Committing view.
	CommittingBlock tmconsensus.Block

	// There is a dedicated manager for the view to send to the state machine.
	// The views we send to the gossip strategy are effectively stateless,
	// but the state machine has some subtle edge cases that need to be handled.
	StateMachineViewManager stateMachineViewManager
}

// FindView finds the view in s matching the given height and round,
// if such a view exists, and returns that view and an identifier.
// If no view matches the height and round, it returns nil, 0, and an appropriate status.
func (s *kState) FindView(h uint64, r uint32, reason string) (*View, ViewID, ViewLookupStatus) {
	if h == s.Voting.VRV.Height {
		vr := s.Voting.VRV.Round
		if r == vr {
			return &s.Voting, ViewIDVoting, ViewFound
		}

		if r == vr+1 {
			return &s.NextRound, ViewIDNextRound, ViewFound
		}

		if r < vr {
			return nil, 0, ViewOrphaned
		}

		return nil, 0, ViewLaterVotingRound
	}

	if h == s.Committing.VRV.Height {
		cr := s.Committing.VRV.Round
		if r == cr {
			return &s.Committing, ViewIDCommitting, ViewFound
		}

		if r < cr {
			return nil, 0, ViewBeforeCommitting
		}
	}

	if h < s.Committing.VRV.Height {
		return nil, 0, ViewBeforeCommitting
	}

	if h > s.Voting.VRV.Height {
		// TODO: this does not properly account for NextHeight, which is not yet implemented.
		return nil, 0, ViewFuture
	}

	panic(fmt.Errorf(
		"TODO: unhandled attempt to find view (reason: %s, request: %d/%d, voting view: %d/%d, committing view: %d/%d)",
		reason, h, r, s.Voting.VRV.Height, s.Voting.VRV.Round, s.Committing.VRV.Height, s.Committing.VRV.Round,
	))
}
