package gservertest

import (
	"errors"
	"fmt"

	"cosmossdk.io/core/transaction"
)

// HashOnlyTransactionDecoder is a [transaction.Codec] specialized to
// decode only [HashOnlyTransaction] values.
//
// Its DecodeJSON method always returns ErrUnsupported.
//
// The type parameter for its implementation of transaction.Codec
// is fixed to [transaction.Tx], matching the behavior throughout
// the SDK's simapp code.
// Since this is for tests only, we can add a new method
// on HashOnlyTransactionDecoder that returns a value of
// [transaction.Codec[HashOnlyTransaction] if that turns out to be helpful.
type HashOnlyTransactionDecoder struct{}

var _ transaction.Codec[transaction.Tx] = HashOnlyTransactionDecoder{}

func (HashOnlyTransactionDecoder) Decode(data []byte) (transaction.Tx, error) {
	if len(data) != HashSize {
		return nil, fmt.Errorf(
			"wrong buffer size: want %d, got %d",
			HashSize, len(data),
		)
	}

	// Even though we only offer a constructor for packing a uint64 into the hash,
	// we will still do a straight copy of all 32 bytes during decode.
	var t HashOnlyTransaction
	_ = copy(t.h[:], data)
	return t, nil
}

// DecodeJSON always returns [errors.ErrUnsupported].
func (HashOnlyTransactionDecoder) DecodeJSON([]byte) (transaction.Tx, error) {
	// The package documentation for errors says to always wrap ErrUnsupported,
	// but that seems like unnecessary work given the narrow test scope of this type.
	return nil, errors.ErrUnsupported
}
