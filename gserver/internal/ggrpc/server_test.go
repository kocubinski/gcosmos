package ggrpc_test

import (
	context "context"
	"testing"

	"github.com/gordian-engine/gcosmos/gserver/internal/ggrpc"
	"github.com/gordian-engine/gcosmos/gserver/internal/ggrpc/ggrpctest"
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
