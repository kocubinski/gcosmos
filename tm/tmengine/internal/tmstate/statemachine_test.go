package tmstate_test

import (
	"context"
	"slices"
	"testing"

	"github.com/rollchains/gordian/gcrypto"
	"github.com/rollchains/gordian/internal/gtest"
	"github.com/rollchains/gordian/tm/tmapp"
	"github.com/rollchains/gordian/tm/tmconsensus"
	"github.com/rollchains/gordian/tm/tmengine/internal/tmeil"
	"github.com/rollchains/gordian/tm/tmengine/internal/tmstate/tmstatetest"
	"github.com/stretchr/testify/require"
)

func TestStateMachine_initialization(t *testing.T) {
	t.Run("initial action set at genesis", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		sfx := tmstatetest.NewFixture(t, 2)

		// No extra data in any stores, so we are from a natural 1/0 genesis.

		sm := sfx.NewStateMachine(ctx)
		defer sm.Wait()
		defer cancel()

		// The state machine sends its initial action set at 1/0.
		as := gtest.ReceiveSoon(t, sfx.ToMirrorCh)
		require.Equal(t, uint64(1), as.H)
		require.Zero(t, as.R)

		// The pubkey matches the signer.
		require.True(t, sfx.Fx.PrivVals[0].Signer.PubKey().Equal(as.PubKey))

		// The created Actions channel is 3-buffered so sends from the state machine do not block.
		require.Equal(t, 3, cap(as.Actions))

		// And the response is 1-buffered so the kernel does not block in sending its response.
		require.Equal(t, 1, cap(as.StateResponse))
	})

	t.Run("empty round view response from mirror", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		sfx := tmstatetest.NewFixture(t, 2)

		sm := sfx.NewStateMachine(ctx)
		defer sm.Wait()
		defer cancel()

		as := gtest.ReceiveSoon(t, sfx.ToMirrorCh)

		// Set up consensus strategy expectation before mocking the response.
		cStrat := sfx.CStrat
		enterCh := cStrat.ExpectEnterRound(1, 0, nil)

		// Channel is 1-buffered, don't have to select.
		vrv := sfx.EmptyVRV(1, 0)
		as.StateResponse <- tmeil.StateUpdate{VRV: vrv} // No PrevBlockHash at initial height.

		// And since we sent a VRV, the state machine calls into consensus strategy,
		// with vrv's RoundView.
		erc := gtest.ReceiveSoon(t, enterCh)
		require.Equal(t, vrv.RoundView, erc.RV)

		// Now, if the consensus strategy were to send a proposed block,
		// the state machine would pass it on to the mirror.
		p := tmconsensus.Proposal{
			AppDataID: "foobar",
		}
		gtest.SendSoon(t, erc.ProposalOut, p)

		// Sending the proposal causes a corresponding proposed block action to the mirror.
		action := gtest.ReceiveSoon(t, as.Actions)
		require.Empty(t, action.Prevote.Sig)
		require.Empty(t, action.Precommit.Sig)

		expPB := sfx.Fx.NextProposedBlock([]byte("foobar"), 0)
		sfx.Fx.SignProposal(ctx, &expPB, 0)
		require.Equal(t, expPB, action.PB)
	})

	t.Run("round view response from mirror when a proposed block is present", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		sfx := tmstatetest.NewFixture(t, 4)

		sm := sfx.NewStateMachine(ctx)
		defer sm.Wait()
		defer cancel()

		as := gtest.ReceiveSoon(t, sfx.ToMirrorCh)

		// Set up consensus strategy expectation before mocking the response.
		cStrat := sfx.CStrat
		enterCh := cStrat.ExpectEnterRound(1, 0, nil)

		// Channel is 1-buffered, don't have to select.
		vrv := sfx.EmptyVRV(1, 0)
		as.StateResponse <- tmeil.StateUpdate{VRV: vrv}

		// And since we sent a VRV, the state machine calls into consensus strategy,
		// with vrv's RoundView.
		erc := gtest.ReceiveSoon(t, enterCh)
		require.Equal(t, vrv.RoundView, erc.RV)

		pb1 := sfx.Fx.NextProposedBlock([]byte("app_data_1"), 1)
		sfx.Fx.SignProposal(ctx, &pb1, 1)
		vrv.ProposedBlocks = []tmconsensus.ProposedBlock{pb1}
		vrv.Version++

		// Sending an updated set of proposed blocks...
		gtest.SendSoon(t, sfx.RoundViewInCh, vrv)

		// ... forces the consensus strategy to choose from the available proposed blocks.
		pbReq := gtest.ReceiveSoon(t, cStrat.ConsiderProposedBlockRequests)
		require.Equal(t, []tmconsensus.ProposedBlock{pb1}, pbReq.Input)
	})

	t.Run("round view response from mirror where network is in minority prevote", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		sfx := tmstatetest.NewFixture(t, 4)

		sm := sfx.NewStateMachine(ctx)
		defer sm.Wait()
		defer cancel()

		as := gtest.ReceiveSoon(t, sfx.ToMirrorCh)

		vrv := sfx.EmptyVRV(1, 0)
		pb1 := sfx.Fx.NextProposedBlock([]byte("app_data_1"), 1)
		vrv.ProposedBlocks = []tmconsensus.ProposedBlock{pb1}
		vrv = sfx.Fx.UpdateVRVPrevotes(ctx, vrv, map[string][]int{
			string(pb1.Block.Hash): {1, 2},
		})

		// Set up consensus strategy expectation before mocking the response.
		cStrat := sfx.CStrat
		enterCh := cStrat.ExpectEnterRound(1, 0, nil)

		// Channel is 1-buffered, don't have to select.
		as.StateResponse <- tmeil.StateUpdate{VRV: vrv}

		// And since we sent a VRV, the state machine calls into consensus strategy,
		// with vrv's RoundView.
		erc := gtest.ReceiveSoon(t, enterCh)
		require.Equal(t, vrv.RoundView, erc.RV)

		// And it forces the consensus strategy to choose from the available proposed blocks.
		pbReq := gtest.ReceiveSoon(t, cStrat.ChooseProposedBlockRequests)
		require.Equal(t, []tmconsensus.ProposedBlock{pb1}, pbReq.Input)

		// Once the consensus strategy chooses a hash...
		gtest.SendSoon(t, pbReq.ChoiceHash, string(pb1.Block.Hash))

		// The state machine constructs a valid vote, saves it to the action store,
		// and sends it to the mirror.
		// We check the mirror send first as a synchronization point.
		action := gtest.ReceiveSoon(t, as.Actions)
		// Only prevote is set.
		require.Empty(t, action.PB.Block.Hash)
		require.Empty(t, action.Precommit.Sig)

		require.Equal(t, string(pb1.Block.Hash), action.Prevote.TargetHash)

		// Ensure the signature is valid, too.
		signContent, err := tmconsensus.PrevoteSignBytes(tmconsensus.VoteTarget{
			Height: 1, Round: 0,
			BlockHash: string(pb1.Block.Hash),
		}, sfx.Fx.SignatureScheme)
		require.NoError(t, err)
		require.Equal(t, signContent, action.Prevote.SignContent)
		require.True(t, sfx.Cfg.Signer.PubKey().Verify(signContent, action.Prevote.Sig))
	})

	t.Run("as follower, ready to commit nil", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		sfx := tmstatetest.NewFixture(t, 4)
		sfx.Cfg.Signer = nil

		sm := sfx.NewStateMachine(ctx)
		defer sm.Wait()
		defer cancel()

		as := gtest.ReceiveSoon(t, sfx.ToMirrorCh)

		vrv := sfx.EmptyVRV(1, 0)
		pb1 := sfx.Fx.NextProposedBlock([]byte("app_data_1"), 3)
		vrv.ProposedBlocks = []tmconsensus.ProposedBlock{pb1}

		// Everyone prevoted for the block.
		vrv = sfx.Fx.UpdateVRVPrevotes(ctx, vrv, map[string][]int{
			string(pb1.Block.Hash): {0, 1, 2, 3},
		})

		// But there was only one precommit for it, perhaps due to some network lag.
		vrv = sfx.Fx.UpdateVRVPrecommits(ctx, vrv, map[string][]int{
			string(pb1.Block.Hash): {3},
			"":                     {0, 1, 2},
		})

		cStrat := sfx.CStrat
		enterCh := cStrat.ExpectEnterRound(1, 0, nil)

		// Channel is 1-buffered, don't have to select.
		as.StateResponse <- tmeil.StateUpdate{VRV: vrv}

		// Immediately after entering the round, the state machine advances to the next round due to the nil precommit.
		erc := gtest.ReceiveSoon(t, enterCh)
		require.Equal(t, vrv.RoundView, erc.RV)

		as11 := gtest.ReceiveSoon(t, sfx.ToMirrorCh)
		require.Equal(t, uint64(1), as11.H)
		require.Equal(t, uint32(1), as11.R)

		// This will call EnterRound(1, 1).
		enterCh = cStrat.ExpectEnterRound(1, 1, nil)

		emptyVRV11 := sfx.EmptyVRV(1, 1)
		as11.StateResponse <- tmeil.StateUpdate{VRV: emptyVRV11}

		erc = gtest.ReceiveSoon(t, enterCh)
		require.Equal(t, emptyVRV11.RoundView, erc.RV)
	})

	t.Run("committed block response from mirror", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		sfx := tmstatetest.NewFixture(t, 4)

		sm := sfx.NewStateMachine(ctx)
		defer sm.Wait()
		defer cancel()

		as := gtest.ReceiveSoon(t, sfx.ToMirrorCh)

		// We don't need a negative assertion on the consensus strategy,
		// because it would panic if called without an ExpectEnterRound.

		pb1 := sfx.Fx.NextProposedBlock([]byte("app_data_1"), 1) // Not 0, the signer for the state machine.
		vt := tmconsensus.VoteTarget{Height: 1, Round: 0, BlockHash: string(pb1.Block.Hash)}
		sfx.Fx.CommitBlock(pb1.Block, []byte("app_state_1"), 0, map[string]gcrypto.CommonMessageSignatureProof{
			string(pb1.Block.Hash): sfx.Fx.PrecommitSignatureProof(ctx, vt, nil, []int{1, 2, 3}), // The other 3/4 validators.
		})

		pb2 := sfx.Fx.NextProposedBlock([]byte("app_data_2"), 1)

		as.StateResponse <- tmeil.StateUpdate{
			CB: tmconsensus.CommittedBlock{
				Block: pb1.Block,
				Proof: pb2.Block.PrevCommitProof,
			},
		}

		// Now the state machine should make a finalize block request.
		req := gtest.ReceiveSoon(t, sfx.FinalizeBlockRequests)

		require.NotNil(t, req.Ctx)
		require.NoError(t, req.Ctx.Err())
		require.Equal(t, pb1.Block, req.Block)
		require.Zero(t, req.Round)

		require.Equal(t, 1, cap(req.Resp))

		resp := tmapp.FinalizeBlockResponse{
			Height: 1, Round: 0,
			BlockHash: pb1.Block.Hash,

			Validators: sfx.Fx.Vals(),

			AppStateHash: []byte("app_state_1"),
		}
		gtest.SendSoon(t, req.Resp, resp)

		t.Skip("TODO: assert entry in finalization store, updated round action set sent to mirror")
	})
}

