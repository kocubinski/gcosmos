package gtxbuf

import (
	"context"
	"errors"
)

type workingState[S, T any] struct {
	BaseState S

	curState  S
	isUpdated bool

	Txs []T

	addTx     func(context.Context, S, T) (S, error)
	deleteTxs func(ctx context.Context, have *[]T, reject []T)
}

func (w *workingState[S, T]) CheckAddTx(
	ctx context.Context,
	tx T,
) error {
	var cur S
	if w.isUpdated {
		cur = w.curState
	} else {
		cur = w.BaseState
	}

	newState, err := w.addTx(ctx, cur, tx)
	if err != nil {
		return err
	}

	// On success, update the current state.
	w.curState = newState
	w.isUpdated = true
	w.Txs = append(w.Txs, tx)

	return nil
}

func (w *workingState[S, T]) Buffered(dst []T) []T {
	dst = append(dst, w.Txs...)
	return dst
}

func (w *workingState[S, T]) Rebase(
	ctx context.Context, newBase S, applied []T,
) rebaseResponse[T] {
	w.BaseState = newBase
	w.curState = newBase
	w.isUpdated = false

	if len(w.Txs) == 0 {
		// No further work required.
		return rebaseResponse[T]{}
	}

	if len(applied) > 0 {
		// Avoid calling back into user code if there is nothing to delete.
		w.deleteTxs(ctx, &w.Txs, applied)
	}

	var invalidated []T

	for _, tx := range w.Txs {
		newState, err := w.addTx(ctx, w.curState, tx)
		if err != nil {
			if errors.As(err, new(TxInvalidError)) {
				// Simple invalid transaction.
				// Add it to the invalidated collection.
				invalidated = append(invalidated, tx)
				continue
			}

			// Otherwise, it wasn't a simple transaction error,
			// so we have to fail now.
			return rebaseResponse[T]{Err: err}
		}

		// We have new state from successfully applying this transaction.
		w.curState = newState
		w.isUpdated = true
	}

	// All transactions were applied or invalidated.
	// Prune the invalidated transactions, if any exist.
	if len(invalidated) > 0 {
		w.deleteTxs(ctx, &w.Txs, invalidated)
	}

	return rebaseResponse[T]{Invalidated: invalidated}
}
