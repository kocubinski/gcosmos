package tmstoretest

import (
	"context"
	"testing"

	"github.com/rollchains/gordian/tm/tmconsensus"
	"github.com/rollchains/gordian/tm/tmconsensus/tmconsensustest"
	"github.com/rollchains/gordian/tm/tmstore"
	"github.com/stretchr/testify/require"
)

type ActionStoreFactory func(cleanup func(func())) (tmstore.ActionStore, error)

func TestActionStoreCompliance(t *testing.T, f ActionStoreFactory) {
	t.Run("proposed blocks", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		s, err := f(t.Cleanup)
		require.NoError(t, err)

		fx := tmconsensustest.NewStandardFixture(2)
		pb1 := fx.NextProposedBlock([]byte("app_data_1"), 0)
		pb1.Round = 2 // Arbitrary, test nonzero round.

		fx.RecalculateHash(&pb1.Block)
		fx.SignProposal(ctx, &pb1, 0)

		require.NoError(t, s.SaveProposedBlock(ctx, pb1))

		t.Run("round trip", func(t *testing.T) {
			ra, err := s.Load(ctx, 1, 2)
			require.NoError(t, err)

			require.Equal(t, uint64(1), ra.Height)
			require.Equal(t, uint32(2), ra.Round)
			require.Equal(t, ra.ProposedBlock, pb1)
		})

		t.Run("double save rejected with same input", func(t *testing.T) {
			err := s.SaveProposedBlock(ctx, pb1)
			require.ErrorIs(t, err, tmstore.DoubleActionError{Type: "proposed block"})
		})

		t.Run("double save rejected with different input", func(t *testing.T) {
			otherPB := fx.NextProposedBlock([]byte("different_app_data"), 0)

			otherPB.Round = 2
			fx.RecalculateHash(&otherPB.Block)
			fx.SignProposal(ctx, &otherPB, 0)

			err := s.SaveProposedBlock(ctx, otherPB)
			require.ErrorIs(t, err, tmstore.DoubleActionError{Type: "proposed block"})
		})
	})

	t.Run("prevotes", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		s, err := f(t.Cleanup)
		require.NoError(t, err)

		fx := tmconsensustest.NewStandardFixture(2)
		pb1 := fx.NextProposedBlock([]byte("app_data_1"), 0)
		pb1.Round = 2

		fx.RecalculateHash(&pb1.Block)
		fx.SignProposal(ctx, &pb1, 0)

		vt := tmconsensus.VoteTarget{
			Height:    1,
			Round:     2,
			BlockHash: string(pb1.Block.Hash),
		}
		sig := fx.PrevoteSignature(ctx, vt, 0)

		pubKey := fx.ValidatorPubKey(0)
		require.NoError(t, s.SavePrevote(ctx, pubKey, vt, sig))

		t.Run("round trip", func(t *testing.T) {
			ra, err := s.Load(ctx, 1, 2)
			require.NoError(t, err)

			require.Equal(t, uint64(1), ra.Height)
			require.Equal(t, uint32(2), ra.Round)

			require.True(t, pubKey.Equal(ra.PubKey))
			require.Equal(t, vt.BlockHash, ra.PrevoteTarget)
			require.Equal(t, string(sig), ra.PrevoteSignature)
		})

		t.Run("double save rejected with same signature", func(t *testing.T) {
			err := s.SavePrevote(ctx, pubKey, vt, sig)
			require.ErrorIs(t, err, tmstore.DoubleActionError{Type: "prevote"})
		})

		t.Run("double save rejected with different signature", func(t *testing.T) {
			nilVT := tmconsensus.VoteTarget{Height: 1, Round: 2}
			nilSig := fx.PrevoteSignature(ctx, nilVT, 0)
			err := s.SavePrevote(ctx, pubKey, nilVT, nilSig)
			require.ErrorIs(t, err, tmstore.DoubleActionError{Type: "prevote"})
		})
	})

	t.Run("precommits", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		s, err := f(t.Cleanup)
		require.NoError(t, err)

		fx := tmconsensustest.NewStandardFixture(2)
		pb1 := fx.NextProposedBlock([]byte("app_data_1"), 0)
		pb1.Round = 2

		fx.RecalculateHash(&pb1.Block)
		fx.SignProposal(ctx, &pb1, 0)

		vt := tmconsensus.VoteTarget{
			Height:    1,
			Round:     2,
			BlockHash: string(pb1.Block.Hash),
		}
		sig := fx.PrecommitSignature(ctx, vt, 0)

		pubKey := fx.ValidatorPubKey(0)
		require.NoError(t, s.SavePrecommit(ctx, pubKey, vt, sig))

		t.Run("round trip", func(t *testing.T) {
			ra, err := s.Load(ctx, 1, 2)
			require.NoError(t, err)

			require.Equal(t, uint64(1), ra.Height)
			require.Equal(t, uint32(2), ra.Round)

			require.True(t, pubKey.Equal(ra.PubKey))
			require.Equal(t, vt.BlockHash, ra.PrecommitTarget)
			require.Equal(t, string(sig), ra.PrecommitSignature)
		})

		t.Run("double save rejected with same signature", func(t *testing.T) {
			err := s.SavePrecommit(ctx, pubKey, vt, sig)
			require.ErrorIs(t, err, tmstore.DoubleActionError{Type: "precommit"})
		})

		t.Run("double save rejected with different signature", func(t *testing.T) {
			nilVT := tmconsensus.VoteTarget{Height: 1, Round: 2}
			nilSig := fx.PrecommitSignature(ctx, nilVT, 0)
			err := s.SavePrecommit(ctx, pubKey, nilVT, nilSig)
			require.ErrorIs(t, err, tmstore.DoubleActionError{Type: "precommit"})
		})
	})

	t.Run("pub key change", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		fx := tmconsensustest.NewStandardFixture(2)
		pb1 := fx.NextProposedBlock([]byte("app_data_1"), 0)
		pb1.Round = 2

		fx.RecalculateHash(&pb1.Block)
		fx.SignProposal(ctx, &pb1, 0)

		vt := tmconsensus.VoteTarget{
			Height:    1,
			Round:     2,
			BlockHash: string(pb1.Block.Hash),
		}
		prevoteSig := fx.PrevoteSignature(ctx, vt, 0)
		prevotePubKey := fx.ValidatorPubKey(0)

		precommitSig := fx.PrecommitSignature(ctx, vt, 1)
		precommitPubKey := fx.ValidatorPubKey(1)

		t.Run("rejected when adding precommit", func(t *testing.T) {
			s, err := f(t.Cleanup)
			require.NoError(t, err)

			require.NoError(t, s.SavePrevote(ctx, prevotePubKey, vt, prevoteSig))
			err = s.SavePrecommit(ctx, precommitPubKey, vt, precommitSig)
			require.ErrorIs(t, err, tmstore.PubKeyChangedError{
				ActionType: "precommit",
				Want:       string(prevotePubKey.PubKeyBytes()),
				Got:        string(precommitPubKey.PubKeyBytes()),
			})
		})

		t.Run("rejected when adding prevote", func(t *testing.T) {
			// Unclear if there is any use case for recording precommit before prevote,
			// but we can still test it anyway.

			s, err := f(t.Cleanup)
			require.NoError(t, err)

			require.NoError(t, s.SavePrecommit(ctx, precommitPubKey, vt, precommitSig))
			err = s.SavePrevote(ctx, prevotePubKey, vt, prevoteSig)
			require.ErrorIs(t, err, tmstore.PubKeyChangedError{
				ActionType: "prevote",
				Want:       string(precommitPubKey.PubKeyBytes()),
				Got:        string(prevotePubKey.PubKeyBytes()),
			})
		})
	})

	t.Run("Load", func(t *testing.T) {
		t.Run("happy path", func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			s, err := f(t.Cleanup)
			require.NoError(t, err)

			fx := tmconsensustest.NewStandardFixture(2)
			pb1 := fx.NextProposedBlock([]byte("app_data_1"), 0)
			pb1.Round = 2 // Arbitrary, test nonzero round.

			fx.RecalculateHash(&pb1.Block)
			fx.SignProposal(ctx, &pb1, 0)

			require.NoError(t, s.SaveProposedBlock(ctx, pb1))

			vt := tmconsensus.VoteTarget{
				Height:    1,
				Round:     2,
				BlockHash: string(pb1.Block.Hash),
			}

			pubKey := fx.ValidatorPubKey(0)
			prevoteSig := fx.PrevoteSignature(ctx, vt, 0)
			precommitSig := fx.PrecommitSignature(ctx, vt, 0)

			require.NoError(t, s.SavePrevote(ctx, pubKey, vt, prevoteSig))
			require.NoError(t, s.SavePrecommit(ctx, pubKey, vt, precommitSig))

			ra, err := s.Load(ctx, 1, 2)
			require.NoError(t, err)

			require.Equal(t, uint64(1), ra.Height)
			require.Equal(t, uint32(2), ra.Round)
			require.Equal(t, ra.ProposedBlock, pb1)
			require.True(t, pubKey.Equal(ra.PubKey))

			require.Equal(t, vt.BlockHash, ra.PrevoteTarget)
			require.Equal(t, string(prevoteSig), ra.PrevoteSignature)

			require.Equal(t, vt.BlockHash, ra.PrecommitTarget)
			require.Equal(t, string(precommitSig), ra.PrecommitSignature)
		})

		t.Run("returns RoundUnknownError on invalid round", func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			s, err := f(t.Cleanup)
			require.NoError(t, err)

			_, err = s.Load(ctx, 1, 2)
			require.ErrorIs(t, err, tmconsensus.RoundUnknownError{
				WantHeight: 1,
				WantRound:  2,
			})
		})
	})
}
