package gp2papi_test

import (
	"context"
	"testing"

	"github.com/rollchains/gordian/gcosmos/gserver/internal/gp2papi"
	"github.com/rollchains/gordian/gcosmos/gserver/internal/gsbd"
	"github.com/rollchains/gordian/internal/gtest"
	"github.com/rollchains/gordian/tm/tmconsensus"
	"github.com/rollchains/gordian/tm/tmconsensus/tmconsensustest"
	"github.com/rollchains/gordian/tm/tmengine/tmelink"
	"github.com/stretchr/testify/require"
)

func TestSyncClient_fullBlock_zeroData(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dhfx := NewFixture(t, ctx)

	fx := tmconsensustest.NewStandardFixture(4)
	dataID := gsbd.DataID(1, 0, 0, nil) // Zero data ID
	ph1 := fx.NextProposedHeader([]byte(dataID), 0)
	fx.SignProposal(ctx, &ph1, 0)

	precommitProofs := fx.PrecommitProofMap(ctx, 1, 0, map[string][]int{
		string(ph1.Header.Hash): {0, 1, 2},
		"":                      {3},
	})
	fx.CommitBlock(ph1.Header, []byte("app_state_1"), 0, precommitProofs)
	nextPH := fx.NextProposedHeader([]byte("whatever"), 0)

	require.NoError(t, dhfx.HeaderStore.SaveHeader(ctx, tmconsensus.CommittedHeader{
		Header: ph1.Header,
		Proof:  nextPH.Header.PrevCommitProof,
	}))

	rhCh := make(chan tmelink.ReplayedHeaderRequest)
	sc := gp2papi.NewSyncClient(
		ctx,
		gtest.NewLogger(t).With("sys", "syncclient"),
		dhfx.P2PClientConn.Host().Libp2pHost(),
		dhfx.Codec,
		rhCh,
	)
	defer sc.Wait()
	defer cancel()

	// Resume request before any peers are added,
	// just to exercise the behavior when blocked on lack of peers.
	require.True(t, sc.ResumeFetching(ctx, 1, 2)) // Fetch Height 1 and stop at 2.

	// Now add the host peer.
	require.True(t, sc.AddPeer(ctx, dhfx.P2PHostConn.Host().Libp2pHost().ID()))

	// Get the response.
	replayReq := gtest.ReceiveSoon(t, rhCh)
	require.Equal(t, ph1.Header, replayReq.Header)
	require.Zero(t, replayReq.Proof.Round)
	require.Equal(t, nextPH.Header.PrevCommitProof.PubKeyHash, replayReq.Proof.PubKeyHash)

	// Signal back to the client that the replay was good.
	gtest.SendSoon(t, replayReq.Resp, tmelink.ReplayedHeaderResponse{})
}
