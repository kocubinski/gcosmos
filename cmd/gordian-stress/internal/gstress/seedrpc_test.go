package gstress_test

import (
	"context"
	"net/rpc"
	"testing"

	"github.com/rollchains/gordian/cmd/gordian-stress/internal/gstress"
	"github.com/rollchains/gordian/internal/gtest"
	"github.com/rollchains/gordian/tm/tmconsensus/tmconsensustest"
	"github.com/stretchr/testify/require"
)

func TestSeedRPC_Genesis_startFirst(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bfx, seedHost := newFixtureWithSeedHost(ctx, t, fixtureConfig{})

	c := bfx.NewBootstrapClient()

	vals := tmconsensustest.DeterministicValidatorsEd25519(2).Vals()
	for _, v := range vals {
		require.NoError(t, c.RegisterValidator(v))
	}

	require.NoError(t, c.Start())

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

func TestSeedRPC_Genesis_blocksUntilStart(t *testing.T) {
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

	// Open two client streams without calling Start.
	clientHost1 := bfx.NewSeedClient(ctx, t)
	clientHost2 := bfx.NewSeedClient(ctx, t)

	seedRPCStream1, err := clientHost1.Libp2pHost().NewStream(
		ctx, seedHost.Libp2pHost().ID(), gstress.SeedServiceProtocolID,
	)
	require.NoError(t, err)

	seedRPCStream2, err := clientHost2.Libp2pHost().NewStream(
		ctx, seedHost.Libp2pHost().ID(), gstress.SeedServiceProtocolID,
	)
	require.NoError(t, err)

	rpcClient1 := rpc.NewClient(seedRPCStream1)
	resp1 := new(gstress.RPCGenesisResponse)
	call1 := rpcClient1.Go("SeedRPC.Genesis", gstress.RPCGenesisRequest{}, resp1, nil)

	rpcClient2 := rpc.NewClient(seedRPCStream2)
	resp2 := new(gstress.RPCGenesisResponse)
	call2 := rpcClient2.Go("SeedRPC.Genesis", gstress.RPCGenesisRequest{}, resp2, nil)

	// The async calls are outstanding.
	gtest.NotSending(t, call1.Done)
	gtest.NotSending(t, call2.Done)

	require.NoError(t, c.Start())

	// Now the calls complete rapidly.
	call1 = gtest.ReceiveSoon(t, call1.Done)
	call2 = gtest.ReceiveSoon(t, call2.Done)

	// And they completed without error.
	require.NoError(t, call1.Error)
	require.NoError(t, call2.Error)

	// They both have the correct values.
	require.Equal(t, "my-chain-id", resp1.ChainID)
	require.Equal(t, vals, resp1.Validators)
	require.Equal(t, "my-chain-id", resp2.ChainID)
	require.Equal(t, vals, resp2.Validators)
}
