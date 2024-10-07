// Package tmstore contains the store interfaces for the gordian/tm tree.
package tmstore

// Compile-time assertion that all the stores can be embedded in a single interface.
// This would only fail if there were conflicting method names across different stores.
//
// This could possibly get promoted to a named interface later,
// but right now there is no place where we need all stores explicitly in one value.
var _ interface {
	ActionStore
	CommittedHeaderStore
	FinalizationStore
	MirrorStore
	RoundStore
	ValidatorStore
}
