package tmstoretest

import (
	"context"
	"testing"

	"github.com/rollchains/gordian/tm/tmconsensus"
	"github.com/rollchains/gordian/tm/tmconsensus/tmconsensustest"
	"github.com/rollchains/gordian/tm/tmstore"
	"github.com/stretchr/testify/require"
)

type HeaderStoreFactory func(cleanup func(func())) (tmstore.HeaderStore, error)

func TestHeaderStoreCompliance(t *testing.T, f HeaderStoreFactory) {
	t.Run("happy path", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		s, err := f(t.Cleanup)
		require.NoError(t, err)

		fx := tmconsensustest.NewStandardFixture(4)

		ph1 := fx.NextProposedHeader([]byte("app_data_1"), 0)
		ph1.Header.PrevAppStateHash = []byte("initial_app_state") // TODO: this should be automatically set in the fixture.

		// This tends to be nil from the store.
		// Ensure it is nil here too.
		require.Empty(t, ph1.Header.PrevCommitProof.Proofs)
		ph1.Header.PrevCommitProof.Proofs = nil

		fx.RecalculateHash(&ph1.Header)
		fx.SignProposal(ctx, &ph1, 0)

		voteMap := map[string][]int{
			string(ph1.Header.Hash): {0, 1, 2},
			"":                      {3},
		}
		precommitProofs1 := fx.PrecommitProofMap(ctx, 1, 0, voteMap)
		fx.CommitBlock(ph1.Header, []byte("app_state_height_1"), 0, precommitProofs1)

		ph2 := fx.NextProposedHeader([]byte("app_data_2"), 0)

		ch := tmconsensus.CommittedHeader{
			Header: ph1.Header,
			Proof:  ph2.Header.PrevCommitProof,
		}

		require.NoError(t, s.SaveHeader(ctx, ch))

		got, err := s.LoadHeader(ctx, 1)
		require.NoError(t, err)

		require.Equal(t, ch, got)
	})
}
