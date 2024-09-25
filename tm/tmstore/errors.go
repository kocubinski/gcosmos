package tmstore

import (
	"errors"
	"fmt"
)

// PubKeysAlreadyExistError is returned when saving an existing set of validators
// to the [ValidatorStore].
// It can be safely ignored, but it is better to avoid the error if possible.
type PubKeysAlreadyExistError struct {
	ExistingHash string
}

func (e PubKeysAlreadyExistError) Error() string {
	return fmt.Sprintf("validators already stored with hash %x", e.ExistingHash)
}

// VotePowersAlreadyExistError is returned when saving an existing set of vote powers
// to the [ValidatorStore].
// It can be safely ignored, but it is better to avoid the error if possible.
type VotePowersAlreadyExistError struct {
	ExistingHash string
}

func (e VotePowersAlreadyExistError) Error() string {
	return fmt.Sprintf("vote powers already stored with hash %x", e.ExistingHash)
}

// NoPubKeyHashError is returned when loading pub keys from the [ValidatorStore]
// using a hash that does not exist in the store.
type NoPubKeyHashError struct {
	Want string
}

func (e NoPubKeyHashError) Error() string {
	return fmt.Sprintf("no public keys found for hash %x", e.Want)
}

// NoPubKeyHashError is returned when loading vote powers from the [ValidatorStore]
// using a hash that does not exist in the store.
type NoVotePowerHashError struct {
	Want string
}

func (e NoVotePowerHashError) Error() string {
	return fmt.Sprintf("no vote powers found for hash %x", e.Want)
}

// PubKeyPowerCountMismatchError is returned by [ValidatorStore.LoadValidators]
// when both hashes are valid but they correspond to public keys and vote powers
// of differing lengths.
type PubKeyPowerCountMismatchError struct {
	NPubKeys, NVotePower int
}

func (e PubKeyPowerCountMismatchError) Error() string {
	return fmt.Sprintf(
		"mismatched length for pubkeys (%d) and vote powers (%d)",
		e.NPubKeys, e.NVotePower,
	)
}

// DoubleActionError is returned by [ActionStore] if a proposed block,
// prevote, or precommit is attempted to be stored in the same height-round more than once.
type DoubleActionError struct {
	Type string
}

func (e DoubleActionError) Error() string {
	return fmt.Sprintf("refusing double action; %s already recorded", e.Type)
}

// PubKeyChangedError is returned by [ActionStore] when attempting to record
// a prevote and a precommit with two different public keys.
type PubKeyChangedError struct {
	ActionType string
	Want, Got  string
}

func (e PubKeyChangedError) Error() string {
	return fmt.Sprintf(
		"refusing to record %s action with different pubkey; had %x, got %x",
		e.ActionType, e.Want, e.Got,
	)
}

// OverwriteError is returned from store methods that are not intended to overwrite existing values.
// The [tmengine] components that manage the store instances,
// are typically intended to only use the store as durable backup.
// The presence of an OverwriteError therefore usually indicates a programming bug.
type OverwriteError struct {
	Field, Value string
}

func (e OverwriteError) Error() string {
	return fmt.Sprintf(
		"attempted to overwrite existing entry with %s = %s",
		e.Field, e.Value,
	)
}

// FinalizationOverwriteError is returned from [FinalizationStore.SaveFinalization]
// if a finalization already exists at the given height.
// This error indicates a serious programming bug.
type FinalizationOverwriteError struct {
	Height uint64
}

func (e FinalizationOverwriteError) Error() string {
	return fmt.Sprintf(
		"attempted to overwrite existing finalization with height = %d",
		e.Height,
	)
}

// ErrStoreUninitialized is returned by certain store methods
// that need a corresponding Save call before a call to Load is valid.
var ErrStoreUninitialized = errors.New("uninitialized")
