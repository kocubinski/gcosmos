package tmstate

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/trace"
	"slices"

	"github.com/rollchains/gordian/gcrypto"
	"github.com/rollchains/gordian/internal/gchan"
	"github.com/rollchains/gordian/internal/glog"
	"github.com/rollchains/gordian/tm/tmapp"
	"github.com/rollchains/gordian/tm/tmconsensus"
	"github.com/rollchains/gordian/tm/tmengine/internal/tmeil"
	"github.com/rollchains/gordian/tm/tmengine/internal/tmstate/internal/tsi"
	"github.com/rollchains/gordian/tm/tmstore"
)

type StateMachine struct {
	log *slog.Logger

	signer    gcrypto.Signer
	sigScheme tmconsensus.SignatureScheme

	hashScheme tmconsensus.HashScheme

	genesis tmconsensus.Genesis

	aStore tmstore.ActionStore
	fStore tmstore.FinalizationStore

	rt RoundTimer

	cm *tsi.ConsensusManager

	viewInCh               <-chan tmconsensus.VersionedRoundView
	toMirrorCh             chan<- tmeil.StateMachineRoundActionSet
	finalizeBlockRequestCh chan<- tmapp.FinalizeBlockRequest

	kernelDone chan struct{}
}

type StateMachineConfig struct {
	Signer          gcrypto.Signer
	SignatureScheme tmconsensus.SignatureScheme

	HashScheme tmconsensus.HashScheme

	Genesis tmconsensus.Genesis

	ActionStore       tmstore.ActionStore
	FinalizationStore tmstore.FinalizationStore

	RoundTimer RoundTimer

	ConsensusStrategy tmconsensus.ConsensusStrategy

	RoundViewInCh <-chan tmconsensus.VersionedRoundView
	ToMirrorCh    chan<- tmeil.StateMachineRoundActionSet

	FinalizeBlockRequestCh chan<- tmapp.FinalizeBlockRequest
}

func NewStateMachine(ctx context.Context, log *slog.Logger, cfg StateMachineConfig) (*StateMachine, error) {
	m := &StateMachine{
		log: log,

		signer:    cfg.Signer,
		sigScheme: cfg.SignatureScheme,

		hashScheme: cfg.HashScheme,

		genesis: cfg.Genesis,

		aStore: cfg.ActionStore,
		fStore: cfg.FinalizationStore,

		rt: cfg.RoundTimer,

		cm: tsi.NewConsensusManager(ctx, log.With("sm_sys", "consmgr"), cfg.ConsensusStrategy),

		viewInCh:               cfg.RoundViewInCh,
		toMirrorCh:             cfg.ToMirrorCh,
		finalizeBlockRequestCh: cfg.FinalizeBlockRequestCh,

		kernelDone: make(chan struct{}),
	}

	go m.kernel(ctx)

	if m.signer == nil {
		m.log.Info("State machine starting with nil signer; can never participate in consensus")
	}

	return m, nil
}

func (m *StateMachine) Wait() {
	m.cm.Wait()
	<-m.kernelDone
}

func (m *StateMachine) kernel(ctx context.Context) {
	defer close(m.kernelDone)

	ctx, task := trace.NewTask(ctx, "StateMachine.kernel")
	defer task.End()

	rlc, ok := m.initializeRLC(ctx)
	if !ok {
		// Failure during initialization.
		// Already logged, so just quit.
		return
	}

	for {
		if rlc.IsReplaying() {
			if !m.handleReplayEvent(ctx, &rlc) {
				return
			}
		} else {
			if !m.handleLiveEvent(ctx, &rlc) {
				return
			}
		}
	}
}

func (m *StateMachine) handleReplayEvent(ctx context.Context, rlc *tsi.RoundLifecycle) (ok bool) {
	return false
}

func (m *StateMachine) handleLiveEvent(ctx context.Context, rlc *tsi.RoundLifecycle) (ok bool) {
	defer trace.StartRegion(ctx, "handleLiveEvent").End()

	select {
	case <-ctx.Done():
		m.log.Info(
			"Quitting due to context cancellation in kernel main loop",
			"cause", context.Cause(ctx),
			"height", rlc.H, "round", rlc.R, "step", rlc.S,
			"vote_summary", rlc.PrevVRV.VoteSummary,
		)
		return false

	case vrv := <-m.viewInCh:
		m.handleViewUpdate(ctx, rlc, vrv)

	case p := <-rlc.ProposalCh:
		if !m.recordProposedBlock(ctx, *rlc, p) {
			return false
		}

		rlc.ProposalCh = nil

	case he := <-rlc.PrevoteHashCh:
		if he.Err != nil {
			glog.HRE(m.log, rlc.H, rlc.R, he.Err).Error(
				"Consensus strategy returned error when choosing proposed block to prevote",
			)
			return false
		}

		if !m.recordPrevote(ctx, rlc, he.Hash) {
			return false
		}

		rlc.PrevoteHashCh = nil

	case he := <-rlc.PrecommitHashCh:
		if he.Err != nil {
			glog.HRE(m.log, rlc.H, rlc.R, he.Err).Error(
				"Consensus strategy returned error when deciding precommit",
			)
			return false
		}

		if !m.recordPrecommit(ctx, rlc, he.Hash) {
			return false
		}

		rlc.PrecommitHashCh = nil

	case resp := <-rlc.FinalizeRespCh:
		if !m.handleFinalization(ctx, rlc, resp) {
			return false
		}

		rlc.FinalizeRespCh = nil

	case <-rlc.StepTimer:
		if !m.handleTimerElapsed(ctx, rlc) {
			return false
		}
	}

	return true
}