func TestStateMachine_decidePrecommit(t *testing.T) {
	t.Run("majority prevotes at initialization", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		sfx := tmstatetest.NewFixture(t, 4)

		sm := sfx.NewStateMachine(ctx)
		defer sm.Wait()
		defer cancel()

		as := gtest.ReceiveSoon(t, sfx.ToMirrorCh)

		vrv := sfx.EmptyVRV(1, 0)
		pb1 := sfx.Fx.NextProposedBlock([]byte("app_data_1"), 1)
		vrv.ProposedBlocks = []tmconsensus.ProposedBlock{pb1}
		vrv = sfx.Fx.UpdateVRVPrevotes(ctx, vrv, map[string][]int{
			string(pb1.Block.Hash): {1, 2, 3}, // Everyone else already prevoted for the block.
		})

		// Set up consensus strategy expectation before mocking the response.
		cStrat := sfx.CStrat
		_ = cStrat.ExpectEnterRound(1, 0, nil)

		// Channel is 1-buffered, don't have to select.
		as.StateResponse <- tmeil.StateUpdate{VRV: vrv}

		// Since there was already a majority prevote for a block,
		// we don't need to submit our prevote -- we jump straight into the precommit decision.
		cReq := gtest.ReceiveSoon(t, cStrat.DecidePrecommitRequests)
		require.Equal(t, vrv.VoteSummary, cReq.Input)

		// And when the consensus strategy responds, the state machine forwards it to the mirror.
		gtest.SendSoon(t, cReq.ChoiceHash, string(pb1.Block.Hash))

		act := gtest.ReceiveSoon(t, as.Actions)
		require.NotEmpty(t, act.Precommit.Sig)

		precommitSignContent, err := tmconsensus.PrecommitSignBytes(tmconsensus.VoteTarget{
			Height: 1, Round: 0,
			BlockHash: string(pb1.Block.Hash),
		}, sfx.Fx.SignatureScheme)
		require.NoError(t, err)
		require.Equal(t, precommitSignContent, act.Precommit.SignContent)
		require.True(t, sfx.Cfg.Signer.PubKey().Verify(act.Precommit.SignContent, act.Precommit.Sig))

		// And at that point, it is present in the action store too.
		ra, err := sfx.Cfg.ActionStore.Load(ctx, 1, 0)
		require.NoError(t, err)
		require.Equal(t, string(pb1.Block.Hash), ra.PrecommitTarget)
		require.Equal(t, string(act.Precommit.Sig), ra.PrecommitSignature)
	})

	t.Run("after prevote delay elapses", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		sfx := tmstatetest.NewFixture(t, 4)

		sm := sfx.NewStateMachine(ctx)
		defer sm.Wait()
		defer cancel()

		as := gtest.ReceiveSoon(t, sfx.ToMirrorCh)

		// Set up consensus strategy expectation before mocking the response.
		cStrat := sfx.CStrat
		_ = cStrat.ExpectEnterRound(1, 0, nil)

		prevoteDelayTimerStarted := sfx.RoundTimer.PrevoteDelayStartNotification(1, 0)

		// Initial state has the proposed block.
		vrv := sfx.EmptyVRV(1, 0)
		pb1 := sfx.Fx.NextProposedBlock([]byte("app_data_1"), 3)
		vrv.ProposedBlocks = []tmconsensus.ProposedBlock{pb1}
		// Channel is 1-buffered, don't have to select.
		as.StateResponse <- tmeil.StateUpdate{VRV: vrv} // No PrevBlockHash at initial height.

		// This causes a Consider request to the consensus strategy,
		// and we will prevote for the block.
		considerReq := gtest.ReceiveSoon(t, cStrat.ConsiderProposedBlockRequests)
		gtest.SendSoon(t, considerReq.ChoiceHash, string(pb1.Block.Hash))

		// The choice is sent to the mirror as an action.
		// We have other coverage asserting it sends the hash correctly.
		_ = gtest.ReceiveSoon(t, as.Actions)

		// Now when the mirror responds, we are at 75% votes without consensus.
		vrv = sfx.Fx.UpdateVRVPrevotes(ctx, vrv, map[string][]int{
			string(pb1.Block.Hash): {0, 1},
			"":                     {2},
		})

		gtest.SendSoon(t, sfx.RoundViewInCh, vrv)

		// Synchronize on the prevote delay starting, then make it elapse.
		_ = gtest.ReceiveSoon(t, prevoteDelayTimerStarted)
		sfx.RoundTimer.ElapsePrevoteDelayTimer(1, 0)

		// Upon elapse, the state machine makes a decide precommit request.
		req := gtest.ReceiveSoon(t, cStrat.DecidePrecommitRequests)
		require.Equal(t, vrv.VoteSummary, req.Input)

		// And the state machine would typically precommit nil at this point.
		gtest.SendSoon(t, req.ChoiceHash, "")

		// That precommit is sent to the mirror.
		act := gtest.ReceiveSoon(t, as.Actions)
		require.NotEmpty(t, act.Precommit.Sig)

		precommitSignContent, err := tmconsensus.PrecommitSignBytes(tmconsensus.VoteTarget{
			Height: 1, Round: 0,
		}, sfx.Fx.SignatureScheme)
		require.NoError(t, err)
		require.Equal(t, precommitSignContent, act.Precommit.SignContent)
		require.True(t, sfx.Cfg.Signer.PubKey().Verify(act.Precommit.SignContent, act.Precommit.Sig))

		// And at that point, it is present in the action store too.
		ra, err := sfx.Cfg.ActionStore.Load(ctx, 1, 0)
		require.NoError(t, err)
		require.Empty(t, ra.PrecommitTarget)
		require.Equal(t, string(act.Precommit.Sig), ra.PrecommitSignature)
	})

	t.Run("when majority prevotes reached while delay timer is active", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		sfx := tmstatetest.NewFixture(t, 4)

		sm := sfx.NewStateMachine(ctx)
		defer sm.Wait()
		defer cancel()

		as := gtest.ReceiveSoon(t, sfx.ToMirrorCh)

		// Set up consensus strategy expectation before mocking the response.
		cStrat := sfx.CStrat
		_ = cStrat.ExpectEnterRound(1, 0, nil)

		prevoteDelayTimerStarted := sfx.RoundTimer.PrevoteDelayStartNotification(1, 0)

		// Initial state has the proposed block.
		vrv := sfx.EmptyVRV(1, 0)
		pb1 := sfx.Fx.NextProposedBlock([]byte("app_data_1"), 3)
		vrv.ProposedBlocks = []tmconsensus.ProposedBlock{pb1}
		// Channel is 1-buffered, don't have to select.
		as.StateResponse <- tmeil.StateUpdate{VRV: vrv} // No PrevBlockHash at initial height.

		// This causes a Consider request to the consensus strategy,
		// and we will prevote for the block.
		considerReq := gtest.ReceiveSoon(t, cStrat.ConsiderProposedBlockRequests)
		gtest.SendSoon(t, considerReq.ChoiceHash, string(pb1.Block.Hash))

		// The choice is sent to the mirror as an action.
		// We have other coverage asserting it sends the hash correctly.
		_ = gtest.ReceiveSoon(t, as.Actions)

		// Now when the mirror responds, we are at 75% votes without consensus.
		vrv = sfx.Fx.UpdateVRVPrevotes(ctx, vrv, map[string][]int{
			string(pb1.Block.Hash): {0, 1},
			"":                     {2},
		})
		gtest.SendSoon(t, sfx.RoundViewInCh, vrv)

		// We don't have a synchronization point to detect the active prevote delay timer.
		// Poll for it to be active, then make it elapse.
		_ = gtest.ReceiveSoon(t, prevoteDelayTimerStarted)

		// Now while the timer is active, the final prevote arrives, pushing the proposed block to a majority prevote.
		vrv = sfx.Fx.UpdateVRVPrevotes(ctx, vrv, map[string][]int{
			string(pb1.Block.Hash): {0, 1, 3},
			"":                     {2},
		})
		gtest.SendSoon(t, sfx.RoundViewInCh, vrv)

		// The state machine makes a decide precommit request.
		req := gtest.ReceiveSoon(t, cStrat.DecidePrecommitRequests)
		require.Equal(t, vrv.VoteSummary, req.Input)

		// And the timer is no longer active since we are in awaiting precommits at this point.
		sfx.RoundTimer.RequireNoActiveTimer(t)

		// Since precommitting during a delay is an edge case,
		// do the full precommit assertion here.
		gtest.SendSoon(t, req.ChoiceHash, string(pb1.Block.Hash))

		// That precommit is sent to the mirror.
		act := gtest.ReceiveSoon(t, as.Actions)
		require.NotEmpty(t, act.Precommit.Sig)

		precommitSignContent, err := tmconsensus.PrecommitSignBytes(tmconsensus.VoteTarget{
			Height: 1, Round: 0,
			BlockHash: string(pb1.Block.Hash),
		}, sfx.Fx.SignatureScheme)
		require.NoError(t, err)
		require.Equal(t, precommitSignContent, act.Precommit.SignContent)
		require.True(t, sfx.Cfg.Signer.PubKey().Verify(act.Precommit.SignContent, act.Precommit.Sig))

		// And at that point, it is present in the action store too.
		ra, err := sfx.Cfg.ActionStore.Load(ctx, 1, 0)
		require.NoError(t, err)
		require.Equal(t, string(pb1.Block.Hash), ra.PrecommitTarget)
		require.Equal(t, string(act.Precommit.Sig), ra.PrecommitSignature)
	})

	t.Run("after full prevote received", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		sfx := tmstatetest.NewFixture(t, 4)

		sm := sfx.NewStateMachine(ctx)
		defer sm.Wait()
		defer cancel()

		as := gtest.ReceiveSoon(t, sfx.ToMirrorCh)

		// Set up consensus strategy expectation before mocking the response.
		cStrat := sfx.CStrat
		_ = cStrat.ExpectEnterRound(1, 0, nil)

		// Initial state has the proposed block.
		vrv := sfx.EmptyVRV(1, 0)
		pb1 := sfx.Fx.NextProposedBlock([]byte("app_data_1"), 3)
		vrv.ProposedBlocks = []tmconsensus.ProposedBlock{pb1}
		// Channel is 1-buffered, don't have to select.
		as.StateResponse <- tmeil.StateUpdate{VRV: vrv} // No PrevBlockHash at initial height.

		// This causes a Consider request to the consensus strategy,
		// and we will prevote for the block.
		considerReq := gtest.ReceiveSoon(t, cStrat.ConsiderProposedBlockRequests)
		gtest.SendSoon(t, considerReq.ChoiceHash, string(pb1.Block.Hash))

		// The choice is sent to the mirror as an action.
		// We have other coverage asserting it sends the hash correctly.
		_ = gtest.ReceiveSoon(t, as.Actions)

		vrv = sfx.Fx.UpdateVRVPrevotes(ctx, vrv, map[string][]int{
			string(pb1.Block.Hash): {0, 1, 2, 3},
		})

		gtest.SendSoon(t, sfx.RoundViewInCh, vrv)

		// With full prevotes present, the state machine makes a decide precommit request.
		req := gtest.ReceiveSoon(t, cStrat.DecidePrecommitRequests)
		require.Equal(t, vrv.VoteSummary, req.Input)
		gtest.SendSoon(t, req.ChoiceHash, string(pb1.Block.Hash))

		// That precommit is sent to the mirror.
		act := gtest.ReceiveSoon(t, as.Actions)
		require.NotEmpty(t, act.Precommit.Sig)

		precommitSignContent, err := tmconsensus.PrecommitSignBytes(tmconsensus.VoteTarget{
			Height: 1, Round: 0,
			BlockHash: string(pb1.Block.Hash),
		}, sfx.Fx.SignatureScheme)
		require.NoError(t, err)
		require.Equal(t, precommitSignContent, act.Precommit.SignContent)
		require.True(t, sfx.Cfg.Signer.PubKey().Verify(act.Precommit.SignContent, act.Precommit.Sig))

		// And at that point, it is present in the action store too.
		ra, err := sfx.Cfg.ActionStore.Load(ctx, 1, 0)
		require.NoError(t, err)
		require.Equal(t, string(pb1.Block.Hash), ra.PrecommitTarget)
		require.Equal(t, string(act.Precommit.Sig), ra.PrecommitSignature)
	})
}

