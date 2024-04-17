package tmengine

import "time"

// TimeoutStrategy informs the state machine how to calculate timeouts.
// While the individual methods all include a height parameter,
// the height will rarely if ever be used in calculating the timeout duration.
// The height is more intended as a mechanism to coordinate changing the timeouts
// after a certain height.
type TimeoutStrategy interface {
	ProposalTimeout(height uint64, round uint32) time.Duration
	PrevoteDelayTimeout(height uint64, round uint32) time.Duration
	PrecommitDelayTimeout(height uint64, round uint32) time.Duration
	CommitWaitTimeout(height uint64, round uint32) time.Duration
}

// LinearTimeoutStrategy provides timeout durations that increase linearly with round increases.
// If any of the provided values are zero, reasonable defaults are used.
type LinearTimeoutStrategy struct {
	ProposalBase      time.Duration
	ProposalIncrement time.Duration

	PrevoteDelayBase      time.Duration
	PrevoteDelayIncrement time.Duration

	PrecommitDelayBase      time.Duration
	PrecommitDelayIncrement time.Duration

	CommitWaitBase      time.Duration
	CommitWaitIncrement time.Duration
}

func (s LinearTimeoutStrategy) ProposalTimeout(_ uint64, round uint32) time.Duration {
	b := s.ProposalBase
	if b == 0 {
		b = 5 * time.Second
	}
	i := s.ProposalIncrement
	if i == 0 {
		i = 500 * time.Millisecond
	}
	return b + (time.Duration(round) * i)
}

func (s LinearTimeoutStrategy) PrevoteDelayTimeout(_ uint64, round uint32) time.Duration {
	b := s.PrevoteDelayBase
	if b == 0 {
		b = 5 * time.Second
	}
	i := s.PrevoteDelayIncrement
	if i == 0 {
		i = 500 * time.Millisecond
	}
	return b + (time.Duration(round) * i)
}

func (s LinearTimeoutStrategy) PrecommitDelayTimeout(_ uint64, round uint32) time.Duration {
	b := s.PrecommitDelayBase
	if b == 0 {
		b = 5 * time.Second
	}
	i := s.PrecommitDelayIncrement
	if i == 0 {
		i = 500 * time.Millisecond
	}
	return b + (time.Duration(round) * i)
}

func (s LinearTimeoutStrategy) CommitWaitTimeout(_ uint64, round uint32) time.Duration {
	b := s.CommitWaitBase
	if b == 0 {
		b = 2 * time.Second
	}
	i := s.CommitWaitIncrement
	if i == 0 {
		i = 500 * time.Millisecond
	}
	return b + (time.Duration(round) * i)
}