func (m *StateMachine) initializeRLC(ctx context.Context) (rlc tsi.RoundLifecycle, ok bool) {
	rlc, su, ok := m.sendInitialActionSet(ctx)
	if !ok {
		// Failure. Assume it was logged.
		return rlc, false
	}

	if !su.IsVRV() {
		return rlc, false
	}

	ok = m.beginRoundLive(ctx, &rlc, su.VRV)
	return rlc, ok
}

// beginRoundLive updates some fields on rlc,
// makes appropriate calls into the consensus strategy based on data in update,
// and starts any necessary timers.
// The initVRV value seeds the RoundLifecycle.
func (m *StateMachine) beginRoundLive(
	ctx context.Context, rlc *tsi.RoundLifecycle, initVRV tmconsensus.VersionedRoundView,
) (ok bool) {
	// Only calculate the step if we are dealing with a round view,
	// not if we have a committed block.
	curStep := tsi.GetStepFromVoteSummary(initVRV.VoteSummary)
	switch curStep {
	case tsi.StepAwaitingProposal:
		if len(initVRV.ProposedBlocks) > 0 {
			if !gchan.SendC(
				ctx, m.log,
				m.cm.ConsiderProposedBlocksRequests, tsi.ChooseProposedBlockRequest{
					PBs:    initVRV.ProposedBlocks,
					Result: rlc.PrevoteHashCh,
				},
				"making consider proposed blocks request from initial state",
			) {
				// Context cancelled and logged. Quit.
				return false
			}
		}

	case tsi.StepAwaitingPrevotes:
		if !gchan.SendC(
			ctx, m.log,
			m.cm.ConsiderProposedBlocksRequests, tsi.ChooseProposedBlockRequest{
				PBs:    initVRV.ProposedBlocks,
				Result: rlc.PrevoteHashCh,
			},
			"making consider proposed blocks request from initial state",
		) {
			// Context cancelled and logged. Quit.
			return false
		}

	case tsi.StepAwaitingPrecommits:
		if !gchan.SendC(
			ctx, m.log,
			m.cm.DecidePrecommitRequests, tsi.DecidePrecommitRequest{
				VS:     initVRV.VoteSummary.Clone(),
				Result: rlc.PrecommitHashCh,
			},
			"making decide precommit request from initial state",
		) {
			// Context cancelled and logged. Quit.
			return false
		}

	case tsi.StepCommitWait:
		committingHash := initVRV.VoteSummary.MostVotedPrecommitHash
		if committingHash == "" {
			// Ready to commit nil is a special case.
			// If we got here through normal flow we wouldn't be in commit wait.
			// But if the mirror gave us this information, we need to just go to the next round.
			return m.advanceRound(ctx, rlc)
		}

		// Otherwise we have a non-nil block to finalize.
		idx := slices.IndexFunc(initVRV.ProposedBlocks, func(pb tmconsensus.ProposedBlock) bool {
			return string(pb.Block.Hash) == committingHash
		})
		if idx < 0 {
			panic(fmt.Errorf(
				"TODO: beginRoundLive: handle committing a block that we don't have yet (height=%d hash=%x)",
				rlc.H, committingHash,
			))
		}
		if !gchan.SendC(
			ctx, m.log,
			m.finalizeBlockRequestCh, tmapp.FinalizeBlockRequest{
				// Not including Ctx. That field needs to go away for 0.3.
				// We would never cancel a finalize request due to local state in the state machine.

				Block: initVRV.ProposedBlocks[idx].Block,
				Round: initVRV.Round,

				Resp: rlc.FinalizeRespCh,
			},
			"making finalize block request from initial state",
		) {
			return false
		}

	default:
		panic(fmt.Errorf("BUG: unhandled initial step %s", curStep))
	}

	rlc.S = curStep
	rlc.PrevVRV = &initVRV

	// Only attempt to start the timer if we have a live view.
	m.startInitialTimer(ctx, rlc)

	return true
}

func (m *StateMachine) startInitialTimer(ctx context.Context, rlc *tsi.RoundLifecycle) {
	switch rlc.S {
	case tsi.StepAwaitingProposal:
		rlc.StepTimer, rlc.CancelTimer = m.rt.ProposalTimer(ctx, rlc.H, rlc.R)
	case tsi.StepAwaitingPrevotes, tsi.StepAwaitingPrecommits:
		// No timer needed in these starting steps.
	case tsi.StepCommitWait:
		rlc.StepTimer, rlc.CancelTimer = m.rt.CommitWaitTimer(ctx, rlc.H, rlc.R)
	default:
		panic(fmt.Errorf("BUG: no initial timer configured for step %s", rlc.S))
	}
}

