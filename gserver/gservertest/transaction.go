package gservertest

import (
	"encoding/binary"
	"errors"
	"sync/atomic"

	"cosmossdk.io/core/transaction"
)

const HashSize = 32

// HashOnlyTransaction satisfies the [transaction.Tx] interface,
// while only implementing the Hash method.
// All other transaction.Tx methods return [ErrOnlyHashImplemented].
//
// The value of the hash is determined by the value passed to
// [NewHashOnlyTransaction].
type HashOnlyTransaction struct {
	h [HashSize]byte
}

var _ transaction.Tx = HashOnlyTransaction{}

// NewHashOnlyTransaction returns a HashOnlyTransaction,
// whose hash is the little endian representation of hashNum,
// padded with zeros out to [HashSize] bytes.
//
// Use NewHashOnlyTransaction when hash values for transactions
// within a test must be predictable.
// Otherwise, use [NextHashOnlyTransaction] to avoid manual bookkeeping.
func NewHashOnlyTransaction(hashNum uint64) HashOnlyTransaction {
	var h [HashSize]byte
	binary.LittleEndian.PutUint64(h[:], hashNum)
	return HashOnlyTransaction{h: h}
}

func NewRawHashOnlyTransaction(hash [HashSize]byte) HashOnlyTransaction {
	return HashOnlyTransaction{h: hash}
}

var globalTxCounter uint64

// NextHashOnlyTransaction returns a HashOnlyTransaction
// using a package-level counter, so that tests within a single package
// all use unique values within calls to this function.
//
// If predictable hash values are necessary for a test, use NewHashOnlyTransaction.
// Note that uniqueness is only guaranteed within calls to NextHashOnlyTransaction,
// and it is possible to choose a repeated value using calls to NewHashOnlyTransaction.
func NextHashOnlyTransaction() HashOnlyTransaction {
	n := atomic.AddUint64(&globalTxCounter, 1)
	return NewHashOnlyTransaction(n)
}

func (t HashOnlyTransaction) Hash() [HashSize]byte {
	return t.h
}

func (HashOnlyTransaction) GetMessages() ([]transaction.Msg, error) {
	return nil, ErrOnlyHashImplemented
}

func (HashOnlyTransaction) GetSenders() ([]transaction.Identity, error) {
	return nil, ErrOnlyHashImplemented
}

func (HashOnlyTransaction) GetGasLimit() (uint64, error) {
	return 0, ErrOnlyHashImplemented
}

// Bytes returns a copy of the hash bytes of t.
func (t HashOnlyTransaction) Bytes() []byte {
	z := t.h
	return z[:]
}

var ErrOnlyHashImplemented = errors.New("HashOnlyTransaction only implements the Hash method")