func TestStateMachine_nilPrecommit(t *testing.T) {
	t.Run("normal flow from initial height", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		sfx := tmstatetest.NewFixture(t, 4)

		sm := sfx.NewStateMachine(ctx)
		defer sm.Wait()
		defer cancel()

		as := gtest.ReceiveSoon(t, sfx.ToMirrorCh)

		vrv := sfx.EmptyVRV(1, 0)
		vrv = sfx.Fx.UpdateVRVPrevotes(ctx, vrv, map[string][]int{
			"": {1, 2, 3}, // Everyone else already prevoted for the block.
		})

		// Set up consensus strategy expectation before mocking the response.
		cStrat := sfx.CStrat
		_ = cStrat.ExpectEnterRound(1, 0, nil)

		// Channel is 1-buffered, don't have to select.
		as.StateResponse <- tmeil.StateUpdate{VRV: vrv}

		// Since there was already a majority prevote for a block,
		// we don't need to submit our prevote -- we jump straight into the precommit decision.
		cReq := gtest.ReceiveSoon(t, cStrat.DecidePrecommitRequests)
		require.Equal(t, vrv.VoteSummary, cReq.Input)

		// And when the consensus strategy responds, the state machine forwards it to the mirror.
		gtest.SendSoon(t, cReq.ChoiceHash, "")

		act := gtest.ReceiveSoon(t, as.Actions)
		require.NotEmpty(t, act.Precommit.Sig)

		// Once the full nil precommits arrive, we will go to the next round.
		ercCh := cStrat.ExpectEnterRound(1, 1, nil)

		// Now we get a live update with everyone's precommit.
		vrv = sfx.Fx.UpdateVRVPrecommits(ctx, vrv, map[string][]int{
			"": {0, 1, 2, 3},
		})
		gtest.SendSoon(t, sfx.RoundViewInCh, vrv)

		// The state machine requests any existing state at 1/1 first.
		as = gtest.ReceiveSoon(t, sfx.ToMirrorCh)
		vrv = sfx.EmptyVRV(1, 1)
		as.StateResponse <- tmeil.StateUpdate{VRV: vrv}

		// Then it calls enter round on the gossip strategy.
		// We already set an expectation for this.
		erc := gtest.ReceiveSoon(t, ercCh)
		rv := erc.RV
		require.Equal(t, uint64(1), rv.Height)
		require.Equal(t, uint32(1), rv.Round)
	})

	t.Run("precommit delay timer canceled when advancing round", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		sfx := tmstatetest.NewFixture(t, 4)

		sm := sfx.NewStateMachine(ctx)
		defer sm.Wait()
		defer cancel()

		as := gtest.ReceiveSoon(t, sfx.ToMirrorCh)

		pb1 := sfx.Fx.NextProposedBlock([]byte("app_data_1"), 3)
		vrv := sfx.EmptyVRV(1, 0)
		vrv.ProposedBlocks = []tmconsensus.ProposedBlock{pb1}
		vrv = sfx.Fx.UpdateVRVPrevotes(ctx, vrv, map[string][]int{
			string(pb1.Block.Hash): {1, 2, 3}, // Everyone else already prevoted for the block.
		})

		// Set up consensus strategy expectation before mocking the response.
		cStrat := sfx.CStrat
		_ = cStrat.ExpectEnterRound(1, 0, nil)

		// Channel is 1-buffered, don't have to select.
		as.StateResponse <- tmeil.StateUpdate{VRV: vrv}

		// Since there was already a majority prevote for a block,
		// we don't need to submit our prevote -- we jump straight into the precommit decision.
		cReq := gtest.ReceiveSoon(t, cStrat.DecidePrecommitRequests)
		require.Equal(t, vrv.VoteSummary, cReq.Input)

		// And when the consensus strategy responds, the state machine forwards it to the mirror.
		gtest.SendSoon(t, cReq.ChoiceHash, string(pb1.Block.Hash))

		act := gtest.ReceiveSoon(t, as.Actions)
		require.NotEmpty(t, act.Precommit.Sig)

		// We expect a precommit delay due to how the precommits arrive.
		precommitDelayStarted := sfx.RoundTimer.PrecommitDelayStartNotification(1, 0)

		// Now we get a live update with 75% and no consensus yet.
		vrv = sfx.Fx.UpdateVRVPrecommits(ctx, vrv, map[string][]int{
			string(pb1.Block.Hash): {0, 3},
			"":                     {1},
		})
		gtest.SendSoon(t, sfx.RoundViewInCh, vrv)

		// This causes a precommit delay.
		_ = gtest.ReceiveSoon(t, precommitDelayStarted)

		ercCh := cStrat.ExpectEnterRound(1, 1, nil)
		proposalDelayStarted := sfx.RoundTimer.ProposalStartNotification(1, 1)

		// Without the precommit delay elapsing, we receive the last precommit.
		vrv = sfx.Fx.UpdateVRVPrecommits(ctx, vrv, map[string][]int{
			string(pb1.Block.Hash): {0, 3},
			"":                     {1, 2},
		})
		gtest.SendSoon(t, sfx.RoundViewInCh, vrv)

		// Once the full nil precommits arrive, we will go to the next round.
		as11 := gtest.ReceiveSoon(t, sfx.ToMirrorCh)
		require.Equal(t, uint64(1), as11.H)
		require.Equal(t, uint32(1), as11.R)
		as11.StateResponse <- tmeil.StateUpdate{VRV: sfx.EmptyVRV(1, 1)}

		_ = gtest.ReceiveSoon(t, ercCh)

		// And the proposal timer starts too.
		_ = gtest.ReceiveSoon(t, proposalDelayStarted)
	})
}