// sendInitialActionSet loads any initial state from the stores
// and sends the current round action set to the mirror,
// so that the mirror can either begin sending round views or replayed blocks.
//
// If the context is cancelled when executing this method,
// it logs an appropriate error and reports false.
func (m *StateMachine) sendInitialActionSet(ctx context.Context) (
	rlc tsi.RoundLifecycle,
	su tmeil.StateUpdate,
	ok bool,
) {
	// Assume for now that we start up with no history.
	// We have to send our height and round to the mirror.
	initActionSet := tmeil.StateMachineRoundActionSet{
		H: m.genesis.InitialHeight, R: 0,

		StateResponse: make(chan tmeil.StateUpdate, 1),
	}
	if m.signer != nil {
		initActionSet.PubKey = m.signer.PubKey()
		initActionSet.Actions = make(chan tmeil.StateMachineRoundAction, 3)
	}
	update, ok := gchan.ReqResp(
		ctx, m.log,
		m.toMirrorCh, initActionSet,
		initActionSet.StateResponse,
		"seeding initial state from mirror",
	)
	if !ok {
		return rlc, update, false
	}
	// Closing the outgoing StateResponse channel is not strictly necessary,
	// but it is a little helpful in tests in case a StateMachineRoundActionSet is mistakenly reused.
	close(initActionSet.StateResponse)

	// We have a response -- do we need to call into the consensus strategy,
	// or do we only need to replay the block?
	if update.IsVRV() {
		rlc.Reset(ctx, initActionSet.H, initActionSet.R)
		rlc.OutgoingActionsCh = initActionSet.Actions // Should this be part of the Reset method instead?

		// Still assuming we are initializing at genesis,
		// which will not always be a correct assumption.
		rlc.PrevFinNextVals = slices.Clone(m.genesis.Validators)
		rlc.PrevFinAppStateHash = string(m.genesis.CurrentAppStateHash)

		rlc.CurVals = slices.Clone(m.genesis.Validators)

		// For now, set the previous block hash as the genesis pseudo-block's hash.
		// But maybe it would be better if the mirror generated this
		// and sent it as part of the state update.
		b, err := m.genesis.Block(m.hashScheme)
		if err != nil {
			panic(fmt.Errorf(
				"FATAL: failed to generate genesis block hash: %w", err,
			))
		}
		rlc.PrevBlockHash = string(b.Hash)

		// We have to synchronously enter the round,
		// but we still enter through the consensus manager for this.
		req := tsi.EnterRoundRequest{
			RV:     update.VRV.RoundView,
			Result: make(chan error), // Unbuffered since both sides sync on this.

			ProposalOut: rlc.ProposalCh,
		}

		err, ok := gchan.ReqResp(
			ctx, m.log,
			m.cm.EnterRoundRequests, req,
			req.Result,
			"entering round on consensus strategy",
		)
		if !ok {
			// Context cancelled, we cannot continue.
			return rlc, update, false
		}
		if err != nil {
			panic(fmt.Errorf(
				"FATAL: error when calling ConsensusStrategy.EnterRound: %v", err,
			))
		}
	} else {
		// TODO: there should be a different method for resetting
		// in expectation of handling a replayed block.
		rlc.Reset(ctx, initActionSet.H, initActionSet.R)

		// This is a replay, so we can just tell the app to finalize it.
		finReq := tmapp.FinalizeBlockRequest{
			Ctx:   ctx, // Is this the right context? Do we even still need context?
			Block: update.CB.Block,
			Round: update.CB.Proof.Round,

			Resp: rlc.FinalizeRespCh,
		}

		ok = gchan.SendC(
			ctx, m.log,
			m.finalizeBlockRequestCh, finReq,
			"sending finalize block response for replayed block",
		)
	}

	return rlc, update, ok
}

func (m *StateMachine) handleViewUpdate(
	ctx context.Context,
	rlc *tsi.RoundLifecycle,
	vrv tmconsensus.VersionedRoundView,
) {
	defer trace.StartRegion(ctx, "handleViewUpdate").End()

	if vrv.Height != rlc.H || vrv.Round != rlc.R {
		m.log.Debug(
			"Received out of bounds voting view",
			"cur_h", rlc.H, "cur_r", rlc.R,
			"recv_h", vrv.Height, "recv_r", vrv.Round,
		)
		return
	}

	if vrv.Version <= rlc.PrevVRV.Version {
		m.log.Warn(
			"Received non-increasing round view version; this is a likely bug",
			"h", rlc.H, "r", rlc.R, "step", rlc.S,
			"prev_version", rlc.PrevVRV.Version, "cur_version", vrv.Version,
		)
		return
	}

	switch rlc.S {
	case tsi.StepAwaitingProposal:
		m.handleProposalViewUpdate(ctx, rlc, vrv)
	case tsi.StepAwaitingPrevotes, tsi.StepPrevoteDelay:
		m.handlePrevoteViewUpdate(ctx, rlc, vrv)
	case tsi.StepAwaitingPrecommits, tsi.StepPrecommitDelay:
		m.handlePrecommitViewUpdate(ctx, rlc, vrv)
	case tsi.StepCommitWait, tsi.StepAwaitingFinalization:
		// Nothing to do here, for now.
		// By the time we are in commit wait, we've already made a finalization request.
		// In the future we might find a way to notify an in-progress finalization request
		// that there are updated precommits.
	default:
		panic(fmt.Errorf("TODO: handle view update for step %q", rlc.S))
	}

	if vrv.Height == rlc.PrevVRV.Height && vrv.Round == rlc.PrevVRV.Round {
		// If the view update caused a nil commit,
		// the incoming vrv's height and round will differ from the set PrevVRV.
		// So we have to check this in order to avoid "moving backwards".

		// Assuming it's okay to take ownership of vrv as opposed to copying in to our value.
		rlc.PrevVRV = &vrv
	}
}

