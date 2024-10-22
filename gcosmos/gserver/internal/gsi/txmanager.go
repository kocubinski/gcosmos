package gsi

import (
	"context"
	"fmt"

	corestore "cosmossdk.io/core/store"
	"cosmossdk.io/core/transaction"
	"cosmossdk.io/server/v2/appmanager"
	"github.com/gordian-engine/gordian/gdriver/gtxbuf"
)

// SDKTxBuf is aliased to the fully qualified gtx.Buffer,
// just to simplify declarations elsewhere through the gserver package tree.
// The two long names for the generic types are unpleasant to repeat everywhere.
type SDKTxBuf = gtxbuf.Buffer[corestore.ReaderMap, transaction.Tx]

// TxManager wraps an AppManager in order to provide the callback functions
// needed to instantiate a [gtxbuf.Buffer].
type TxManager struct {
	AppManager appmanager.AppManager[transaction.Tx]
}

// AddTx uses m's AppManager to simulate the transaction against the given state.
// On success, it returns the new state.
// As of writing, AppManager.SimulateWithState does not return an error,
// although its signature allows for it.
// If that error is non-nil, it is returned wrapped.
// Otherwise, if the transaction result contains an error,
// AddTx returns that error wrapped in [gtxbuf.TxInvalidError].
func (m TxManager) AddTx(
	ctx context.Context, state corestore.ReaderMap, tx transaction.Tx,
) (corestore.ReaderMap, error) {
	txRes, newState, err := m.AppManager.SimulateWithState(ctx, state, tx)
	if err != nil {
		return nil, fmt.Errorf("failed to execute AppManager.SimulateWithState: %w", err)
	}
	if txRes.Error != nil {
		return nil, gtxbuf.TxInvalidError{Err: txRes.Error}
	}

	return newState, nil
}

// TxDeleterFunc returns a function that reports true
// when given a transaction with a hash identical to
// any transaction in the reject slice.
func (m TxManager) TxDeleterFunc(
	ctx context.Context, reject []transaction.Tx,
) func(tx transaction.Tx) bool {
	rejectMap := make(map[[32]byte]struct{}, len(reject))
	for _, r := range reject {
		rejectMap[r.Hash()] = struct{}{}
	}

	return func(tx transaction.Tx) bool {
		_, ok := rejectMap[tx.Hash()]
		return ok
	}
}