// These tests are focused on events that happen outside the happy path flow.
func TestStateMachine_unexpectedSteps(t *testing.T) {
	t.Run("view update during commit wait", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		sfx := tmstatetest.NewFixture(t, 4)

		sm := sfx.NewStateMachine(ctx)
		defer sm.Wait()
		defer cancel()

		as := gtest.ReceiveSoon(t, sfx.ToMirrorCh)

		pb1 := sfx.Fx.NextProposedBlock([]byte("app_data_1"), 3)
		vrv := sfx.EmptyVRV(1, 0)
		vrv.ProposedBlocks = []tmconsensus.ProposedBlock{pb1}
		vrv = sfx.Fx.UpdateVRVPrevotes(ctx, vrv, map[string][]int{
			string(pb1.Block.Hash): {1, 2, 3}, // Everyone else already prevoted for the block.
		})

		// Set up consensus strategy expectation before mocking the response.
		cStrat := sfx.CStrat
		_ = cStrat.ExpectEnterRound(1, 0, nil)

		// Channel is 1-buffered, don't have to select.
		as.StateResponse <- tmeil.StateUpdate{VRV: vrv}

		// Since there was already a majority prevote for a block,
		// we don't need to submit our prevote -- we jump straight into the precommit decision.
		cReq := gtest.ReceiveSoon(t, cStrat.DecidePrecommitRequests)
		require.Equal(t, vrv.VoteSummary, cReq.Input)

		// And when the consensus strategy responds, the state machine forwards it to the mirror.
		gtest.SendSoon(t, cReq.ChoiceHash, string(pb1.Block.Hash))

		act := gtest.ReceiveSoon(t, as.Actions)
		require.NotEmpty(t, act.Precommit.Sig)

		// We expect commit wait timer after we get the 3/4 precommits.
		commitWaitStarted := sfx.RoundTimer.CommitWaitStartNotification(1, 0)

		// Now we get a live update with majority consensus for the block.
		vrv = sfx.Fx.UpdateVRVPrecommits(ctx, vrv, map[string][]int{
			string(pb1.Block.Hash): {0, 1, 2},
		})
		gtest.SendSoon(t, sfx.RoundViewInCh, vrv)

		// Upon receiving that update, we are in commit wait.
		gtest.ReceiveSoon(t, commitWaitStarted)

		// And there is a finalization.
		finReq := gtest.ReceiveSoon(t, sfx.FinalizeBlockRequests)
		require.Equal(t, pb1.Block, finReq.Block)
		require.Zero(t, pb1.Round)

		// Now we get the view update with the last precommit.
		vrv = sfx.Fx.UpdateVRVPrecommits(ctx, vrv, map[string][]int{
			string(pb1.Block.Hash): {0, 1, 2, 3},
		})
		gtest.SendSoon(t, sfx.RoundViewInCh, vrv)

		// Just handling the view update successfully at least means
		// there is general handling for view updates while in commit wait.
		//
		// In the future we should have a way, on the finalization request,
		// to indicate that there are updated precommits available.
	})

	t.Run("view update during awaiting finalization", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		sfx := tmstatetest.NewFixture(t, 4)

		sm := sfx.NewStateMachine(ctx)
		defer sm.Wait()
		defer cancel()

		as := gtest.ReceiveSoon(t, sfx.ToMirrorCh)

		pb1 := sfx.Fx.NextProposedBlock([]byte("app_data_1"), 3)
		vrv := sfx.EmptyVRV(1, 0)
		vrv.ProposedBlocks = []tmconsensus.ProposedBlock{pb1}
		vrv = sfx.Fx.UpdateVRVPrevotes(ctx, vrv, map[string][]int{
			string(pb1.Block.Hash): {1, 2, 3}, // Everyone else already prevoted for the block.
		})

		// Set up consensus strategy expectation before mocking the response.
		cStrat := sfx.CStrat
		_ = cStrat.ExpectEnterRound(1, 0, nil)

		// Channel is 1-buffered, don't have to select.
		as.StateResponse <- tmeil.StateUpdate{VRV: vrv}

		// Since there was already a majority prevote for a block,
		// we don't need to submit our prevote -- we jump straight into the precommit decision.
		cReq := gtest.ReceiveSoon(t, cStrat.DecidePrecommitRequests)
		require.Equal(t, vrv.VoteSummary, cReq.Input)

		// And when the consensus strategy responds, the state machine forwards it to the mirror.
		gtest.SendSoon(t, cReq.ChoiceHash, string(pb1.Block.Hash))

		act := gtest.ReceiveSoon(t, as.Actions)
		require.NotEmpty(t, act.Precommit.Sig)

		// We expect commit wait timer after we get the 3/4 precommits.
		commitWaitStarted := sfx.RoundTimer.CommitWaitStartNotification(1, 0)

		// Now we get a live update with majority consensus for the block.
		vrv = sfx.Fx.UpdateVRVPrecommits(ctx, vrv, map[string][]int{
			string(pb1.Block.Hash): {0, 1, 2},
		})
		gtest.SendSoon(t, sfx.RoundViewInCh, vrv)

		// Upon receiving that update, we are in commit wait.
		gtest.ReceiveSoon(t, commitWaitStarted)

		// The commit wait timer elapses before the finalization request is handled.
		require.NoError(t, sfx.RoundTimer.ElapseCommitWaitTimer(1, 0))

		// Accept the finalization request.
		finReq := gtest.ReceiveSoon(t, sfx.FinalizeBlockRequests)
		require.Equal(t, pb1.Block, finReq.Block)
		require.Zero(t, pb1.Round)

		// Now we get the view update with the last precommit.
		vrv = sfx.Fx.UpdateVRVPrecommits(ctx, vrv, map[string][]int{
			string(pb1.Block.Hash): {0, 1, 2, 3},
		})
		gtest.SendSoon(t, sfx.RoundViewInCh, vrv)

		// Just handling the view update successfully at least means
		// there is general handling for view updates while in commit wait.
		//
		// In the future we should have a way, on the finalization request,
		// to indicate that there are updated precommits available.
	})
}

