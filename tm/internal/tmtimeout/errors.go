package tmtimeout

import "errors"

// These errors are sentinel errors used to indicate elapsed timeouts.
// See [tmengine.Timer] for more details.
// They are declared in this package because
// some of the internal tmstate types depend on them.
var (
	ErrProposalTimedOut   = errors.New("proposal timed out")
	ErrCommitWaitTimedOut = errors.New("commit wait timed out")

	ErrPrevoteDelayTimedOut   = errors.New("prevote delay timed out")
	ErrPrecommitDelayTimedOut = errors.New("precommit delay timed out")
)
