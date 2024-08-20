//go:build !debug

package tsi

// No-op invariant checks.

func (rlc *RoundLifecycle) invariantCycleFinalization() {}