func TestStateMachine_finalization(t *testing.T) {
	t.Run("majority precommits at initialization", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		sfx := tmstatetest.NewFixture(t, 4)

		sm := sfx.NewStateMachine(ctx)
		defer sm.Wait()
		defer cancel()

		as := gtest.ReceiveSoon(t, sfx.ToMirrorCh)

		vrv := sfx.EmptyVRV(1, 0)
		pb1 := sfx.Fx.NextProposedBlock([]byte("app_data_1"), 1)
		vrv.ProposedBlocks = []tmconsensus.ProposedBlock{pb1}
		vrv = sfx.Fx.UpdateVRVPrecommits(ctx, vrv, map[string][]int{
			string(pb1.Block.Hash): {1, 2, 3}, // Everyone else already precommited for the block.
		})

		// Set up consensus strategy expectation before mocking the response.
		cStrat := sfx.CStrat
		_ = cStrat.ExpectEnterRound(1, 0, nil)

		// Channel is 1-buffered, don't have to select.
		as.StateResponse <- tmeil.StateUpdate{VRV: vrv}

		// Since there was already a majority precommit for a block,
		// we don't need to submit our precommit -- we jump straight into the finalization request.
		// NOTE: this may be a dubious assumption.
		// If everyone else precommitted, and we are live, there is an argument that the presence of precommits
		// implies that prevotes were also present.
		// But, if we somehow missed the prevotes, then we would need another method on the consensus strategy
		// to handle this special case.
		// So for now we will finalize without submitting our own precommit.
		finReq := gtest.ReceiveSoon(t, sfx.FinalizeBlockRequests)

		// Not asserting anything about the context.
		// That field is likely going away.

		require.Equal(t, pb1.Block, finReq.Block)
		require.Zero(t, finReq.Round)

		// Response channel must be 1-buffered to avoid the app blocking on send.
		require.Equal(t, 1, cap(finReq.Resp))

		// By the time that the finalize request has been made,
		// the commit wait timer has begun.
		sfx.RoundTimer.RequireActiveCommitWaitTimer(t, 1, 0)

		// Simulate the app responding.
		finReq.Resp <- tmapp.FinalizeBlockResponse{
			Height: 1, Round: 0,
			BlockHash: pb1.Block.Hash,

			Validators: sfx.Fx.Vals(),

			AppStateHash: []byte("app_state_1"),
		}

		// We don't have a synchronization point for the finalization being stored.
		// So, if we elapse the commit wait timer...
		require.NoError(t, sfx.RoundTimer.ElapseCommitWaitTimer(1, 0))

		// Then the state machine will have completed the round,
		// and it will submit a new action set to the mirror.
		as2 := gtest.ReceiveSoon(t, sfx.ToMirrorCh)
		require.Equal(t, uint64(2), as2.H)
		require.Zero(t, as2.R)
		require.True(t, sfx.Cfg.Signer.PubKey().Equal(as2.PubKey))

		// Actions channel is buffered so state machine doesn't block sending to mirror.
		require.Equal(t, 3, cap(as2.Actions))

		// State response is buffered so state machine doesn't risk blocking on send.
		require.Equal(t, 1, cap(as2.StateResponse))

		// And now that the state machine has sent the action set,
		// we can be sure the finalization store has the finalization for height 1.
		r, blockHash, vals, appHash, err := sfx.Cfg.FinalizationStore.LoadFinalizationByHeight(ctx, 1)
		require.NoError(t, err)
		require.Zero(t, r)
		require.Equal(t, string(pb1.Block.Hash), blockHash)
		require.True(t, tmconsensus.ValidatorSlicesEqual(vals, pb1.Block.Validators))
		require.Equal(t, "app_state_1", appHash) // String from the hand-coded response earlier in this test.
	})

	t.Run("when precommits arrive during a normal live update", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		sfx := tmstatetest.NewFixture(t, 4)

		sm := sfx.NewStateMachine(ctx)
		defer sm.Wait()
		defer cancel()

		as := gtest.ReceiveSoon(t, sfx.ToMirrorCh)

		vrv := sfx.EmptyVRV(1, 0)
		pb1 := sfx.Fx.NextProposedBlock([]byte("app_data_1"), 1)
		vrv.ProposedBlocks = []tmconsensus.ProposedBlock{pb1}
		vrv = sfx.Fx.UpdateVRVPrevotes(ctx, vrv, map[string][]int{
			string(pb1.Block.Hash): {1, 2, 3}, // Everyone else already prevoted for the block.
		})

		// Set up consensus strategy expectation before mocking the response.
		cStrat := sfx.CStrat
		_ = cStrat.ExpectEnterRound(1, 0, nil)

		// Channel is 1-buffered, don't have to select.
		as.StateResponse <- tmeil.StateUpdate{VRV: vrv}

		// Since there was already a majority prevote for a block,
		// we don't need to submit our prevote -- we jump straight into the precommit decision.
		cReq := gtest.ReceiveSoon(t, cStrat.DecidePrecommitRequests)
		require.Equal(t, vrv.VoteSummary, cReq.Input)

		// And when the consensus strategy responds, the state machine forwards it to the mirror.
		gtest.SendSoon(t, cReq.ChoiceHash, string(pb1.Block.Hash))

		act := gtest.ReceiveSoon(t, as.Actions)
		require.NotEmpty(t, act.Precommit.Sig)

		// Now we get a live update with everyone's precommit.
		vrv = sfx.Fx.UpdateVRVPrecommits(ctx, vrv, map[string][]int{
			string(pb1.Block.Hash): {0, 1, 2, 3},
		})
		gtest.SendSoon(t, sfx.RoundViewInCh, vrv)

		// This causes a finalization request.
		finReq := gtest.ReceiveSoon(t, sfx.FinalizeBlockRequests)

		// Not asserting anything about the context.
		// That field is likely going away.

		require.Equal(t, pb1.Block, finReq.Block)
		require.Zero(t, finReq.Round)

		// Response channel must be 1-buffered to avoid the app blocking on send.
		require.Equal(t, 1, cap(finReq.Resp))

		// By the time that the finalize request has been made,
		// the commit wait timer has begun.
		sfx.RoundTimer.RequireActiveCommitWaitTimer(t, 1, 0)

		// Simulate the app responding.
		finReq.Resp <- tmapp.FinalizeBlockResponse{
			Height: 1, Round: 0,
			BlockHash: pb1.Block.Hash,

			Validators: sfx.Fx.Vals(),

			AppStateHash: []byte("app_state_1"),
		}

		// We don't have a synchronization point for the finalization being stored.
		// So, if we elapse the commit wait timer...
		require.NoError(t, sfx.RoundTimer.ElapseCommitWaitTimer(1, 0))

		// Then the state machine will have completed the round,
		// and it will submit a new action set to the mirror.
		as2 := gtest.ReceiveSoon(t, sfx.ToMirrorCh)
		require.Equal(t, uint64(2), as2.H)
		require.Zero(t, as2.R)
		require.True(t, sfx.Cfg.Signer.PubKey().Equal(as2.PubKey))

		// Actions channel is buffered so state machine doesn't block sending to mirror.
		require.Equal(t, 3, cap(as2.Actions))

		// State response is buffered so state machine doesn't risk blocking on send.
		require.Equal(t, 1, cap(as2.StateResponse))

		// And now that the state machine has sent the action set,
		// we can be sure the finalization store has the finalization for height 1.
		r, blockHash, vals, appHash, err := sfx.Cfg.FinalizationStore.LoadFinalizationByHeight(ctx, 1)
		require.NoError(t, err)
		require.Zero(t, r)
		require.Equal(t, string(pb1.Block.Hash), blockHash)
		require.True(t, tmconsensus.ValidatorSlicesEqual(vals, pb1.Block.Validators))
		require.Equal(t, "app_state_1", appHash) // String from the hand-coded response earlier in this test.
	})

	t.Run("when precommits are finalized during a precommit delay", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		sfx := tmstatetest.NewFixture(t, 4)

		sm := sfx.NewStateMachine(ctx)
		defer sm.Wait()
		defer cancel()

		as := gtest.ReceiveSoon(t, sfx.ToMirrorCh)

		vrv := sfx.EmptyVRV(1, 0)
		pb1 := sfx.Fx.NextProposedBlock([]byte("app_data_1"), 1)
		vrv.ProposedBlocks = []tmconsensus.ProposedBlock{pb1}
		vrv = sfx.Fx.UpdateVRVPrevotes(ctx, vrv, map[string][]int{
			string(pb1.Block.Hash): {1, 2, 3}, // Everyone else already prevoted for the block.
		})

		// Set up consensus strategy expectation before mocking the response.
		cStrat := sfx.CStrat
		_ = cStrat.ExpectEnterRound(1, 0, nil)

		// Channel is 1-buffered, don't have to select.
		as.StateResponse <- tmeil.StateUpdate{VRV: vrv}

		// Since there was already a majority prevote for a block,
		// we don't need to submit our prevote -- we jump straight into the precommit decision.
		cReq := gtest.ReceiveSoon(t, cStrat.DecidePrecommitRequests)
		require.Equal(t, vrv.VoteSummary, cReq.Input)

		// And when the consensus strategy responds, the state machine forwards it to the mirror.
		gtest.SendSoon(t, cReq.ChoiceHash, string(pb1.Block.Hash))

		act := gtest.ReceiveSoon(t, as.Actions)
		require.NotEmpty(t, act.Precommit.Sig)

		precommitDelayTimerStarted := sfx.RoundTimer.PrecommitDelayStartNotification(1, 0)

		// Now we get a live update with 3/4 of precommits.
		// The last validator precommitted nil, so we enter precommit delay.
		vrv = sfx.Fx.UpdateVRVPrecommits(ctx, vrv, map[string][]int{
			string(pb1.Block.Hash): {0, 1},
			"":                     {3},
		})
		gtest.SendSoon(t, sfx.RoundViewInCh, vrv)

		_ = gtest.ReceiveSoon(t, precommitDelayTimerStarted)

		// Then there is another precommit update, causing the proposed block to cross the majority threshold.
		vrv = sfx.Fx.UpdateVRVPrecommits(ctx, vrv, map[string][]int{
			string(pb1.Block.Hash): {0, 1, 2},
			"":                     {3},
		})
		gtest.SendSoon(t, sfx.RoundViewInCh, vrv)

		// This causes a finalization request.
		finReq := gtest.ReceiveSoon(t, sfx.FinalizeBlockRequests)
		require.Equal(t, pb1.Block, finReq.Block)
		require.Zero(t, finReq.Round)

		// Response channel must be 1-buffered to avoid the app blocking on send.
		require.Equal(t, 1, cap(finReq.Resp))

		// By the time the finalization request is made, the precommit delay timer is no longer active,
		// but the commit wait timer is.
		sfx.RoundTimer.RequireActiveCommitWaitTimer(t, 1, 0)

		// Simulate the app responding.
		finReq.Resp <- tmapp.FinalizeBlockResponse{
			Height: 1, Round: 0,
			BlockHash: pb1.Block.Hash,

			Validators: sfx.Fx.Vals(),

			AppStateHash: []byte("app_state_1"),
		}

		// We don't have a synchronization point for the finalization being stored.
		// So, if we elapse the commit wait timer...
		require.NoError(t, sfx.RoundTimer.ElapseCommitWaitTimer(1, 0))

		// Then the state machine will have completed the round,
		// and it will submit a new action set to the mirror.
		as2 := gtest.ReceiveSoon(t, sfx.ToMirrorCh)
		require.Equal(t, uint64(2), as2.H)
		require.Zero(t, as2.R)
		require.True(t, sfx.Cfg.Signer.PubKey().Equal(as2.PubKey))

		// Actions channel is buffered so state machine doesn't block sending to mirror.
		require.Equal(t, 3, cap(as2.Actions))

		// State response is buffered so state machine doesn't risk blocking on send.
		require.Equal(t, 1, cap(as2.StateResponse))

		// And now that the state machine has sent the action set,
		// we can be sure the finalization store has the finalization for height 1.
		r, blockHash, vals, appHash, err := sfx.Cfg.FinalizationStore.LoadFinalizationByHeight(ctx, 1)
		require.NoError(t, err)
		require.Zero(t, r)
		require.Equal(t, string(pb1.Block.Hash), blockHash)
		require.True(t, tmconsensus.ValidatorSlicesEqual(vals, pb1.Block.Validators))
		require.Equal(t, "app_state_1", appHash) // String from the hand-coded response earlier in this test.
	})

	t.Run("when the commit wait timeout elapses before the finalization arrives", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		sfx := tmstatetest.NewFixture(t, 4)

		sm := sfx.NewStateMachine(ctx)
		defer sm.Wait()
		defer cancel()

		as := gtest.ReceiveSoon(t, sfx.ToMirrorCh)

		vrv := sfx.EmptyVRV(1, 0)
		pb1 := sfx.Fx.NextProposedBlock([]byte("app_data_1"), 1)
		vrv.ProposedBlocks = []tmconsensus.ProposedBlock{pb1}
		vrv = sfx.Fx.UpdateVRVPrevotes(ctx, vrv, map[string][]int{
			string(pb1.Block.Hash): {1, 2, 3}, // Everyone else already prevoted for the block.
		})

		// Set up consensus strategy expectation before mocking the response.
		cStrat := sfx.CStrat
		_ = cStrat.ExpectEnterRound(1, 0, nil)

		// Channel is 1-buffered, don't have to select.
		as.StateResponse <- tmeil.StateUpdate{VRV: vrv}

		// Since there was already a majority prevote for a block,
		// we don't need to submit our prevote -- we jump straight into the precommit decision.
		cReq := gtest.ReceiveSoon(t, cStrat.DecidePrecommitRequests)
		require.Equal(t, vrv.VoteSummary, cReq.Input)

		// And when the consensus strategy responds, the state machine forwards it to the mirror.
		gtest.SendSoon(t, cReq.ChoiceHash, string(pb1.Block.Hash))

		act := gtest.ReceiveSoon(t, as.Actions)
		require.NotEmpty(t, act.Precommit.Sig)

		// Now we get a live update with everyone's precommit.
		vrv = sfx.Fx.UpdateVRVPrecommits(ctx, vrv, map[string][]int{
			string(pb1.Block.Hash): {0, 1, 2, 3},
		})
		gtest.SendSoon(t, sfx.RoundViewInCh, vrv)

		// This causes a finalization request.
		finReq := gtest.ReceiveSoon(t, sfx.FinalizeBlockRequests)

		// And by the time we have the request, there is an active commit wait timer.
		sfx.RoundTimer.RequireActiveCommitWaitTimer(t, 1, 0)

		// Elapse it before we send the finalization response.
		require.NoError(t, sfx.RoundTimer.ElapseCommitWaitTimer(1, 0))
		finReq.Resp <- tmapp.FinalizeBlockResponse{
			Height: 1, Round: 0,
			BlockHash: pb1.Block.Hash,

			Validators: sfx.Fx.Vals(),

			AppStateHash: []byte("app_state_1"),
		}

		// The state machine tells the mirror we are on the next height.
		as = gtest.ReceiveSoon(t, sfx.ToMirrorCh)
		require.Equal(t, uint64(2), as.H)
		require.Zero(t, as.R)
	})
}

