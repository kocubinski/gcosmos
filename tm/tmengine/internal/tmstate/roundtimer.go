package tmstate

import (
	"context"
	"errors"
	"sync"
	"time"
)

// RoundTimer is the interface the state machine uses to manage timeouts per step.
// While using a [time.Timer] directly would be simpler,
// that would pose difficulty in fine-grained management of timers during tests.
// So instead, the RoundTimer offers a set of methods that return a channel that will close upon a timeout,
// and an associated cancel function that must be called to release resources.
// It is safe to call the cancel function multiple times, and concurrently, if needed.
//
// Note that calling the cancel function will not close the returned channel,
// as to avoid spuriously indicating a timer has elapsed.
//
// The context argument is used only for communicating with any coordination goroutines;
// it has no bearing on when the returned channel is closed.
// If the context is cancelled while attempting to get a timer,
// the returned channel is nil and the returned cancel function is a no-op non-nil function.
type RoundTimer interface {
	ProposalTimer(ctx context.Context, height uint64, round uint32) (ch <-chan struct{}, cancel func())
	PrevoteDelayTimer(ctx context.Context, height uint64, round uint32) (ch <-chan struct{}, cancel func())
	PrecommitDelayTimer(ctx context.Context, height uint64, round uint32) (ch <-chan struct{}, cancel func())
	CommitWaitTimer(ctx context.Context, height uint64, round uint32) (ch <-chan struct{}, cancel func())
}

// TimeoutStrategy defines how to calculate the timeout durations
// for a [StandardRoundTimer].
type TimeoutStrategy interface {
	ProposalTimeout(height uint64, round uint32) time.Duration
	PrevoteDelayTimeout(height uint64, round uint32) time.Duration
	PrecommitDelayTimeout(height uint64, round uint32) time.Duration
	CommitWaitTimeout(height uint64, round uint32) time.Duration
}

// StandardRoundTimer is the default implementation of [RoundTimer],
// backed by actual [time.Timer] instances.
type StandardRoundTimer struct {
	strat TimeoutStrategy

	startTimerRequests chan startTimerRequest

	bgDone chan struct{}
}

type startTimerRequest struct {
	Dur  time.Duration
	Resp chan startTimerResponse
}

type startTimerResponse struct {
	Elapsed <-chan struct{}
	Cancel  func()
}

func NewStandardRoundTimer(ctx context.Context, s TimeoutStrategy) *StandardRoundTimer {
	t := &StandardRoundTimer{
		strat: s,

		startTimerRequests: make(chan startTimerRequest),

		bgDone: make(chan struct{}),
	}

	go t.background(ctx)

	return t
}

func (t *StandardRoundTimer) Wait() {
	<-t.bgDone
}

func (t *StandardRoundTimer) background(ctx context.Context) {
	defer close(t.bgDone)

	// One timer for the main loop.
	timer := time.NewTimer(time.Hour) // Long enough that it should be impossible to hit within one goroutine.
	defer timer.Stop()                // Unconditional defer in case we hit an early return.

	// And an unconditional stop call,
	// because the first start timer request requires that the timer is stopped upon entry.
	if !timer.Stop() {
		select {
		case <-timer.C:
			// Okay.
		case <-ctx.Done():
			return
		}
	}

	var timerElapsed, cancelTimer chan struct{}

	for {
		// Wait for signal to start timer.
		select {
		case <-ctx.Done():
			return

		case req := <-t.startTimerRequests:
			// We assume the timer is always stopped by the time we receive a valid start timer request.
			// If the timer is stopped, then we are safe to reset.
			timer.Reset(req.Dur)

			timerElapsed = make(chan struct{})
			cancelTimer = make(chan struct{})
			// Local reference so the returned cancel function
			// doesn't have a closure over the outer variable.
			localCancel := cancelTimer
			var cancelOnce sync.Once
			// The caller should be blocking on the receive here,
			// so we should be safe to do a blocking send.
			req.Resp <- startTimerResponse{
				Elapsed: timerElapsed,
				Cancel: func() {
					cancelOnce.Do(func() {
						close(localCancel)
					})
				},
			}
		}

		// The timer is running.
		select {
		case <-ctx.Done():
			return

		case <-timer.C:
			// The timer elapsed.
			close(timerElapsed)
			timerElapsed = nil
			cancelTimer = nil

		case <-cancelTimer:
			// We need to stop the timer, to avoid leaking resources.
			if !timer.Stop() {
				select {
				case <-timer.C:
					// Okay.
				case <-ctx.Done():
					return
				}
			}

			// Don't close the channel on cancel.
			// Closing it would allow a read to indicate an elapse,
			// which we should assume is undesired.
			timerElapsed = nil
			cancelTimer = nil

		case <-t.startTimerRequests:
			panic(errors.New(
				"BUG: new timer requested before previous timer elapsed or was cancelled",
			))
		}
	}
}

func (t *StandardRoundTimer) getTimer(ctx context.Context, dur time.Duration) (<-chan struct{}, func()) {
	respCh := make(chan startTimerResponse)
	req := startTimerRequest{
		Dur:  dur,
		Resp: respCh,
	}

	select {
	case t.startTimerRequests <- req:
		// Okay.
	case <-ctx.Done():
		return nil, func() {}
	}

	select {
	case resp := <-respCh:
		return resp.Elapsed, resp.Cancel
	case <-ctx.Done():
		return nil, func() {}
	}
}

func (t *StandardRoundTimer) ProposalTimer(ctx context.Context, height uint64, round uint32) (<-chan struct{}, func()) {
	return t.getTimer(ctx, t.strat.ProposalTimeout(height, round))
}

func (t *StandardRoundTimer) PrevoteDelayTimer(ctx context.Context, height uint64, round uint32) (<-chan struct{}, func()) {
	return t.getTimer(ctx, t.strat.PrevoteDelayTimeout(height, round))
}

func (t *StandardRoundTimer) PrecommitDelayTimer(ctx context.Context, height uint64, round uint32) (<-chan struct{}, func()) {
	return t.getTimer(ctx, t.strat.PrecommitDelayTimeout(height, round))
}

func (t *StandardRoundTimer) CommitWaitTimer(ctx context.Context, height uint64, round uint32) (<-chan struct{}, func()) {
	return t.getTimer(ctx, t.strat.CommitWaitTimeout(height, round))
}
