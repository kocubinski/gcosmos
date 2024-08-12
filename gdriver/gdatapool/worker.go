package gdatapool

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
)

type worker[T any] struct {
	log *slog.Logger

	roundList *roundList

	retriever Retriever[T]
}

func (w *worker[T]) Run(
	wg *sync.WaitGroup,
	retrieveRequests <-chan retrieveRequest[T],
) {
	defer wg.Done()

	for {
		select {
		case <-w.roundList.Ctx.Done():
			if cause := context.Cause(w.roundList.Ctx); cause != errRoundOver {
				w.log.Info("Stopping due to context cancellation", "cause", cause)
				return
			}

			// If it was round over, we can definitely advance the round list once.
			w.roundList = w.roundList.Next

			// See how much further we can advance.
			if _, quit := w.roundList.FastForward(); quit {
				w.log.Info("Stopping due to context cancellation", "cause", context.Cause(w.roundList.Ctx))
				return
			}

		case req := <-retrieveRequests:
			// Incoming retrieve request.

			// If this looks old, discard it.
			if req.Height < w.roundList.Height || (req.Height == w.roundList.Height && req.Round < w.roundList.Round) {
				// Don't need to clutter logs with discarding an old request.
				continue
			}

			// If it looks like it's in the future, advance the round list.
			if req.Height > w.roundList.Height || (req.Height == w.roundList.Height && req.Round > w.roundList.Round) {
				didAdvance, quit := w.roundList.FastForward()
				if quit {
					w.log.Info("Stopping due to context cancellation", "cause", context.Cause(w.roundList.Ctx))
					return
				}

				// If we did advance, check if we advanced past the request.
				if didAdvance {
					if req.Height < w.roundList.Height || (req.Height == w.roundList.Height && req.Round < w.roundList.Round) {
						// Don't log the old request.
						continue
					}
				}

				// And if we didn't advance, then the request is still in the future, so we have to drop it.
				if !didAdvance {
					w.log.Warn(
						"Received request to retrieve block data for future height/round; dropping the request",
						"recv_h", req.Height, "recv_r", req.Round,
						"h", w.roundList.Height, "r", w.roundList.Round,
					)
					continue
				}
			}

			// If we got this far, then whether we advanced or not,
			// we should be on the matching height and round.
			// Temporary panic to assert that is the case.
			if req.Height != w.roundList.Height || req.Round != w.roundList.Round {
				panic(fmt.Errorf(
					"BUG: would have handled retrieval request for %d/%d when actually on %d/%d",
					req.Height, req.Round, w.roundList.Height, w.roundList.Round,
				))
			}

			// Now we want to do the retrieval in the foreground.
			// If the context is cancelled while this is executing,
			// hopefully the execution fails immediately,
			// and then we will stop on the next iteration of this loop.
			w.retrieveData(req)
		}
	}
}

func (w *worker[T]) retrieveData(req retrieveRequest[T]) {
	req.Result.Value, req.Result.Error = w.retriever.Retrieve(
		w.roundList.Ctx,
		req.DataID,
		req.Metadata,
	)
	close(req.Ready)
}
