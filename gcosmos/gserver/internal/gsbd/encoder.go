package gsbd

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"

	"cosmossdk.io/core/transaction"
	"github.com/golang/snappy"
)

// EncodeBlockData encodes a set of transactions as block data.
// The encoding format is as follows:
//
//  1. A header byte indicating the compression format,
//     possibly indicating uncompressed.
//  2. A varint indicating the length of the maybe-compressed data
//     (see [binary.AppendVarint]).
//  3. The maybe compressed data, which is currently inefficiently coded as
//     a JSON array of base64 data (in Go, it is a [][]byte that is JSON-marshalled).
//
// The returned decodedDataSize is the size of the uncompressed data,
// to be provided to the [DataID] function.
//
// EncodeBlockData panics when len(txs) == 0.
// Use [DataID] with arguments (height, round, 0, nil) directly
// to get the data ID in that case.
func EncodeBlockData(w io.Writer, txs []transaction.Tx) (
	decompressedDataSize int, err error,
) {
	if len(txs) == 0 {
		panic("BUG: do not call EncodeBlockData with an empty set of transactions")
	}

	items := make([][]byte, len(txs))
	for i, tx := range txs {
		items[i] = tx.Bytes()
	}

	j, err := json.Marshal(items)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal encoded transactions")
	}

	return compressEncodedBlockData(w, j)
}

func compressEncodedBlockData(w io.Writer, j []byte) (int, error) {
	var header byte
	var szBuf []byte
	var data *bytes.Reader

	if c := snappy.Encode(nil, j); len(c) < len(j) {
		// We've saved some bytes, so use the snappy stream.
		header = snappyHeader
		szBuf = binary.AppendVarint(nil, int64(len(c)))
		data = bytes.NewReader(c)
	} else {
		header = uncompressedHeader
		szBuf = binary.AppendVarint(nil, int64(len(j)))
		data = bytes.NewReader(j)
	}

	if _, err := w.Write([]byte{header}); err != nil {
		return 0, fmt.Errorf("failed to write compression header: %w", err)
	}
	if _, err := io.Copy(w, bytes.NewReader(szBuf)); err != nil {
		return 0, fmt.Errorf("failed to write size: %w", err)
	}
	if _, err := data.WriteTo(w); err != nil {
		return 0, fmt.Errorf("failed to write maybe-compressed data: %w", err)
	}
	return len(j), nil
}
