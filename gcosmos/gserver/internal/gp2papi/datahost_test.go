package gp2papi_test

import (
	"context"
	"encoding/json"
	"io"
	"testing"

	libp2phost "github.com/libp2p/go-libp2p/core/host"
	libp2pprotocol "github.com/libp2p/go-libp2p/core/protocol"
	"github.com/rollchains/gordian/gcosmos/gserver/internal/gp2papi"
	"github.com/rollchains/gordian/gcrypto"
	"github.com/rollchains/gordian/internal/gtest"
	"github.com/rollchains/gordian/tm/tmcodec/tmjson"
	"github.com/rollchains/gordian/tm/tmconsensus"
	"github.com/rollchains/gordian/tm/tmconsensus/tmconsensustest"
	"github.com/rollchains/gordian/tm/tmp2p/tmlibp2p/tmlibp2ptest"
	"github.com/rollchains/gordian/tm/tmstore/tmmemstore"
	"github.com/stretchr/testify/require"
)

func TestDataHost_serveCommittedHeader(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// tmlibp2ptest probably isn't exactly the right network setup to use,
	// as it does extra work for consensus message setup;
	// but it does all the other heavy lifting we need,
	// and it's already written, so we are using it here.
	log := gtest.NewLogger(t)
	net, err := tmlibp2ptest.NewNetwork(ctx, log.With("sys", "net"), tmjson.MarshalCodec{
		// I'm a little surprised this doesn't fail when we omit a registry.
		// I guess it's okay since we don't attempt to marshal any consensus messages here.
	})
	require.NoError(t, err)
	defer net.Wait()
	defer cancel()

	host, err := net.Connect(ctx)
	require.NoError(t, err)

	client, err := net.Connect(ctx)
	require.NoError(t, err)

	require.NoError(t, net.Stabilize(ctx))

	// Set up a fixture to put something in the header store.
	fx := tmconsensustest.NewStandardFixture(4)
	ph1 := fx.NextProposedHeader([]byte("app_data_1"), 0)
	fx.SignProposal(ctx, &ph1, 0)

	precommitProofs := fx.PrecommitProofMap(ctx, 1, 0, map[string][]int{
		string(ph1.Header.Hash): {0, 1, 2},
		"":                      {3},
	})
	fx.CommitBlock(ph1.Header, []byte("app_state_1"), 0, precommitProofs)
	nextPH := fx.NextProposedHeader([]byte("whatever"), 0)

	hs := tmmemstore.NewHeaderStore()
	require.NoError(t, hs.SaveHeader(ctx, tmconsensus.CommittedHeader{
		Header: ph1.Header,
		Proof:  nextPH.Header.PrevCommitProof,
	}))

	reg := new(gcrypto.Registry)
	gcrypto.RegisterEd25519(reg)

	codec := tmjson.MarshalCodec{CryptoRegistry: reg}
	dh := gp2papi.NewDataHost(
		ctx, log,
		host.Host().Libp2pHost(),
		hs,
		codec,
	)

	// We aren't doing anything with the data host value yet.
	// We could potentially allow it to be removed from the host on a context cancellation.
	// Other than that, there are not yet really any obvious uses for the returned value.
	_ = dh

	hostInfo := libp2phost.InfoFromHost(host.Host().Libp2pHost())
	require.NoError(t, client.Host().Libp2pHost().Connect(ctx, *hostInfo))

	s, err := client.Host().Libp2pHost().NewStream(ctx, hostInfo.ID, libp2pprotocol.ID(
		"/gcosmos/committed_headers/v1/height/1",
	))
	require.NoError(t, err)
	defer s.Close()

	b, err := io.ReadAll(s)
	require.NoError(t, err)

	var jr gp2papi.JSONResult
	require.NoError(t, json.Unmarshal(b, &jr))
	require.Empty(t, jr.Err)

	var ch tmconsensus.CommittedHeader
	require.NoError(t, codec.UnmarshalCommittedHeader(jr.Result, &ch))

	require.Equal(t, tmconsensus.CommittedHeader{
		Header: ph1.Header,
		Proof:  nextPH.Header.PrevCommitProof,
	}, ch)

	// And if we try to get a header that hasn't been written, it errors.
	s, err = client.Host().Libp2pHost().NewStream(ctx, hostInfo.ID, libp2pprotocol.ID(
		"/gcosmos/committed_headers/v1/height/30",
	))
	require.NoError(t, err)
	defer s.Close()

	b, err = io.ReadAll(s)
	require.NoError(t, err)
	jr = gp2papi.JSONResult{}
	require.NoError(t, json.Unmarshal(b, &jr))
	require.Empty(t, jr.Result)
	require.NotEmpty(t, jr.Err)
}