// handleProposalViewUpdate is called when we receive an update from the mirror
// and the current step is awaiting proposal.
func (m *StateMachine) handleProposalViewUpdate(
	ctx context.Context,
	rlc *tsi.RoundLifecycle,
	vrv tmconsensus.VersionedRoundView,
) {
	defer trace.StartRegion(ctx, "handleProposalViewUpdate").End()

	min := tmconsensus.ByzantineMinority(vrv.VoteSummary.AvailablePower)
	maj := tmconsensus.ByzantineMajority(vrv.VoteSummary.AvailablePower)

	if vrv.VoteSummary.TotalPrecommitPower >= maj {
		// The majority of the network is on precommit and we are just expecting a proposal.

		// We can start by canceling the proposal timer.
		rlc.CancelTimer()
		rlc.StepTimer = nil
		rlc.CancelTimer = nil

		// There is no point in submitting a prevote at this point,
		// because the majority of the network has already seen enough prevotes
		// to make their precommit decision.

		// If the majority has already reached consensus then
		// we will attempt to submit our own precommit
		// based on the prevotes we have right now.
		vs := vrv.VoteSummary
		maxBlockPow := vs.PrecommitBlockPower[vs.MostVotedPrecommitHash]
		if maxBlockPow >= maj {
			// There is consensus on a hash,
			// but we have to treat nil differently from a particular block.
			if vs.MostVotedPrecommitHash == "" {
				// For now, we just advance the round without submitting our own precommit.
				// We do have sufficient information to submit a precommit,
				// but we ought to adjust the way the consensus strategy is structured
				// in order to indicate that the round is terminating
				// and that the consensus strategy is allowed to elect not to precommit.
				_ = m.advanceRound(ctx, rlc)
				return
			}

			// Otherwise it must be a particular block.
			// Just like the nil precommit case,
			// we are currently not consulting the consensus strategy.
			_ = m.advanceHeight(ctx, rlc)
			return
		}

		// There was majority precommit power present but it was not for a particular block.
		// Start the precommit delay.
		rlc.S = tsi.StepPrecommitDelay
		rlc.StepTimer, rlc.CancelTimer = m.rt.PrecommitDelayTimer(ctx, rlc.H, rlc.R)

		// And we need to submit our own precommit decision still.
		_ = gchan.SendC(
			ctx, m.log,
			m.cm.DecidePrecommitRequests, tsi.DecidePrecommitRequest{
				VS:     vrv.VoteSummary.Clone(), // Clone under assumption to avoid data race.
				Result: rlc.PrecommitHashCh,     // Is it ever possible this channel is nil?
			},
			"deciding precommit following observation of majority precommit while expecting proposal",
		)

		return
	}

	if vrv.VoteSummary.TotalPrecommitPower >= min {
		// A sufficient portion of the rest of the network
		// has decided they have enough information to precommit.
		// So we will begin our precommit too.

		// We can start by canceling the proposal timer.
		rlc.CancelTimer()
		rlc.StepTimer = nil
		rlc.CancelTimer = nil

		rlc.S = tsi.StepAwaitingPrecommits

		// And we need to submit our own precommit decision still.
		_ = gchan.SendC(
			ctx, m.log,
			m.cm.DecidePrecommitRequests, tsi.DecidePrecommitRequest{
				VS:     vrv.VoteSummary.Clone(), // Clone under assumption to avoid data race.
				Result: rlc.PrecommitHashCh,     // Is it ever possible this channel is nil?
			},
			"deciding precommit following observation of minority precommit while expecting proposal",
		)

		return
	}

	if vrv.VoteSummary.TotalPrevotePower >= maj {
		// The majority has made their prevote.

		// We are switching to either awaiting precommits or prevote delay.
		// Either way, when we entered this method, we had a proposal timer,
		// so an unconditional cancel should be safe.
		rlc.CancelTimer()
		rlc.StepTimer = nil
		rlc.CancelTimer = nil

		// And we are making a request to choose or consider in either case too.
		req := tsi.ChooseProposedBlockRequest{
			PBs: vrv.ProposedBlocks,

			Result: rlc.PrevoteHashCh,
		}

		vs := vrv.VoteSummary
		maxBlockPow := vs.PrevoteBlockPower[vs.MostVotedPrevoteHash]
		if maxBlockPow >= maj {
			// If the majority power is at consensus, we submit our prevote immediately.
			rlc.S = tsi.StepAwaitingPrecommits

			select {
			case m.cm.ChooseProposedBlockRequests <- req:
				// Okay.
			default:
				panic("TODO: handle blocked send to ChooseProposedBlocksRequests")
			}
			return
		}

		// Otherwise, the majority power is present but there is not yet consensus.
		// Consider the proposed blocks and start the prevote delay.

		rlc.S = tsi.StepPrevoteDelay
		rlc.StepTimer, rlc.CancelTimer = m.rt.PrevoteDelayTimer(ctx, rlc.H, rlc.R)

		select {
		case m.cm.ConsiderProposedBlocksRequests <- req:
			// Okay.
		default:
			panic("TODO: handle blocked send to ConsiderProposedBlocksRequests")
		}
		return
	}

	// If we have only exceeded minority prevote power, we will continue waiting for proposed blocks.

	if len(vrv.ProposedBlocks) > len(rlc.PrevVRV.ProposedBlocks) {
		// At least one new proposed block.
		// The timer hasn't elapsed yet so it is only a Consider call at this point.
		req := tsi.ChooseProposedBlockRequest{
			PBs: vrv.ProposedBlocks,

			Result: rlc.PrevoteHashCh,
		}
		select {
		case m.cm.ConsiderProposedBlocksRequests <- req:
			// Okay.
		default:
			panic("TODO: handle blocked send to ConsiderProposedBlocksRequests")
		}
	}
}