func TestStateMachine_followerMode(t *testing.T) {
	t.Run("happy path at initial height", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		sfx := tmstatetest.NewFixture(t, 4)
		sfx.Cfg.Signer = nil // Nil signer means "follower mode"; will never participate in consensus.

		sm := sfx.NewStateMachine(ctx)
		defer sm.Wait()
		defer cancel()

		as := gtest.ReceiveSoon(t, sfx.ToMirrorCh)

		// Still expect consensus strategy calls, for any local state.
		cStrat := sfx.CStrat
		_ = cStrat.ExpectEnterRound(1, 0, nil)

		vrv := sfx.EmptyVRV(1, 0)
		as.StateResponse <- tmeil.StateUpdate{VRV: vrv} // No PrevBlockHash at initial height.

		pb := sfx.Fx.NextProposedBlock([]byte("app_data_1"), 1)

		vrv = vrv.Clone()
		vrv.ProposedBlocks = []tmconsensus.ProposedBlock{pb}
		vrv.Version++

		gtest.SendSoon(t, sfx.RoundViewInCh, vrv)

		// This causes a Consider call.
		// Even in follower mode, the state machine is allowed to consider and choose proposed blocks.
		considerReq := gtest.ReceiveSoon(t, cStrat.ConsiderProposedBlockRequests)

		// The proposal timer is active before we make a decision.
		sfx.RoundTimer.RequireActiveProposalTimer(t, 1, 0)
		gtest.SendSoon(t, considerReq.ChoiceHash, string(pb.Block.Hash))

		// The majority of the network also prevotes for the block.
		vrv = sfx.Fx.UpdateVRVPrevotes(ctx, vrv, map[string][]int{string(pb.Block.Hash): {0, 1, 2}})
		gtest.SendSoon(t, sfx.RoundViewInCh, vrv)

		// Now that we have majority prevotes, the state machine makes a precommit decision request.
		precommitReq := gtest.ReceiveSoon(t, cStrat.DecidePrecommitRequests)

		// We precommit for the block.
		gtest.SendSoon(t, precommitReq.ChoiceHash, string(pb.Block.Hash))

		// Then the rest of the precommits arrive.
		vrv = sfx.Fx.UpdateVRVPrecommits(ctx, vrv, map[string][]int{string(pb.Block.Hash): {0, 1, 2, 3}})
		gtest.SendSoon(t, sfx.RoundViewInCh, vrv)

		// This causes a finalize block request.
		finReq := gtest.ReceiveSoon(t, sfx.FinalizeBlockRequests)

		require.Equal(t, pb.Block, finReq.Block)
		require.Zero(t, finReq.Round)

		// Response channel must be 1-buffered to avoid the app blocking on send.
		require.Equal(t, 1, cap(finReq.Resp))

		// By the time that the finalize request has been made,
		// the commit wait timer has begun.
		sfx.RoundTimer.RequireActiveCommitWaitTimer(t, 1, 0)

		// Simulate the app responding.
		finReq.Resp <- tmapp.FinalizeBlockResponse{
			Height: 1, Round: 0,
			BlockHash: pb.Block.Hash,

			Validators: sfx.Fx.Vals(),

			AppStateHash: []byte("app_state_1"),
		}

		// We don't have a synchronization point for the finalization being stored.
		// So, if we elapse the commit wait timer...
		require.NoError(t, sfx.RoundTimer.ElapseCommitWaitTimer(1, 0))

		// Then the state machine will have completed the round,
		// and it will submit a new action set to the mirror.
		as2 := gtest.ReceiveSoon(t, sfx.ToMirrorCh)
		require.Equal(t, uint64(2), as2.H)
		require.Zero(t, as2.R)
		require.Nil(t, as2.PubKey)

		// Actions channel is nil in follower mode.
		require.Nil(t, as2.Actions)

		// State response is buffered so state machine doesn't risk blocking on send.
		// Not nil even in follower mode.
		require.Equal(t, 1, cap(as2.StateResponse))
	})
}

