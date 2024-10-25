package gcstore

import (
	"context"
)

// BlockDataStore persists block data for later retrieval.
//
// Typically this would not be part of the consensus layer,
// but the SDK does not offer this out of the box.
//
// The data for this store is treated as completely opaque.
// Therefore, this store can be used to store block data
// that is ready to transmit across the wire,
// pre-compressed for instance.
type BlockDataStore interface {
	// SaveBlockData persists the given data under the given pair of height and data ID.
	// Both height and dataID must be respectively unique across
	// other heights and data IDs;
	// otherwise an [AlreadyHaveBlockDataForHeightError]
	// or [AlreadyHaveBlockDataForIDError] is returned.
	//
	// Callers may assume that the store does not retain a reference to data.
	SaveBlockData(ctx context.Context, height uint64, dataID string, data []byte) error

	// LoadBlockDataByHeight returns the data for the given height,
	// and the dataID with which it was saved.
	// The returned data is the result of appending the underlying data
	// to the given dst slice, which is allowed to be nil.
	//
	// If data for that height was never saved,
	// [ErrBlockDataNotFound] is returned.
	LoadBlockDataByHeight(ctx context.Context, height uint64, dst []byte) (
		dataID string, data []byte, err error,
	)

	// LoadBlockDataByID returns the data for the given ID,
	// and the height with which it was saved.
	// The returned data is the result of appending the underlying data
	// to the given dst slice, which is allowed to be nil.
	//
	// If data for that height was never saved,
	// [ErrBlockDataNotFound] is returned.
	LoadBlockDataByID(ctx context.Context, dataID string, dst []byte) (
		height uint64, data []byte, err error,
	)
}
