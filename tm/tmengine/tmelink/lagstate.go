package tmelink

// LagState is a value sent from engine internals to the driver,
// when the engine has an updated belief about
// whether it is lagging the rest of the network.
//
// LagState is the combination of a [LagStatus],
// an always-preseent committing height,
// and a sometimes-present "needed" height.
//
// If the Status is [LagStatusKnownMissing], then the NeedHeight field will be non-zero,
// indicating the final needed height to be fully synchronized.
//
// New LagState values are only sent wen the Status field changes.
// An updated CommittingHeight without a Status change,
// will not result in a new value being sent.
type LagState struct {
	Status LagStatus

	CommittingHeight uint64

	NeedHeight uint64
}

type LagStatus uint8

//go:generate go run golang.org/x/tools/cmd/stringer -type LagStatus -trimprefix=LagStatus .

const (
	// At startup, nothing known yet.
	LagStatusInitializing LagStatus = iota

	// We believe we are up to date wtih the network.
	LagStatusUpToDate

	// We think we are behind, but we don't know how many blocks we need yet.
	LagStatusAssumedBehind

	// We know we are missing some range of blocks.
	// This is the only status for which [LagState.NeedHeight] is set.
	LagStatusKnownMissing
)
