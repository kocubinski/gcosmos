package tmeil

import (
	"github.com/rollchains/gordian/gcrypto"
	"github.com/rollchains/gordian/tm/tmconsensus"
)

// StateMachineRoundActionSet is the value the state machine sends to the engine,
// specifically routed to the mirror kernel.
//
// The state machine indicates its current height and round,
// its public key if it has one,
// and a StateMachineRoundAction channel to send its actions.
// The channel will be 3-buffered so that each action can be sent independently
// and without blocking.
// If the state machine is not participating in that round,
// the channels may be nil.
//
// Values sent by the state machine while the height and round
// match the current committing or voting round view
// should be merged with the current state and persisted to the store.
// However, if the state machine begins to fall behind,
// the mirror may ignore values sent on an action set
// belonging to a stale round.
type StateMachineRoundActionSet struct {
	H uint64
	R uint32

	PubKey gcrypto.PubKey

	Actions chan StateMachineRoundAction

	StateResponse chan StateUpdate
}

// StateMachineRoundAction is the collection of actions that a state machine
// sends to the engine (specifically to the mirror)
// indicating its actions for the round.
//
// Exactly one of the fields must be set.
type StateMachineRoundAction struct {
	PB                 tmconsensus.ProposedBlock
	Prevote, Precommit ScopedSignature
}

// ScopedSignature is a pair of target hash and signature that have an implied height and round,
// stated explicitly in the H and R fields of a [StateMachineRoundActionSet].
type ScopedSignature struct {
	TargetHash string

	// In case this is the first vote for this hash,
	// the state machine provides the sign content so that
	// the mirror does not have to duplicate work to recalculate the same signing content.
	SignContent []byte

	Sig []byte
}

// StateUpdate contains either a versioned round view or a committed block.
// The mirror sends this type to the state machine,
// in response to the state machine sending an updated [StateMachineRoundActionSet].
type StateUpdate struct {
	VRV tmconsensus.VersionedRoundView
	// Only set when VRV is also set. Empty string at initial height.
	// The state machine depends on this value when proposing a block.
	// Actually... the state machine should be able to retrieve the previous finalization,
	// so we probably do not need this field.
	PrevBlockHash string

	CB tmconsensus.CommittedBlock
}

// IsVRV reports whether the update contains a VersionedRoundView,
// by checking for a non-zero height on u.VRV.
func (u StateUpdate) IsVRV() bool {
	return u.VRV.Height > 0
}

// IsCB reports whether the update contains a CommittedBlock,
// by checking for a non-zero height on u.CB.
func (u StateUpdate) IsCB() bool {
	return u.CB.Block.Height > 0
}
