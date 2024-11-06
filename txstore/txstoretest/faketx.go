package txstoretest

import (
	"cosmossdk.io/core/transaction"
)

var _ transaction.Tx = (*FakeTx)(nil)

type FakeTx struct {
	TxHash  [32]byte
	TxBytes []byte

	Messages []transaction.Msg
	GasLimit uint64
}

func (f *FakeTx) Hash() [32]byte {
	return f.TxHash
}

func (f *FakeTx) GetMessages() ([]transaction.Msg, error) {
	return f.Messages, nil
}

func (f *FakeTx) GetSenders() ([]transaction.Identity, error) {
	// Is this okay?
	return nil, nil
}

func (f *FakeTx) GetGasLimit() (uint64, error) {
	return f.GasLimit, nil
}

func (f *FakeTx) Bytes() []byte {
	return f.TxBytes
}
