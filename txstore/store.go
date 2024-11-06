package txstore

import (
	"context"

	"cosmossdk.io/core/server"
	"cosmossdk.io/core/transaction"
)

// Store stores and retrieves transactions at an SDK level of abstraction.
type Store interface {
	SaveBlockResult(
		ctx context.Context,
		req server.BlockRequest[transaction.Tx],
		resp server.BlockResponse,
	) error

	LoadTxByHash(ctx context.Context, hash []byte) (
		height uint64,
		txBytes []byte,
		result server.TxResult,
		err error,
	)
}