// handlePrevoteViewUpdate is called when we receive an update from the mirror
// and the current step is awaiting prevotes or prevote delay.
func (m *StateMachine) handlePrevoteViewUpdate(
	ctx context.Context,
	rlc *tsi.RoundLifecycle,
	vrv tmconsensus.VersionedRoundView,
) {
	defer trace.StartRegion(ctx, "handlePrevoteViewUpdate").End()

	vs := vrv.VoteSummary
	maj := tmconsensus.ByzantineMajority(vs.AvailablePower)
	if vs.TotalPrevotePower >= maj {
		// We have majority vote power present;
		// do we have majority vote power on a single block?
		maxPow := vs.PrevoteBlockPower[vs.MostVotedPrevoteHash]
		if maxPow >= maj {
			if rlc.S == tsi.StepPrevoteDelay {
				// Clear out the old timers.
				rlc.CancelTimer()
				rlc.StepTimer = nil
				rlc.CancelTimer = nil
			}

			rlc.S = tsi.StepAwaitingPrecommits

			if !gchan.SendC(
				ctx, m.log,
				m.cm.DecidePrecommitRequests, tsi.DecidePrecommitRequest{
					VS:     vrv.VoteSummary.Clone(), // Clone under assumption to avoid data race.
					Result: rlc.PrecommitHashCh,     // Is it ever possible this channel is nil?
				},
				"choosing precommit following majority prevote",
			) {
				// Context cancelled and logged. Quit.
				return
			}
		} else {
			// We have majority prevotes but not on a single block.
			// Only start the timer if we were not already in prevote delay.
			if rlc.S == tsi.StepAwaitingPrevotes {
				rlc.S = tsi.StepPrevoteDelay
				rlc.StepTimer, rlc.CancelTimer = m.rt.PrevoteDelayTimer(ctx, rlc.H, rlc.R)
			}
		}
	}
}

func (m *StateMachine) recordPrevote(
	ctx context.Context,
	rlc *tsi.RoundLifecycle,
	targetHash string,
) (ok bool) {
	if m.signer != nil {
		// Record to the action store first.
		h, r := rlc.H, rlc.R
		vt := tmconsensus.VoteTarget{
			Height: h, Round: r,
			BlockHash: targetHash,
		}
		signContent, err := tmconsensus.PrevoteSignBytes(vt, m.sigScheme)
		if err != nil {
			glog.HRE(m.log, h, r, err).Error(
				"Failed to generate prevote sign bytes",
				"target_hash", glog.Hex(targetHash),
			)
			return false
		}

		sig, err := m.signer.Sign(ctx, signContent)
		if err != nil {
			glog.HRE(m.log, h, r, err).Error(
				"Failed to sign prevote content",
				"target_hash", glog.Hex(targetHash),
			)
			return false
		}

		if err := m.aStore.SavePrevote(ctx, m.signer.PubKey(), vt, sig); err != nil {
			glog.HRE(m.log, h, r, err).Error("Failed to save prevote to action store")
			return false
		}

		// The OutgoingActionsCh is 3-buffered so we assume this will never block.
		rlc.OutgoingActionsCh <- tmeil.StateMachineRoundAction{
			Prevote: tmeil.ScopedSignature{
				TargetHash:  targetHash,
				SignContent: signContent,
				Sig:         sig,
			},
		}
	}

	// Finally, if we were waiting for proposed blocks and we submitted our own prevote,
	// then we can advance to the next step.
	if rlc.S == tsi.StepAwaitingProposal {
		rlc.S = tsi.StepAwaitingPrevotes
		rlc.CancelTimer()
		rlc.CancelTimer = nil
		rlc.StepTimer = nil
	}

	return true
}

