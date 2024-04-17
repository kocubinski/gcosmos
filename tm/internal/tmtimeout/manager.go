package tmtimeout

import "context"

// Manager is a copy of [tmengine.TimeoutManager].
// The declaration in [tmengine] is canonical so that readers don't have to follow
// a type alias to see the underlying declaration.
type Manager interface {
	WithProposalTimeout(ctx context.Context, height uint64, round uint32) (context.Context, context.CancelFunc)
	WithPrevoteDelayTimeout(ctx context.Context, height uint64, round uint32) (context.Context, context.CancelFunc)
	WithPrecommitDelayTimeout(ctx context.Context, height uint64, round uint32) (context.Context, context.CancelFunc)
	WithCommitWaitTimeout(ctx context.Context, height uint64, round uint32) (context.Context, context.CancelFunc)
}
