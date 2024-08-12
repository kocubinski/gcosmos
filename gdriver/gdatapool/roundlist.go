package gdatapool

import (
	"context"
	"errors"
)

// roundList is an effective write-synchronized singly linked list
// of height, round, and context values.
//
// The [worker] uses the context to control the lifecycle
// of block data retrieval.
type roundList struct {
	Height uint64
	Round  uint32

	Ctx context.Context

	Next *roundList
}

// FastForward advances to the end of the round list
// and reports whether it advanced (whether the height or round changed).
// The quit value is true if the round list's context was cancelled
// for any reason other than round over.
func (r *roundList) FastForward() (didAdvance, quit bool) {
	for {
		select {
		case <-r.Ctx.Done():
			if cause := context.Cause(r.Ctx); cause != errRoundOver {
				// Shouldn't really amtter if we advanced when quit=true,
				// but report it anyway.
				return didAdvance, true
			}

			r = r.Next
			didAdvance = true
			continue

		default:
			// We've reached the current end of the round list.
			// Report whether we advanced.
			return didAdvance, false
		}
	}
}

// Sentinel error to signal the end of a round,
// to be distinguished from a normal context cancellation.
var errRoundOver = errors.New("round over")
