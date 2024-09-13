package gsbd

import (
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"cosmossdk.io/core/transaction"
	"golang.org/x/crypto/blake2b"
)

const txsHashSize = 32

// We need the zero hash for the special, but probably common,
// case of zero transactions.
//go:generate go run ./dataid_generate.go

// TxsHash returns the accumulated hash resulting from
// the hash of the concatenated transaction hashes.
// This is the last segment of the data ID.
func TxsHash(txs []transaction.Tx) [txsHashSize]byte {
	hasher, err := blake2b.New(txsHashSize, nil)
	if err != nil {
		panic(fmt.Errorf("impossible: blake2b.New failed: %w", err))
	}

	for _, tx := range txs {
		hash := tx.Hash()
		_, _ = hasher.Write(hash[:])
	}

	txsHash := make([]byte, 0, txsHashSize)
	txsHash = hasher.Sum(txsHash)
	out := [txsHashSize]byte{}
	_ = copy(out[:], txsHash)
	return out
}

// DataID returns a string formatted as:
//
//	HEIGHT:ROUND:NUM_TXs:DATA_LEN:HASH(TXs)
//
// Where the HEIGHT, ROUND, and NUM_TXs are written as decimal numbers in ASCII,
// so that they are easily human-readable.
// DATA_LEN is the length of the raw data in bytes,
// encoded in lowercase hexadecimal to slightly shorten the data ID,
// and because it is less important that this value is human-readable.
// HASH(TXs) is the blake2b hash of the concatenation
// of every transaction in txs, formatted as lowercase hex-encoded bytes.
func DataID(
	height uint64,
	round uint32,
	dataLen uint32,
	txs []transaction.Tx,
) string {
	if len(txs) == 0 {
		if dataLen != 0 {
			panic(fmt.Errorf("BUG: got dataLen=%d with 0 transactions", dataLen))
		}

		return fmt.Sprintf("%d:%d%s", height, round, zeroHashSuffix)
	}

	return fmt.Sprintf("%d:%d:%d:%x:%x", height, round, len(txs), dataLen, TxsHash(txs))
}

func ParseDataID(id string) (
	height uint64, round uint32,
	nTxs int,
	dataLen uint32,
	txsHash [txsHashSize]byte,
	err error,
) {
	if hr, ok := strings.CutSuffix(id, zeroHashSuffix); ok {
		parts := strings.SplitN(hr, ":", 3) // Yes, 3, not 2.
		if len(parts) != 2 {
			// Should be left with only height and round.
			err = fmt.Errorf(
				"invalid data ID format; expected HEIGHT:ROUND:N_TXS:DATA_LEN:TXS_HASH; got %q", id,
			)
			return
		}

		height, err = strconv.ParseUint(parts[0], 10, 64)
		if err != nil {
			err = fmt.Errorf("failed to parse height: %w", err)
			return
		}

		var u64 uint64
		u64, err = strconv.ParseUint(parts[1], 10, 32)
		if err != nil {
			err = fmt.Errorf("failed to parse round: %w", err)
			return
		}
		round = uint32(u64)

		// nTxs and dataLen are already initialized to zero, don't need to reassign.

		copy(txsHash[:], zeroHash)

		return
	}

	parts := strings.SplitN(id, ":", 6) // Yes, 6, not 5.
	if len(parts) != 5 {
		err = fmt.Errorf(
			"invalid data ID format; expected HEIGHT:ROUND:N_TXS:DATA_LEN:TXS_HASH; got %q", id,
		)
		return
	}

	height, err = strconv.ParseUint(parts[0], 10, 64)
	if err != nil {
		err = fmt.Errorf("failed to parse height: %w", err)
		return
	}

	u64, err := strconv.ParseUint(parts[1], 10, 32)
	if err != nil {
		err = fmt.Errorf("failed to parse round: %w", err)
		return
	}
	round = uint32(u64)

	s64, err := strconv.ParseInt(parts[2], 10, 32)
	if err != nil {
		err = fmt.Errorf("failed to parse number of txs: %w", err)
		return
	}
	nTxs = int(s64)

	// The data length is encoded in, and therefore also parsed in, hexadecimal.
	u64, err = strconv.ParseUint(parts[3], 16, 32)
	if err != nil {
		err = fmt.Errorf("failed to parse data length: %w", err)
		return
	}
	dataLen = uint32(u64)

	if wantLen := hex.EncodedLen(txsHashSize); len(parts[4]) != wantLen {
		// We could check this earlier to avoid work in parsing the numbers.
		err = fmt.Errorf("wrong length for txs hash; want %d, got %d", wantLen, len(parts[4]))
		return
	}
	_, err = hex.AppendDecode(txsHash[:0], []byte(parts[4]))
	if err != nil {
		err = fmt.Errorf("failed to decode txs hash: %w", err)
		return
	}

	// We've parsed everything, and err is nil.
	return
}

func IsZeroTxDataID(id string) bool {
	return strings.HasSuffix(id, zeroHashSuffix)
}
