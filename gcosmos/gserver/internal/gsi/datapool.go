package gsi

import (
	"context"
	"log/slog"

	"cosmossdk.io/core/transaction"
	"github.com/rollchains/gordian/gcosmos/gserver/internal/gsbd"
	"github.com/rollchains/gordian/gdriver/gdatapool"
)

// NewDataPool returns a gcosmos-specific data pool.
func NewDataPool(
	ctx context.Context,
	log *slog.Logger,
	nWorkers int,
	blockDataClient *gsbd.Libp2pClient,
) *gdatapool.Pool[[]transaction.Tx] {
	return gdatapool.New[[]transaction.Tx](
		ctx,
		log,
		nWorkers,

		&libp2pRetriever{
			log: log.With("c_sys", "libp2p_retriever"),
			c:   blockDataClient,
		},
	)
}