func TestStateMachine_timers(t *testing.T) {
	t.Run("proposal", func(t *testing.T) {
		t.Run("choose from empty proposed block set when elapsed before receiving a proposed block", func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			sfx := tmstatetest.NewFixture(t, 2)

			sm := sfx.NewStateMachine(ctx)
			defer sm.Wait()
			defer cancel()

			as := gtest.ReceiveSoon(t, sfx.ToMirrorCh)

			// Set up consensus strategy expectation before mocking the response.
			cStrat := sfx.CStrat
			_ = cStrat.ExpectEnterRound(1, 0, nil)

			proposalTimerStarted := sfx.RoundTimer.ProposalStartNotification(1, 0)

			// Channel is 1-buffered, don't have to select.
			as.StateResponse <- tmeil.StateUpdate{VRV: sfx.EmptyVRV(1, 0)} // No PrevBlockHash at initial height.

			// Synchronize on proposal timer starting.
			_ = gtest.ReceiveSoon(t, proposalTimerStarted)

			// We haven't sent any proposed blocks, so if the timer elapses,
			// the state machine calls ChooseProposedBlock on the consensus strategy
			// with an empty set of proposed blocks.
			require.NoError(t, sfx.RoundTimer.ElapseProposalTimer(1, 0))
			choosePBReq := gtest.ReceiveSoon(t, cStrat.ChooseProposedBlockRequests)
			require.Empty(t, choosePBReq.Input)

			// And if the strategy makes a choice, it gets sent to the mirror.
			gtest.SendSoon(t, choosePBReq.ChoiceHash, "")

			action := gtest.ReceiveSoon(t, as.Actions)
			prevote := action.Prevote
			require.Empty(t, prevote.TargetHash)
			require.True(t, sfx.Cfg.Signer.PubKey().Verify(prevote.SignContent, prevote.Sig))
		})

		t.Run("choose from received proposed block set when elapsed", func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			sfx := tmstatetest.NewFixture(t, 4)

			sm := sfx.NewStateMachine(ctx)
			defer sm.Wait()
			defer cancel()

			as := gtest.ReceiveSoon(t, sfx.ToMirrorCh)

			// Set up consensus strategy expectation before mocking the response.
			cStrat := sfx.CStrat
			_ = cStrat.ExpectEnterRound(1, 0, nil)

			// Channel is 1-buffered, don't have to select.
			vrv := sfx.EmptyVRV(1, 0)
			as.StateResponse <- tmeil.StateUpdate{VRV: vrv} // No PrevBlockHash at initial height.

			pbs := []tmconsensus.ProposedBlock{
				sfx.Fx.NextProposedBlock([]byte("val1"), 1),
				sfx.Fx.NextProposedBlock([]byte("val2"), 2),
			}

			vrv = vrv.Clone()
			vrv.ProposedBlocks = slices.Clone(pbs)
			vrv.Version++

			gtest.SendSoon(t, sfx.RoundViewInCh, vrv)

			// This causes a Consider call, and we won't pick one at this point.
			considerReq := gtest.ReceiveSoon(t, cStrat.ConsiderProposedBlockRequests)
			gtest.SendSoon(t, considerReq.ChoiceError, tmconsensus.ErrProposedBlockChoiceNotReady)

			require.NoError(t, sfx.RoundTimer.ElapseProposalTimer(1, 0))
			choosePBReq := gtest.ReceiveSoon(t, cStrat.ChooseProposedBlockRequests)
			require.Equal(t, pbs, choosePBReq.Input)

			// Now choosing one of the PBs causes if the strategy makes a choice, it gets sent to the mirror.
			gtest.SendSoon(t, choosePBReq.ChoiceHash, string(pbs[0].Block.Hash))

			action := gtest.ReceiveSoon(t, as.Actions)
			prevote := action.Prevote
			require.Equal(t, string(pbs[0].Block.Hash), prevote.TargetHash)
			require.True(t, sfx.Cfg.Signer.PubKey().Verify(prevote.SignContent, prevote.Sig))
		})

		t.Run("choosing during ConsiderProposedBlocks cancels the timer", func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			sfx := tmstatetest.NewFixture(t, 2)

			sm := sfx.NewStateMachine(ctx)
			defer sm.Wait()
			defer cancel()

			as := gtest.ReceiveSoon(t, sfx.ToMirrorCh)

			// Set up consensus strategy expectation before mocking the response.
			cStrat := sfx.CStrat
			_ = cStrat.ExpectEnterRound(1, 0, nil)

			// Channel is 1-buffered, don't have to select.
			vrv := sfx.EmptyVRV(1, 0)
			as.StateResponse <- tmeil.StateUpdate{VRV: vrv} // No PrevBlockHash at initial height.

			pb := sfx.Fx.NextProposedBlock([]byte("app_data_1"), 1)

			vrv = vrv.Clone()
			vrv.ProposedBlocks = []tmconsensus.ProposedBlock{pb}
			vrv.Version++

			gtest.SendSoon(t, sfx.RoundViewInCh, vrv)

			// This causes a Consider call.
			considerReq := gtest.ReceiveSoon(t, cStrat.ConsiderProposedBlockRequests)

			// The proposal timer is active before we make a decision.
			sfx.RoundTimer.RequireActiveProposalTimer(t, 1, 0)
			gtest.SendSoon(t, considerReq.ChoiceHash, string(pb.Block.Hash))

			// Making a decision causes the prevote to be submitted.
			action := gtest.ReceiveSoon(t, as.Actions)
			prevote := action.Prevote
			require.Equal(t, string(pb.Block.Hash), prevote.TargetHash)
			require.True(t, sfx.Cfg.Signer.PubKey().Verify(prevote.SignContent, prevote.Sig))

			// And at that point the timer is no longer active.
			sfx.RoundTimer.RequireNoActiveTimer(t)
		})

		t.Run("crossing minority prevotes cancels the timer", func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			sfx := tmstatetest.NewFixture(t, 4)

			sm := sfx.NewStateMachine(ctx)
			defer sm.Wait()
			defer cancel()

			as := gtest.ReceiveSoon(t, sfx.ToMirrorCh)

			// Set up consensus strategy expectation before mocking the response.
			cStrat := sfx.CStrat
			_ = cStrat.ExpectEnterRound(1, 0, nil)

			// Channel is 1-buffered, don't have to select.
			vrv := sfx.EmptyVRV(1, 0)
			as.StateResponse <- tmeil.StateUpdate{VRV: vrv} // No PrevBlockHash at initial height.

			vrv = sfx.Fx.UpdateVRVPrevotes(ctx, vrv, map[string][]int{"": {1}}) // A quarter of the votes.

			gtest.SendSoon(t, sfx.RoundViewInCh, vrv)

			// After the first prevote, the proposal timer is still active,
			// and no PB requests have started.
			sfx.RoundTimer.RequireActiveProposalTimer(t, 1, 0)
			gtest.NotSending(t, cStrat.ConsiderProposedBlockRequests)
			gtest.NotSending(t, cStrat.ChooseProposedBlockRequests)

			// Now a second nil prevote arrives, which tells the state machine that
			// it is time to prevote.
			vrv = sfx.Fx.UpdateVRVPrevotes(ctx, vrv, map[string][]int{"": {1, 2}})

			gtest.SendSoon(t, sfx.RoundViewInCh, vrv)

			// Seeing the >1/3 prevotes causes a ChooseProposedBlock request
			// (with an empty set of proposed blocks since the VRV doesn't have any).
			choosePBReq := gtest.ReceiveSoon(t, cStrat.ChooseProposedBlockRequests)
			require.Empty(t, choosePBReq.Input)

			// The timer is cancelled even before the choose response.
			sfx.RoundTimer.RequireNoActiveTimer(t)
		})
	})

	t.Run("prevote", func(t *testing.T) {
		t.Run("starts when majority prevote without consensus", func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			sfx := tmstatetest.NewFixture(t, 8)

			sm := sfx.NewStateMachine(ctx)
			defer sm.Wait()
			defer cancel()

			as := gtest.ReceiveSoon(t, sfx.ToMirrorCh)

			// Set up consensus strategy expectation before mocking the response.
			cStrat := sfx.CStrat
			_ = cStrat.ExpectEnterRound(1, 0, nil)

			pb1 := sfx.Fx.NextProposedBlock([]byte("app_data_1"), 1)
			vrv := sfx.EmptyVRV(1, 0)
			vrv.ProposedBlocks = []tmconsensus.ProposedBlock{pb1}

			// One prevote for the block first.
			vrv = sfx.Fx.UpdateVRVPrevotes(ctx, vrv, map[string][]int{
				string(pb1.Block.Hash): {1},
			})

			prevoteDelayTimerStarted := sfx.RoundTimer.PrevoteDelayStartNotification(1, 0)

			// Channel is 1-buffered, don't have to select.
			as.StateResponse <- tmeil.StateUpdate{VRV: vrv} // No PrevBlockHash at initial height.

			// Our validator votes for the proposed block.
			considerPBReq := gtest.ReceiveSoon(t, cStrat.ConsiderProposedBlockRequests)
			require.Equal(t, []tmconsensus.ProposedBlock{pb1}, considerPBReq.Input)
			gtest.SendSoon(t, considerPBReq.ChoiceHash, string(pb1.Block.Hash))

			// Just drain the action; we have other coverage that this behaves correctly.
			_ = gtest.ReceiveSoon(t, as.Actions)

			// At this point, with only 12.5% or 25% of prevotes in, there should be no active timer.
			// But we don't have a good synchronization point to match this.

			// Then if we get more prevotes and they are split...
			vrv = sfx.Fx.UpdateVRVPrevotes(ctx, vrv, map[string][]int{
				string(pb1.Block.Hash): {0, 1, 2},
				"":                     {3, 4, 5},
			})
			gtest.SendSoon(t, sfx.RoundViewInCh, vrv)

			// Then we have an active prevote delay timer.
			_ = gtest.ReceiveSoon(t, prevoteDelayTimerStarted)

			// And when that timer elapses, there is a decide precommit request.
			require.NoError(t, sfx.RoundTimer.ElapsePrevoteDelayTimer(1, 0))
			req := gtest.ReceiveSoon(t, cStrat.DecidePrecommitRequests)
			require.Equal(t, vrv.VoteSummary, req.Input)
		})
	})

	t.Run("precommit", func(t *testing.T) {
		t.Run("starts when majority precommit without consensus", func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			sfx := tmstatetest.NewFixture(t, 8)

			sm := sfx.NewStateMachine(ctx)
			defer sm.Wait()
			defer cancel()

			as := gtest.ReceiveSoon(t, sfx.ToMirrorCh)

			// Set up consensus strategy expectation before mocking the response.
			cStrat := sfx.CStrat
			_ = cStrat.ExpectEnterRound(1, 0, nil)

			pb1 := sfx.Fx.NextProposedBlock([]byte("app_data_1"), 1)
			vrv := sfx.EmptyVRV(1, 0)
			vrv.ProposedBlocks = []tmconsensus.ProposedBlock{pb1}

			// Everyone else already prevoted for the block.
			vrv = sfx.Fx.UpdateVRVPrevotes(ctx, vrv, map[string][]int{
				string(pb1.Block.Hash): {1, 2, 3, 4, 5, 6, 7},
			})

			precommitDelayTimerStarted := sfx.RoundTimer.PrecommitDelayStartNotification(1, 0)

			// Channel is 1-buffered, don't have to select.
			as.StateResponse <- tmeil.StateUpdate{VRV: vrv} // No PrevBlockHash at initial height.

			// Since there are majority prevotes, we go straight to precommit.
			decidePrecommitReq := gtest.ReceiveSoon(t, cStrat.DecidePrecommitRequests)
			gtest.SendSoon(t, decidePrecommitReq.ChoiceHash, string(pb1.Block.Hash))

			// Now the mirror responds with some other precommits, enough to start the timer.
			vrv = sfx.Fx.UpdateVRVPrecommits(ctx, vrv, map[string][]int{
				string(pb1.Block.Hash): {0, 1, 2},
				"":                     {3, 4, 5},
			})
			gtest.SendSoon(t, sfx.RoundViewInCh, vrv)

			// Then we have an active prevote delay timer.
			_ = gtest.ReceiveSoon(t, precommitDelayTimerStarted)

			// When it elapses, we are going to enter the next round.
			ercCh := cStrat.ExpectEnterRound(1, 1, nil)

			require.NoError(t, sfx.RoundTimer.ElapsePrecommitDelayTimer(1, 0))

			// Elapsing advances to the next round now.
			as11 := gtest.ReceiveSoon(t, sfx.ToMirrorCh)
			as11.StateResponse <- tmeil.StateUpdate{VRV: sfx.EmptyVRV(1, 1)}

			erc := gtest.ReceiveSoon(t, ercCh)
			require.Equal(t, uint64(1), erc.RV.Height)
			require.Equal(t, uint32(1), erc.RV.Round)
		})
	})
}
