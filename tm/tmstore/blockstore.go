package tmstore

import (
	"context"

	"github.com/rollchains/gordian/tm/tmconsensus"
)

// BlockStore is the store that the Engine's Mirror uses for committed blocks.
// The committed blocks always lag the voting round by two heights.
type BlockStore interface {
	SaveBlock(ctx context.Context, cb tmconsensus.CommittedBlock) error

	LoadBlock(ctx context.Context, height uint64) (tmconsensus.CommittedBlock, error)
}
