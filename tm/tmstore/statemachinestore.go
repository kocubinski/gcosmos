package tmstore

import "context"

// StateMachineStore contains values that an engine's state machine needs to read and write.
type StateMachineStore interface {
	// Track the state machine's current height and round,
	// so that it can pick up where it left off, after a process restart.
	SetStateMachineHeightRound(
		ctx context.Context,
		height uint64, round uint32,
	) error

	StateMachineHeightRound(context.Context) (
		height uint64, round uint32,
		err error,
	)
}
