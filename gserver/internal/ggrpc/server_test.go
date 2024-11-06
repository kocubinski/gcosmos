package ggrpc_test

import (
	"bytes"
	context "context"
	"testing"

	"cosmossdk.io/core/server"
	"cosmossdk.io/core/transaction"
	"github.com/gordian-engine/gcosmos/gserver/internal/ggrpc"
	"github.com/gordian-engine/gcosmos/gserver/internal/ggrpc/ggrpctest"
	"github.com/gordian-engine/gcosmos/txstore/txstoretest"
	"github.com/stretchr/testify/require"
)

func TestGRPCServer_GetBlocksWatermark(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fx := ggrpctest.NewFixture(t, ctx)
	defer cancel()

	require.NoError(t, fx.MirrorStore.SetNetworkHeightRound(
		ctx,
		10, 1,
		9, 3,
	))

	resp, err := fx.Client.GetBlocksWatermark(ctx, new(ggrpc.CurrentBlockRequest))
	require.NoError(t, err)

	require.Equal(t, uint64(10), resp.VotingHeight)
	require.Equal(t, uint32(1), resp.VotingRound)
	require.Equal(t, uint64(9), resp.CommittingHeight)
	require.Equal(t, uint32(3), resp.CommittingRound)
}

func TestGRPCServer_QueryTx(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fx := ggrpctest.NewFixture(t, ctx)
	defer cancel()

	fTx := &txstoretest.FakeTx{
		TxBytes: []byte("raw_tx_bytes"),
	}
	_ = append(fTx.TxHash[:0], "fake_tx_hash"...)

	// Overly simplified result for initial prototyping.
	txRes := server.TxResult{
		GasWanted: 100,
		GasUsed:   25,
	}

	fx.TxStore.SaveBlockResult(
		ctx,
		server.BlockRequest[transaction.Tx]{
			Height: 5,
			Txs:    []transaction.Tx{fTx},
		},
		server.BlockResponse{
			TxResults: []server.TxResult{txRes},
		},
	)

	resp, err := fx.Client.QueryTx(ctx, &ggrpc.SDKQueryTxRequest{
		TxHash: fTx.TxHash[:],
	})
	require.NoError(t, err)

	require.True(t, bytes.Equal(fTx.TxHash[:], resp.TxHash))
	require.Equal(t, int64(5), resp.Height) // Annoying uint -> int cast.

	// TODO: more assertions against returned repsonse.
	require.Equal(t, txRes.GasWanted, resp.Result.GasWanted)
	require.Equal(t, txRes.GasUsed, resp.Result.GasUsed)

	require.True(t, bytes.Equal(fTx.TxBytes, resp.TxBytes))
}