func (m *StateMachine) handlePrecommitViewUpdate(
	ctx context.Context,
	rlc *tsi.RoundLifecycle,
	vrv tmconsensus.VersionedRoundView,
) {
	defer trace.StartRegion(ctx, "handlePrecommitViewUpdate").End()

	vs := vrv.VoteSummary
	maj := tmconsensus.ByzantineMajority(vs.AvailablePower)
	if vs.TotalPrecommitPower >= maj {
		// We have majority vote power present;
		// do we have majority vote power on a single block?
		maxPow := vs.PrecommitBlockPower[vs.MostVotedPrecommitHash]
		if maxPow >= maj {
			if vs.MostVotedPrecommitHash == "" {
				_ = m.advanceRound(ctx, rlc)
				return
			}

			if rlc.S == tsi.StepPrecommitDelay {
				rlc.CancelTimer()
				rlc.StepTimer = nil
				rlc.CancelTimer = nil
			}

			rlc.S = tsi.StepCommitWait
			rlc.StepTimer, rlc.CancelTimer = m.rt.CommitWaitTimer(ctx, rlc.H, rlc.R)

			idx := slices.IndexFunc(vrv.ProposedBlocks, func(pb tmconsensus.ProposedBlock) bool {
				return string(pb.Block.Hash) == vs.MostVotedPrecommitHash
			})
			if idx < 0 {
				panic(fmt.Errorf(
					"TODO: handlePrecommitViewUpdate: handle committing a block that we don't have yet (height=%d hash=%x)",
					rlc.H, vs.MostVotedPrecommitHash,
				))
			}

			if !gchan.SendC(
				ctx, m.log,
				m.finalizeBlockRequestCh, tmapp.FinalizeBlockRequest{
					// Not including Ctx. That field needs to go away for 0.3.
					// We would never cancel a finalize request due to local state in the state machine.

					Block: vrv.ProposedBlocks[idx].Block,
					Round: vrv.Round,

					Resp: rlc.FinalizeRespCh,
				},
				"making finalize block request from precommit update",
			) {
				return
			}
		} else if vs.TotalPrecommitPower == vs.AvailablePower {
			// Reached 100% precommits but didn't reach consensus on a single block or nil.
			_ = m.advanceRound(ctx, rlc)
		} else {
			// We have majority precommits but not on a single block.
			// Only start the timer if we were not already in precommit delay.
			if rlc.S == tsi.StepAwaitingPrecommits {
				rlc.S = tsi.StepPrecommitDelay
				rlc.StepTimer, rlc.CancelTimer = m.rt.PrecommitDelayTimer(ctx, rlc.H, rlc.R)
			}
		}
	}
}

func (m *StateMachine) recordPrecommit(
	ctx context.Context,
	rlc *tsi.RoundLifecycle,
	targetHash string,
) (ok bool) {
	if m.signer == nil {
		return true
	}

	// Record to the action store first.
	h, r := rlc.H, rlc.R
	vt := tmconsensus.VoteTarget{
		Height: h, Round: r,
		BlockHash: targetHash,
	}
	signContent, err := tmconsensus.PrecommitSignBytes(vt, m.sigScheme)
	if err != nil {
		glog.HRE(m.log, h, r, err).Error(
			"Failed to generate precommit sign bytes",
			"target_hash", glog.Hex(targetHash),
		)
		return false
	}

	sig, err := m.signer.Sign(ctx, signContent)
	if err != nil {
		glog.HRE(m.log, h, r, err).Error(
			"Failed to sign precommit content",
			"target_hash", glog.Hex(targetHash),
		)
		return false
	}

	if err := m.aStore.SavePrecommit(ctx, m.signer.PubKey(), vt, sig); err != nil {
		glog.HRE(m.log, h, r, err).Error("Failed to save precommit to action store")
		return false
	}

	// The OutgoingActionsCh is 3-buffered so we assume this will never block.
	rlc.OutgoingActionsCh <- tmeil.StateMachineRoundAction{
		Precommit: tmeil.ScopedSignature{
			TargetHash:  targetHash,
			SignContent: signContent,
			Sig:         sig,
		},
	}

	return true
}

