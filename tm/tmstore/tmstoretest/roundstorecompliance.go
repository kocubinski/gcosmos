package tmstoretest

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/rollchains/gordian/gcrypto"
	"github.com/rollchains/gordian/tm/tmconsensus"
	"github.com/rollchains/gordian/tm/tmconsensus/tmconsensustest"
	"github.com/rollchains/gordian/tm/tmstore"
	"github.com/stretchr/testify/require"
)

type RoundStoreFactory func(cleanup func(func())) (tmstore.RoundStore, error)

func TestRoundStoreCompliance(t *testing.T, f RoundStoreFactory) {
	t.Run("nothing stored at height", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		s, err := f(t.Cleanup)
		require.NoError(t, err)

		_, _, _, err = s.LoadRoundState(ctx, 1, 0)
		require.ErrorIs(t, err, tmconsensus.RoundUnknownError{WantHeight: 1, WantRound: 0})

		_, _, _, err = s.LoadRoundState(ctx, 3, 6)
		require.ErrorIs(t, err, tmconsensus.RoundUnknownError{WantHeight: 3, WantRound: 6})
	})

	t.Run("proposed blocks", func(t *testing.T) {
		t.Run("happy path", func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			s, err := f(t.Cleanup)
			require.NoError(t, err)

			fx := tmconsensustest.NewStandardFixture(2)

			ph0 := fx.NextProposedHeader([]byte("val0"), 0)
			fx.SignProposal(ctx, &ph0, 0)
			require.Empty(t, ph0.Header.PrevCommitProof.Proofs)
			ph0.Header.PrevCommitProof.Proofs = nil

			ph1 := fx.NextProposedHeader([]byte("val1"), 1)
			fx.SignProposal(ctx, &ph1, 1)
			require.Empty(t, ph1.Header.PrevCommitProof.Proofs)
			ph1.Header.PrevCommitProof.Proofs = nil

			require.NoError(t, s.SaveRoundProposedHeader(ctx, ph0))
			require.NoError(t, s.SaveRoundProposedHeader(ctx, ph1))

			pbs, _, _, err := s.LoadRoundState(ctx, 1, 0)
			require.NoError(t, err)

			// Using require.ElementsMatch gives poor output on mismatched lists with huge elements.
			// Ensure the two slices are sorted the same.
			want := []tmconsensus.ProposedHeader{ph0, ph1}
			slices.SortFunc(want, func(a, b tmconsensus.ProposedHeader) int {
				return bytes.Compare(a.Header.Hash, b.Header.Hash)
			})
			slices.SortFunc(pbs, func(a, b tmconsensus.ProposedHeader) int {
				return bytes.Compare(a.Header.Hash, b.Header.Hash)
			})
			require.Equal(t, want, pbs)
		})

		t.Run("overwriting an existing proposed block", func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			s, err := f(t.Cleanup)
			require.NoError(t, err)

			fx := tmconsensustest.NewStandardFixture(2)

			ph0 := fx.NextProposedHeader([]byte("val0"), 0)

			// First write succeeds.
			require.NoError(t, s.SaveRoundProposedHeader(ctx, ph0))

			// And a second write is an error.
			err = s.SaveRoundProposedHeader(ctx, ph0)
			require.ErrorIs(t, err, tmstore.OverwriteError{
				Field: "hash",
				Value: fmt.Sprintf("%x", ph0.Header.Hash),
			})
		})
	})

	for _, tc := range []struct {
		typ        string
		proofMapFn func(f *tmconsensustest.StandardFixture) func(
			ctx context.Context,
			height uint64,
			round uint32,
			voteMap map[string][]int, // Map of block hash to prevote, to validator indices.
		) map[string]gcrypto.CommonMessageSignatureProof
		overwriteFn func(s tmstore.RoundStore) func(
			ctx context.Context,
			height uint64,
			round uint32,
			proofs map[string]gcrypto.CommonMessageSignatureProof,
		) error
		choiceFn func(prevotes, precommits map[string]gcrypto.CommonMessageSignatureProof) (
			want map[string]gcrypto.CommonMessageSignatureProof,
		)
	}{
		{
			typ: "prevote",
			proofMapFn: func(f *tmconsensustest.StandardFixture) func(
				ctx context.Context,
				height uint64,
				round uint32,
				voteMap map[string][]int, // Map of block hash to prevote, to validator indices.
			) map[string]gcrypto.CommonMessageSignatureProof {
				return f.PrevoteProofMap
			},
			overwriteFn: func(s tmstore.RoundStore) func(
				ctx context.Context,
				height uint64,
				round uint32,
				proofs map[string]gcrypto.CommonMessageSignatureProof,
			) error {
				return s.OverwriteRoundPrevoteProofs
			},
			choiceFn: func(prevotes, precommits map[string]gcrypto.CommonMessageSignatureProof) (
				want map[string]gcrypto.CommonMessageSignatureProof,
			) {
				return prevotes
			},
		},
		{
			typ: "precommit",
			proofMapFn: func(f *tmconsensustest.StandardFixture) func(
				ctx context.Context,
				height uint64,
				round uint32,
				voteMap map[string][]int, // Map of block hash to prevote, to validator indices.
			) map[string]gcrypto.CommonMessageSignatureProof {
				return f.PrecommitProofMap
			},
			overwriteFn: func(s tmstore.RoundStore) func(
				ctx context.Context,
				height uint64,
				round uint32,
				proofs map[string]gcrypto.CommonMessageSignatureProof,
			) error {
				return s.OverwriteRoundPrecommitProofs
			},
			choiceFn: func(prevotes, precommits map[string]gcrypto.CommonMessageSignatureProof) (
				want map[string]gcrypto.CommonMessageSignatureProof,
			) {
				return precommits
			},
		},
	} {
		tc := tc
		t.Run(tc.typ+" proofs", func(t *testing.T) {
			t.Run("happy path", func(t *testing.T) {
				t.Parallel()

				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()

				s, err := f(t.Cleanup)
				require.NoError(t, err)

				fx := tmconsensustest.NewStandardFixture(2)

				ph := fx.NextProposedHeader([]byte("app_data"), 0)

				// First validator votes for proposed block,
				// other validator votes for nil.
				voteMap := map[string][]int{
					string(ph.Header.Hash): {0},
					"":                     {1},
				}
				proofMap := tc.proofMapFn(fx)(ctx, 1, 0, voteMap)

				err = tc.overwriteFn(s)(ctx, 1, 0, proofMap)
				require.NoError(t, err)

				_, prevotes, precommits, err := s.LoadRoundState(ctx, 1, 0)
				require.NoError(t, err)
				want := tc.choiceFn(prevotes, precommits)
				require.Equal(t, proofMap, want)
			})
		})
	}

	t.Run("full set of round values", func(t *testing.T) {
		t.Run("all present when all set", func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			s, err := f(t.Cleanup)
			require.NoError(t, err)

			fx := tmconsensustest.NewStandardFixture(2)

			ph := fx.NextProposedHeader([]byte("app_data"), 0)
			require.NoError(t, s.SaveRoundProposedHeader(ctx, ph))

			// First validator votes for proposed block,
			// other validator votes for nil.
			voteMap := map[string][]int{
				string(ph.Header.Hash): {0},
				"":                     {1},
			}
			prevoteProofMap := fx.PrevoteProofMap(ctx, 1, 0, voteMap)
			require.NoError(t, s.OverwriteRoundPrevoteProofs(ctx, 1, 0, prevoteProofMap))

			precommitProofMap := fx.PrecommitProofMap(ctx, 1, 0, voteMap)
			require.NoError(t, s.OverwriteRoundPrecommitProofs(ctx, 1, 0, precommitProofMap))

			pbs, prevotes, precommits, err := s.LoadRoundState(ctx, 1, 0)
			require.NoError(t, err)
			require.Equal(t, []tmconsensus.ProposedHeader{ph}, pbs)
			require.Equal(t, prevoteProofMap, prevotes)
			require.Equal(t, precommitProofMap, precommits)
		})

		t.Run("only proposed blocks set", func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			s, err := f(t.Cleanup)
			require.NoError(t, err)

			fx := tmconsensustest.NewStandardFixture(2)

			ph := fx.NextProposedHeader([]byte("app_data"), 0)
			require.NoError(t, s.SaveRoundProposedHeader(ctx, ph))

			pbs, prevotes, precommits, err := s.LoadRoundState(ctx, 1, 0)
			require.NoError(t, err)
			require.Equal(t, []tmconsensus.ProposedHeader{ph}, pbs)
			require.Nil(t, prevotes)
			require.Nil(t, precommits)
		})

		t.Run("only prevotes set", func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			s, err := f(t.Cleanup)
			require.NoError(t, err)

			fx := tmconsensustest.NewStandardFixture(2)

			ph := fx.NextProposedHeader([]byte("app_data"), 0)

			// First validator votes for proposed block,
			// other validator votes for nil.
			voteMap := map[string][]int{
				string(ph.Header.Hash): {0},
				"":                     {1},
			}
			prevoteProofMap := fx.PrevoteProofMap(ctx, 1, 0, voteMap)
			require.NoError(t, s.OverwriteRoundPrevoteProofs(ctx, 1, 0, prevoteProofMap))

			pbs, prevotes, precommits, err := s.LoadRoundState(ctx, 1, 0)
			require.NoError(t, err)
			require.Nil(t, pbs)
			require.Equal(t, prevoteProofMap, prevotes)
			require.Nil(t, precommits)
		})

		t.Run("only precommits set", func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			s, err := f(t.Cleanup)
			require.NoError(t, err)

			fx := tmconsensustest.NewStandardFixture(2)

			ph := fx.NextProposedHeader([]byte("app_data"), 0)

			// First validator votes for proposed block,
			// other validator votes for nil.
			voteMap := map[string][]int{
				string(ph.Header.Hash): {0},
				"":                     {1},
			}

			precommitProofMap := fx.PrecommitProofMap(ctx, 1, 0, voteMap)
			require.NoError(t, s.OverwriteRoundPrecommitProofs(ctx, 1, 0, precommitProofMap))

			pbs, prevotes, precommits, err := s.LoadRoundState(ctx, 1, 0)
			require.NoError(t, err)
			require.Nil(t, pbs)
			require.Nil(t, prevotes)
			require.Equal(t, precommitProofMap, precommits)
		})
	})
}
