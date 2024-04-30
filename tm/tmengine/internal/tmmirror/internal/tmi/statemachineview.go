package tmi

import (
	"errors"
	"fmt"

	"github.com/rollchains/gordian/gcrypto"
	"github.com/rollchains/gordian/tm/tmconsensus"
	"github.com/rollchains/gordian/tm/tmengine/internal/tmeil"
)

// StateMachineView manages the View values that need to be sent to the state machine.
//
// While the views that the kernel sends to the gossip strategy are effectively stateless,
// there are certain transitionary states that the mirror will quickly discard
// but which must be sent to the state machine in order to keep it in sync.
// Specifically, if a round commits nil, the mirror kernel immediately advances to the next round,
// but the state machine needs that view update which is now referring to an "orphaned" view.
// Otherwise it would be stuck in that orphaned view
// until the kernel discovers a new view it can send to break out,
// such as the height being committed or a subsequent round surpassing a vote threshold.
type stateMachineView struct {
	out chan<- tmconsensus.VersionedRoundView

	// The local state machine's height and round,
	// and the channel it may use to send its actions.
	roundEntrance tmeil.StateMachineRoundEntrance

	// How we separately track the version we've sent,
	// to know if we need to send a new view.
	lastSentVersion uint32

	// There are some circumstances where the kernel needs to ensure
	// that the state machine receives a particular view
	// that the kernel is no longer going to track.
	// See the ForceSend method for more details.
	forceSend *tmconsensus.VersionedRoundView
}

func newStateMachineView(out chan<- tmconsensus.VersionedRoundView) stateMachineView {
	return stateMachineView{out: out}
}

// Output returns a stateMachineOutput value
// which contains a channel and a value to send,
// if the state machine is due for a view update.
func (v *stateMachineView) Output(s *kState) stateMachineOutput {
	if v.forceSend != nil {
		return stateMachineOutput{
			v:           v,
			Ch:          v.out,
			Val:         *v.forceSend,
			sentVersion: v.forceSend.Version,
		}
	}

	if v.roundEntrance.H == s.Voting.VRV.Height &&
		v.roundEntrance.R == s.Voting.VRV.Round &&
		v.lastSentVersion < s.Voting.VRV.Version {
		return stateMachineOutput{
			v:           v,
			Ch:          v.out,
			Val:         s.Voting.VRV.Clone(), // TODO: cloning this is wasteful, but it is a simple approach for now.
			sentVersion: s.Voting.VRV.Version,
		}
	}

	if v.roundEntrance.H == s.Committing.VRV.Height &&
		v.roundEntrance.R == s.Committing.VRV.Round &&
		v.lastSentVersion < s.Committing.VRV.Version {
		return stateMachineOutput{
			v:           v,
			Ch:          v.out,
			Val:         s.Committing.VRV.Clone(), // TODO: cloning this is wasteful, but it is a simple approach for now.
			sentVersion: s.Committing.VRV.Version,
		}
	}

	return stateMachineOutput{}
}

// ForceSend is used by the Kernel to set the Output value to a VersionedRoundView
// that kState is not going to continue tracking.
//
// More specifically, if the kernel is going to advance the voting round,
// but the state machine has not received the update containing the nil precommit,
// the Kernel uses this method to ensure the state machine receives the update
// containing the precommit details to also advance its state to the next round.
func (v *stateMachineView) ForceSend(vrv *tmconsensus.VersionedRoundView) {
	if v.forceSend != nil {
		panic(fmt.Errorf(
			`BUG: (*stateMachineView).ForceSend called twice

Old value: %#v

New value: %#v`,
			v.forceSend, vrv,
		))
	}

	v.forceSend = vrv
}

func (v *stateMachineView) H() uint64 {
	return v.roundEntrance.H
}

func (v *stateMachineView) R() uint32 {
	return v.roundEntrance.R
}

func (v *stateMachineView) Actions() <-chan tmeil.StateMachineRoundAction {
	return v.roundEntrance.Actions
}

func (v *stateMachineView) PubKey() gcrypto.PubKey {
	return v.roundEntrance.PubKey
}

func (v *stateMachineView) Reset(re tmeil.StateMachineRoundEntrance) {
	v.roundEntrance = re
	v.lastSentVersion = 0
}

func (v *stateMachineView) MarkFirstSentVersion(version uint32) {
	if v.lastSentVersion != 0 {
		panic(fmt.Errorf(
			"BUG: (*stateMachineView).MarkFirstSentVersion must only be called with version == 0 (got %d)",
			v.lastSentVersion,
		))
	}

	v.lastSentVersion = version
}

// stateMachineOutput contains a channel and a value to send.
// This value should only be created through the [stateMachineView.Output] method.
//
// If there is no update due, the Ch field is nil, so that using it in a select will not match.
type stateMachineOutput struct {
	v *stateMachineView

	Ch  chan<- tmconsensus.VersionedRoundView
	Val tmconsensus.VersionedRoundView

	sentVersion uint32
}

func (o *stateMachineOutput) MarkSent() {
	if o.v == nil {
		panic(errors.New("BUG: MarkSent called on no-op stateMachineOutput"))
	}

	if o.v.forceSend != nil {
		o.v.forceSend = nil
	}

	o.v.lastSentVersion = o.sentVersion
}
