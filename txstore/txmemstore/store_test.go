package txmemstore_test

import (
	"bytes"
	"context"
	"testing"

	"cosmossdk.io/core/server"
	"cosmossdk.io/core/transaction"
	"github.com/gordian-engine/gcosmos/internal/copy/gtest"
	"github.com/gordian-engine/gcosmos/txstore/txmemstore"
	"github.com/gordian-engine/gcosmos/txstore/txstoretest"
	"github.com/stretchr/testify/require"
)

// TODO: these are literal tests against the actual txmemstore type for now,
// but they will need to get pulled into a set of compliance tests
// so we can confirm behavior of other implementations.

func TestStore_LoadTxByHash(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	log := gtest.NewLogger(t)

	s := txmemstore.NewStore(log)

	fTx := &txstoretest.FakeTx{
		TxBytes: []byte("raw_tx_bytes"),
	}
	_ = append(fTx.TxHash[:0], "fake_tx_hash"...)

	// Overly simplified result for initial prototyping.
	txRes := server.TxResult{
		GasWanted: 100,
		GasUsed:   25,
	}

	require.NoError(
		t,
		s.SaveBlockResult(
			ctx,
			server.BlockRequest[transaction.Tx]{
				Height: 5,
				Txs:    []transaction.Tx{fTx},
			},
			server.BlockResponse{
				TxResults: []server.TxResult{txRes},
			},
		),
	)

	height, txBytes, res, err := s.LoadTxByHash(ctx, fTx.TxHash[:])
	require.NoError(t, err)
	require.Equal(t, uint64(5), height)
	require.True(t, bytes.Equal(txBytes, fTx.Bytes()))
	require.Equal(t, txRes, res)
}

