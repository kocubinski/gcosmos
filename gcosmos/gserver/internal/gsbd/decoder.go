package gsbd

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"

	"cosmossdk.io/core/transaction"
	"github.com/golang/snappy"
)

// BlockDataDecoder parses a "framed" set of block data.
// The framing format is a one-byte compression-type header,
// followed by possibly more header bytes depending on the compression format.
type BlockDataDecoder struct {
	nTxs    int
	dataLen int
	txsHash [txsHashSize]byte

	txDecoder transaction.Codec[transaction.Tx]
}

// NewBlockDataDecoder returns a new BlockDataDecoder.
//
// Rather than doing all the DecodeBlockData work in a single call,
// this validates the dataID input first,
// so that if the dataID is malformatted,
// we don't waste resources opening the reader passed to DecodeBlockData.
func NewBlockDataDecoder(
	dataID string,
	txDecoder transaction.Codec[transaction.Tx],
) (*BlockDataDecoder, error) {
	// Parse the data ID before anything else.
	// We don't need the height or round,
	// so we could potentially justify a lighter weight parser...
	// but this isn't really in a hot path, so it's probably fine.
	_, _, nTxs, dataLen, txsHash, err := ParseDataID(dataID)
	if err != nil {
		return nil, fmt.Errorf("failed to parse data ID: %w", err)
	}

	return &BlockDataDecoder{
		nTxs:    nTxs,
		dataLen: int(dataLen),
		txsHash: txsHash,

		txDecoder: txDecoder,
	}, nil
}

// Decode parses the encoded block data in r
// and returns the resulting transaction slice.
func (d *BlockDataDecoder) Decode(r io.Reader) ([]transaction.Tx, error) {
	var firstByte [1]byte
	_, err := r.Read(firstByte[:])
	if err != nil {
		return nil, fmt.Errorf("failed to read first byte from stream: %w", err)
	}

	// Two-pass decode.
	// First apply any decompression as necessary,
	// storing in the "encoded" slice.
	var encoded []byte
	switch firstByte[0] {
	case uncompressedHeader:
		// This is probably encoded = io.ReadAll(r), right?
		panic("TODO")
	case snappyHeader:
		encoded, err = d.decodeSnappy(r)
		if err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unrecognized header byte %x", firstByte[0])
	}

	// Now we can decode the raw bytes.
	return d.decodeRaw(encoded)
}

func (d *BlockDataDecoder) decodeSnappy(r io.Reader) ([]byte, error) {
	// In our snappy encoded block data,
	// the first value is the compressed size as a uvarint.
	//
	// To read that varint, we need an io.ByteReader.
	// The Stream type does not directly implement ByteReader,
	// so per the io.ByteReader docs we use a bufio.Reader instead.
	//
	// We only buffer the maximum size of a 64 bit varint.
	// That's probably more than we need, but it's fine.
	cSizeReader := bufio.NewReaderSize(r, binary.MaxVarintLen64)
	cSize64, err := binary.ReadVarint(cSizeReader)
	if err != nil {
		return nil, fmt.Errorf("failed to read compressed size: %w", err)
	}
	cSize := int(cSize64)
	if cSize <= 0 {
		return nil, fmt.Errorf(
			"invalid compressed size: %d (%d as 64-bit) must be positive",
			cSize, cSize64,
		)
	}

	maxCSize := snappy.MaxEncodedLen(d.dataLen)
	if cSize > maxCSize {
		return nil, fmt.Errorf(
			"invalid compressed size: %d larger than max snappy-encoded size %d of %d",
			cSize, maxCSize, d.dataLen,
		)
	}

	// Now that we know the size of the compressed data,
	// we can read it into an appropriately sized buffer.
	// Unfortunately, we may have some of the compressed data remaining in the varint reader,
	// so we need to concatenate its remaining data with whatever is left in the stream.
	buffered, err := cSizeReader.Peek(cSizeReader.Buffered())
	if err != nil {
		return nil, fmt.Errorf("impossible: failed to peek buffered length: %w", err)
	}

	r = io.MultiReader(bytes.NewReader(buffered), r)
	cBuf := make([]byte, cSize)
	if _, err := io.ReadFull(r, cBuf); err != nil {
		return nil, fmt.Errorf("failed to read full compressed data: %w", err)
	}

	// Now, we finally have the full compressed data.
	// Before allocating the slice for the uncompressed version,
	// double check that the uncompressed size will match.
	uSize, err := snappy.DecodedLen(cBuf)
	if err != nil {
		return nil, fmt.Errorf("failed to read decoded length from compressed data: %w", err)
	}
	if uSize != d.dataLen {
		return nil, fmt.Errorf(
			"compressed data's decoded length %d differed from expected size %d",
			uSize, d.dataLen,
		)
	}

	// Decoding into nil will right-size the output buffer anyway.
	// This could potentially use a sync.Pool to reuse allocations,
	// but right now we don't have sufficient information to justify that.
	u, err := snappy.Decode(nil, cBuf)
	if err != nil {
		return nil, fmt.Errorf("failed to decode snappy data: %w", err)
	}

	if len(u) != d.dataLen {
		return nil, fmt.Errorf(
			"impossible: decoded length %d differed from expected length %d",
			len(u), d.dataLen,
		)
	}

	return u, nil
}

func (d *BlockDataDecoder) decodeRaw(encoded []byte) ([]transaction.Tx, error) {
	// The encoded data is currently a JSON array of byte slices.
	// Not efficient, but simple to use.
	var items [][]byte
	if err := json.Unmarshal(encoded, &items); err != nil {
		return nil, fmt.Errorf("failed to unmarshal retrieved data: %w", err)
	}
	if len(items) != d.nTxs {
		return nil, fmt.Errorf(
			"unmarshalled incorrect number of encoded transactions: want %d, got %d",
			d.nTxs, len(items),
		)
	}

	txs := make([]transaction.Tx, len(items))
	for i := range items {
		tx, err := d.txDecoder.Decode(items[i])
		if err != nil {
			return nil, fmt.Errorf("failed to decode transaction at index %d: %w", i, err)
		}

		txs[i] = tx

		// Possibly help GC by removing the reference to the decoded byte slice
		// as soon as we're done with it.
		items[i] = nil
	}

	// Finally, confirm the full transaction hash.
	// If this ends up being a hot path in CPU use,
	// we could potentially accumulate the individual transaction hashes
	// while decoding the transactions.
	// But, doing it late is probably the better defensive choice.
	gotTxsHash := TxsHash(txs)
	if gotTxsHash != d.txsHash {
		return nil, fmt.Errorf(
			"decoded transactions hash %x differed from input %x",
			gotTxsHash, d.txsHash,
		)
	}

	return txs, nil
}
