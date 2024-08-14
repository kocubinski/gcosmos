package gtxbuf

import "fmt"

// TxInvalidError indicates that a transaction failed to apply against the state.
// For correct [Buffer] behavior, the addTxFunc argument passed to [New]
// must wrap the returned error in TxInvalidError,
// to indicate that a particular transaction could not be applied to the state.
// Any other error type is effectively fatal to the Buffer.
type TxInvalidError struct {
	Err error
}

func (e TxInvalidError) Error() string {
	return fmt.Sprintf("transaction invalid: %v", e.Err)
}

func (e TxInvalidError) Unwrap() error {
	return e.Err
}
