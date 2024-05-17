package gstress_test

import (
	"context"
	"net/rpc"
	"testing"

	"github.com/rollchains/gordian/cmd/gordian-stress/internal/gstress"
	"github.com/rollchains/gordian/tm/tmconsensus/tmconsensustest"
	"github.com/stretchr/testify/require"
)

func TestSeedRPC_Genesis(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bfx, seedHost := newFixtureWithSeedHost(ctx, t, fixtureConfig{})

	c := bfx.NewBootstrapClient()

	vals := tmconsensustest.DeterministicValidatorsEd25519(2).Vals()
	for _, v := range vals {
		require.NoError(t, c.RegisterValidator(v))
	}

	require.NoError(t, c.SetChainID("my-chain-id"))

	clientHost := bfx.NewSeedClient(ctx, t)

	seedRPCStream, err := clientHost.Libp2pHost().NewStream(
		ctx, seedHost.Libp2pHost().ID(), gstress.SeedServiceProtocolID,
	)
	require.NoError(t, err)

	rpcClient := rpc.NewClient(seedRPCStream)

	resp := new(gstress.RPCGenesisResponse)
	require.NoError(t, rpcClient.Call("SeedRPC.Genesis", gstress.RPCGenesisRequest{}, resp))
	require.Equal(t, "my-chain-id", resp.ChainID)
	require.Equal(t, vals, resp.Validators)
}
