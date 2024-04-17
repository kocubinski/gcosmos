package tmstatetest

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

const (
	proposalTimerName       = "ProposalTimer"
	prevoteDelayTimerName   = "PrevoteDelayTimer"
	precommitDelayTimerName = "PrecommitDelayTimer"
	commitWaitTimerName     = "CommitWaitTimer"
)

type MockRoundTimer struct {
	mu sync.Mutex

	notifications map[startNotification]chan struct{}

	ch     chan struct{}
	cancel func()

	activeName string
	activeH    uint64
	activeR    uint32
}

type startNotification struct {
	Name string
	H    uint64
	R    uint32
}

func (t *MockRoundTimer) ProposalTimer(_ context.Context, h uint64, r uint32) (<-chan struct{}, func()) {
	return t.makeTimer(proposalTimerName, h, r)
}

func (t *MockRoundTimer) PrevoteDelayTimer(_ context.Context, h uint64, r uint32) (<-chan struct{}, func()) {
	return t.makeTimer(prevoteDelayTimerName, h, r)
}

func (t *MockRoundTimer) PrecommitDelayTimer(_ context.Context, h uint64, r uint32) (<-chan struct{}, func()) {
	return t.makeTimer(precommitDelayTimerName, h, r)
}

func (t *MockRoundTimer) CommitWaitTimer(_ context.Context, h uint64, r uint32) (<-chan struct{}, func()) {
	return t.makeTimer(commitWaitTimerName, h, r)
}

func (t *MockRoundTimer) makeTimer(name string, h uint64, r uint32) (<-chan struct{}, func()) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.ch != nil {
		panic(fmt.Errorf(
			"BUG: cannot create %s before previous timer elapses or is cancelled",
			name,
		))
	}

	var ch = make(chan struct{})
	t.ch = ch
	t.cancel = func() {
		t.mu.Lock()
		defer t.mu.Unlock()

		if t.ch != ch {
			// Guard against late/deferred cancel affecting anything else.
			return
		}

		t.ch = nil
		t.cancel = nil

		t.activeName = ""
		t.activeH = 0
		t.activeR = 0
	}

	t.activeName = name
	t.activeH = h
	t.activeR = r

	sn := startNotification{Name: name, H: h, R: r}
	if ch, ok := t.notifications[sn]; ok {
		close(ch)
		delete(t.notifications, sn)
	}

	return t.ch, t.cancel
}

func (t *MockRoundTimer) ActiveTimer() (name string, h uint64, r uint32) {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.activeName, t.activeH, t.activeR
}

func (t *MockRoundTimer) ElapseProposalTimer(h uint64, r uint32) error {
	return t.elapse(proposalTimerName, h, r)
}

func (t *MockRoundTimer) ElapsePrevoteDelayTimer(h uint64, r uint32) error {
	return t.elapse(prevoteDelayTimerName, h, r)
}

func (t *MockRoundTimer) ElapsePrecommitDelayTimer(h uint64, r uint32) error {
	return t.elapse(precommitDelayTimerName, h, r)
}

func (t *MockRoundTimer) ElapseCommitWaitTimer(h uint64, r uint32) error {
	return t.elapse(commitWaitTimerName, h, r)
}

func (t *MockRoundTimer) elapse(name string, h uint64, r uint32) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.activeName != name {
		if t.activeName == "" {
			return fmt.Errorf("requested to elapse timer %q, but no timer active", name)
		}
		return fmt.Errorf("requested to elapse timer %q when %q active", name, t.activeName)
	}

	if t.activeH != h || t.activeR != r {
		return fmt.Errorf(
			"requested to elapse timer %q at %d/%d, but it is active for %d/%d",
			name, h, r, t.activeH, t.activeR,
		)
	}

	close(t.ch)
	t.cancel = nil

	t.activeName = ""
	t.activeH = 0
	t.activeR = 0

	return nil
}

func (t *MockRoundTimer) ProposalStartNotification(h uint64, r uint32) <-chan struct{} {
	return t.startNotification(proposalTimerName, h, r)
}

func (t *MockRoundTimer) PrevoteDelayStartNotification(h uint64, r uint32) <-chan struct{} {
	return t.startNotification(prevoteDelayTimerName, h, r)
}

func (t *MockRoundTimer) PrecommitDelayStartNotification(h uint64, r uint32) <-chan struct{} {
	return t.startNotification(precommitDelayTimerName, h, r)
}

func (t *MockRoundTimer) CommitWaitStartNotification(h uint64, r uint32) <-chan struct{} {
	return t.startNotification(commitWaitTimerName, h, r)
}

func (t *MockRoundTimer) startNotification(name string, h uint64, r uint32) <-chan struct{} {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.notifications == nil {
		t.notifications = make(map[startNotification]chan struct{})
	}

	key := startNotification{Name: name, H: h, R: r}

	if _, ok := t.notifications[key]; ok {
		panic(fmt.Errorf("notification already created for %q at %d/%d", name, h, r))
	}

	ch := make(chan struct{})
	t.notifications[key] = ch
	return ch
}

func (t *MockRoundTimer) RequireNoActiveTimer(tt *testing.T) {
	tt.Helper()

	t.mu.Lock()
	defer t.mu.Unlock()

	if t.activeName != "" {
		tt.Fatalf(
			"expected no active timer, but got %s at h=%d/r=%d",
			t.activeName, t.activeH, t.activeR,
		)
	}
}

func (t *MockRoundTimer) RequireActiveProposalTimer(tt *testing.T, height uint64, round uint32) {
	tt.Helper()

	t.requireActiveTimer(tt, proposalTimerName, height, round)
}

func (t *MockRoundTimer) RequireActivePrevoteDelayTimer(tt *testing.T, height uint64, round uint32) {
	tt.Helper()

	t.requireActiveTimer(tt, prevoteDelayTimerName, height, round)
}

func (t *MockRoundTimer) RequireActivePrecommitDelayTimer(tt *testing.T, height uint64, round uint32) {
	tt.Helper()

	t.requireActiveTimer(tt, precommitDelayTimerName, height, round)
}

func (t *MockRoundTimer) RequireActiveCommitWaitTimer(tt *testing.T, height uint64, round uint32) {
	tt.Helper()

	t.requireActiveTimer(tt, commitWaitTimerName, height, round)
}

func (t *MockRoundTimer) requireActiveTimer(tt *testing.T, name string, h uint64, r uint32) {
	tt.Helper()

	t.mu.Lock()
	defer t.mu.Unlock()

	if t.activeName == "" {
		tt.Fatalf("expected active %s, but no timer was active", name)
	}
	if t.activeName != name || t.activeH != h || t.activeR != r {
		tt.Fatalf(
			"expected active %s at h=%d/r=%d, but got %s at h=%d/r=%d",
			name, h, r, t.activeName, t.activeH, t.activeR,
		)
	}
}