func (m *StateMachine) recordProposedBlock(
	ctx context.Context,
	rlc tsi.RoundLifecycle,
	p tmconsensus.Proposal,
) (ok bool) {
	h, r := rlc.H, rlc.R

	pb := tmconsensus.ProposedBlock{
		Block: tmconsensus.Block{
			PrevBlockHash: []byte(rlc.PrevBlockHash),

			Height: h,

			// TODO: previous commit proof. This should come from the mirror.

			Validators:     slices.Clone(rlc.CurVals),
			NextValidators: slices.Clone(rlc.PrevFinNextVals),

			DataID: []byte(p.AppDataID),

			PrevAppStateHash: []byte(rlc.PrevFinAppStateHash),

			Annotations: tmconsensus.Annotations{
				App: p.BlockAnnotation,

				// TODO: where will the engine annotations come from?
			},
		},

		Round: r,

		ProposerPubKey: m.signer.PubKey(),

		Annotations: tmconsensus.Annotations{
			App: p.ProposalAnnotation,

			// TODO: where will the engine annotations come from?
		},
	}

	hash, err := m.hashScheme.Block(pb.Block)
	if err != nil {
		glog.HRE(m.log, h, r, err).Error("Failed to calculate hash for proposed block")
		return false
	}
	pb.Block.Hash = hash

	signContent, err := tmconsensus.ProposalSignBytes(pb.Block, r, pb.Annotations, m.sigScheme)
	if err != nil {
		glog.HRE(m.log, h, r, err).Error("Failed to produce signing content for proposed block")
		return false
	}
	sig, err := m.signer.Sign(ctx, signContent)
	if err != nil {
		glog.HRE(m.log, h, r, err).Error("Failed to sign proposed block")
		return false
	}
	pb.Signature = sig

	if err := m.aStore.SaveProposedBlock(ctx, pb); err != nil {
		glog.HRE(m.log, h, r, err).Error("Failed to save proposed block to action store")
		return false
	}

	// The OutgoingActionsCh is 3-buffered so we assume this will never block.
	rlc.OutgoingActionsCh <- tmeil.StateMachineRoundAction{
		PB: pb,
	}

	return true
}

func (m *StateMachine) handleFinalization(
	ctx context.Context,
	rlc *tsi.RoundLifecycle,
	resp tmapp.FinalizeBlockResponse,
) (ok bool) {
	if len(resp.Validators) == 0 {
		panic(fmt.Errorf(
			"BUG: application did not set validators in finalization response (height=%d round=%d block_hash=%x)",
			resp.Height, resp.Round, resp.BlockHash,
		))
	}

	clear(rlc.FinalizedValidators)
	rlc.FinalizedValidators = append(rlc.FinalizedValidators, resp.Validators...)
	rlc.FinalizedAppStateHash = string(resp.AppStateHash)

	rlc.FinalizeRespCh = nil

	if resp.Height != rlc.H || resp.Round != rlc.R {
		panic(fmt.Errorf(
			"BUG: app sent height/round %d/%d differing from current (%d/%d)",
			resp.Height, resp.Round, rlc.H, rlc.R,
		))
	}

	if err := m.fStore.SaveFinalization(
		ctx,
		rlc.H, rlc.R,
		string(resp.BlockHash),
		rlc.CurVals,
		string(resp.AppStateHash),
	); err != nil {
		glog.HRE(m.log, rlc.H, rlc.R, err).Error(
			"Failed to save finalization to Finalization Store",
		)
		return false
	}

	// The step is AwaitingFinalization if the commit wait timer has already elapsed.
	if rlc.S == tsi.StepAwaitingFinalization {
		if !m.advanceHeight(ctx, rlc) {
			return false
		}
	}

	return true
}

func (m *StateMachine) handleTimerElapsed(ctx context.Context, rlc *tsi.RoundLifecycle) (ok bool) {
	defer trace.StartRegion(ctx, "handleTimerElapsed").End()

	switch rlc.S {
	case tsi.StepAwaitingProposal:
		if !gchan.SendC(
			ctx, m.log,
			m.cm.ChooseProposedBlockRequests, tsi.ChooseProposedBlockRequest{
				PBs:    slices.Clone(rlc.PrevVRV.ProposedBlocks), // Clone under assumption to avoid data race.
				Result: rlc.PrevoteHashCh,                        // Is it ever possible this channel is nil?
			},
			"choosing proposed block following proposal timeout",
		) {
			// Context cancelled and logged. Quit.
			return false
		}

		// Move on to awaiting prevotes.
		rlc.S = tsi.StepAwaitingPrevotes

		// Call cancel anyway as a matter of cleanup.
		rlc.CancelTimer()
		rlc.StepTimer = nil
		rlc.CancelTimer = nil

	case tsi.StepPrevoteDelay:
		if !gchan.SendC(
			ctx, m.log,
			m.cm.DecidePrecommitRequests, tsi.DecidePrecommitRequest{
				VS:     rlc.PrevVRV.VoteSummary.Clone(), // Clone under assumption to avoid data race.
				Result: rlc.PrecommitHashCh,             // Is it ever possible this channel is nil?
			},
			"choosing precommit following prevote delay timeout",
		) {
			// Context cancelled and logged. Quit.
			return false
		}

		// Move on to awaiting precommits.
		rlc.S = tsi.StepAwaitingPrecommits

		rlc.CancelTimer()
		rlc.StepTimer = nil
		rlc.CancelTimer = nil

	case tsi.StepPrecommitDelay:
		rlc.CancelTimer()
		rlc.StepTimer = nil
		rlc.CancelTimer = nil

		if !m.advanceRound(ctx, rlc) {
			return false
		}

	case tsi.StepCommitWait:
		rlc.CommitWaitElapsed = true

		rlc.CancelTimer()
		rlc.StepTimer = nil
		rlc.CancelTimer = nil

		if len(rlc.FinalizedValidators) == 0 {
			// The timer has elapsed but we don't have a finalization yet.
			rlc.S = tsi.StepAwaitingFinalization
			return true
		}

		// We already finalized and now the commit wait has elapsed,
		// so now we have to advance the height.
		if !m.advanceHeight(ctx, rlc) {
			return false
		}

	default:
		panic(fmt.Errorf("BUG: unhandled timer elapse for step %s", rlc.S))
	}

	return true
}

