package tmconsensus

import (
	"fmt"
	"io"
)

// Genesis is the value used to initialize a consensus store.
//
// In normal use this is derived from InitChain,
// but in tests is is constructed by hand,
// usually through a [tmconsensustest.StandardFixture].
type Genesis struct {
	ChainID string

	// The height of the first block to be proposed.
	InitialHeight uint64

	// This determines PrevAppStateHash for the first proposed block.
	CurrentAppStateHash []byte

	// The set of validators to propose and vote on the first block.
	ValidatorSet ValidatorSet
}

// AsBlock returns the genesis Block corresponding to g.
// It will have only its Height, NextValidators, and Hash set.
// If there is an error retrieving the hash, that error is returned.
func (g Genesis) Block(h HashScheme) (Block, error) {
	b := Block{
		// Genesis initial height is the height of the first block to propose,
		// so the stored block must be one less.
		Height: g.InitialHeight - 1,

		NextValidatorSet: g.ValidatorSet,
	}

	bh, err := h.Block(b)
	if err != nil {
		return b, fmt.Errorf("failed to calculate genesis block hash: %w", err)
	}

	b.Hash = bh
	return b, nil
}

// ExternalGenesis is a view of the externally defined genesis data,
// sent to the app as part of [tmdriver.InitChainRequest].
type ExternalGenesis struct {
	ChainID string

	// Height to use for the first proposed block.
	InitialHeight uint64

	// Initial application state as specified by the external genesis description.
	// Format is determined by the application; it is opaque to the consensus engine.
	//
	// This is a Reader, not a byte slice, so that the consensus engine
	// isn't forced to load the entire state into memory.
	InitialAppState io.Reader

	// Validators according to the consensus engine's view.
	// Can be overridden in the [tmdriver.InitChainResponse].
	GenesisValidatorSet ValidatorSet
}