func (m *StateMachine) advanceHeight(ctx context.Context, rlc *tsi.RoundLifecycle) (ok bool) {
	rlc.CycleFinalization()
	rlc.Reset(ctx, rlc.H+1, 0)

	as := tmeil.StateMachineRoundActionSet{
		H: rlc.H,
		R: 0,

		StateResponse: make(chan tmeil.StateUpdate, 1),
	}

	if m.signer != nil {
		as.PubKey = m.signer.PubKey()
		as.Actions = make(chan tmeil.StateMachineRoundAction, 3)
	}

	update, ok := gchan.ReqResp(
		ctx, m.log,
		m.toMirrorCh, as,
		as.StateResponse,
		"seeding initial state from mirror",
	)
	if !ok {
		return false
	}

	// This is similar to the handling in sendInitialActionSet,
	// but it differs because at this point it is impossible for us to be on the initial height;
	// and the rlc has some state we are reusing through the earlier call to rlc.CycleFinalization.
	if update.IsVRV() {
		rlc.OutgoingActionsCh = as.Actions // Should this be part of the Reset method instead?

		// We have to synchronously enter the round,
		// but we still enter through the consensus manager for this.
		req := tsi.EnterRoundRequest{
			RV:     update.VRV.RoundView,
			Result: make(chan error), // Unbuffered since both sides sync on this.

			ProposalOut: rlc.ProposalCh,
		}

		err, ok := gchan.ReqResp(
			ctx, m.log,
			m.cm.EnterRoundRequests, req,
			req.Result,
			"entering round on consensus strategy while advancing height",
		)
		if !ok {
			// Context cancelled, we cannot continue.
			return false
		}
		if err != nil {
			panic(fmt.Errorf(
				"FATAL: error when calling ConsensusStrategy.EnterRound while advancing height: %v", err,
			))
		}

		if !m.beginRoundLive(ctx, rlc, update.VRV) {
			return false
		}
	} else {
		// This case could happen if we took more than an entire block to finalize,
		// due to an app bug or whatever reason.
		panic("TODO: handle replayed block upon finalization")
	}

	return true
}

func (m *StateMachine) advanceRound(ctx context.Context, rlc *tsi.RoundLifecycle) (ok bool) {
	// TODO: do we need to do anything with the finalizations?
	rlc.Reset(ctx, rlc.H, rlc.R+1)

	as := tmeil.StateMachineRoundActionSet{
		H: rlc.H,
		R: rlc.R,

		StateResponse: make(chan tmeil.StateUpdate, 1),
	}

	if m.signer != nil {
		as.PubKey = m.signer.PubKey()
		as.Actions = make(chan tmeil.StateMachineRoundAction, 3)
	}

	update, ok := gchan.ReqResp(
		ctx, m.log,
		m.toMirrorCh, as,
		as.StateResponse,
		"seeding initial round state from mirror",
	)
	if !ok {
		return false
	}
	// Closing the outgoing StateResponse channel is not strictly necessary,
	// but it is a little helpful in tests in case a StateMachineRoundActionSet is mistakenly reused.
	close(as.StateResponse)

	if update.IsVRV() {
		// TODO: this is probably missing some setup to handle initial height.

		rlc.OutgoingActionsCh = as.Actions // Should this be part of the Reset method instead?

		// We have to synchronously enter the round,
		// but we still enter through the consensus manager for this.
		req := tsi.EnterRoundRequest{
			RV:     update.VRV.RoundView,
			Result: make(chan error), // Unbuffered since both sides sync on this.

			ProposalOut: rlc.ProposalCh,
		}

		err, ok := gchan.ReqResp(
			ctx, m.log,
			m.cm.EnterRoundRequests, req,
			req.Result,
			"entering round on consensus strategy while advancing height",
		)
		if !ok {
			// Context cancelled, we cannot continue.
			return false
		}
		if err != nil {
			panic(fmt.Errorf(
				"FATAL: error when calling ConsensusStrategy.EnterRound while advancing height: %v", err,
			))
		}

		if !m.beginRoundLive(ctx, rlc, update.VRV) {
			return false
		}
	} else {
		// This case could happen if we took more than an entire block to finalize,
		// due to an app bug or whatever reason.
		panic("TODO: handle replayed block upon advancing round")
	}

	return true
}
