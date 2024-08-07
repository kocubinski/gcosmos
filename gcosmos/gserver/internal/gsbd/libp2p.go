package gsbd

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"

	"cosmossdk.io/core/transaction"
	"github.com/golang/snappy"
	libp2phost "github.com/libp2p/go-libp2p/core/host"
	libp2pnetwork "github.com/libp2p/go-libp2p/core/network"
	libp2ppeer "github.com/libp2p/go-libp2p/core/peer"
	libp2pprotocol "github.com/libp2p/go-libp2p/core/protocol"
)

const blockDataV1Prefix = "/gordian/blockdata/v1/"

type Libp2pHost struct {
	host libp2phost.Host

	// To get the listen addresses at runtime.
	// Although probably not likely,
	// the addresses can be changed at runtime,
	// so we should not rely on the initial value at the time of construction.
	net libp2pnetwork.Network

	router libp2pprotocol.Router
}

func NewLibp2pProviderHost(
	host libp2phost.Host,
) *Libp2pHost {
	return &Libp2pHost{
		host: host,

		net: host.Network(),

		router: host.Mux(),
	}
}

func (h *Libp2pHost) Provide(
	ctx context.Context,
	height uint64, round uint32,
	pendingTxs []transaction.Tx,
) (ProvideResult, error) {
	items := make([]json.RawMessage, len(pendingTxs))
	for i, tx := range pendingTxs {
		j, err := json.Marshal(tx)
		if err != nil {
			return ProvideResult{}, fmt.Errorf(
				"failed to marshal transaction: %w", err,
			)
		}
		items[i] = j
	}
	j, err := json.Marshal(items)
	if err != nil {
		return ProvideResult{}, fmt.Errorf(
			"failed to marshal items: %w", err,
		)
	}

	dataID := DataID(height, round, pendingTxs)

	// TODO: the router.AddHandler is a poor approach.
	// It should use host.SetStreamHandler
	// in order to get stream manipulation,
	// in particular CloseRead so we can ignore a faulty client's input.
	pID := libp2pprotocol.ID(blockDataV1Prefix + dataID)
	h.router.AddHandler(pID, h.makeBlockDataHandler(j))

	ai := libp2phost.InfoFromHost(h.host)
	jai, err := json.Marshal(ai)
	if err != nil {
		return ProvideResult{}, fmt.Errorf("failed to marshal AddrInfo %v: %w", ai, err)
	}
	locs := []Location{
		{
			Scheme: Libp2pScheme,
			Addr:   string(jai),
		},
	}

	return ProvideResult{
		DataID:   dataID,
		DataSize: len(j),
		Addrs:    locs,
	}, nil
}

const (
	uncompressedHeader byte = 0
	snappyHeader       byte = 1
)

// makeBlockDataHandler returns a handler to be set on the router,
// to serve a snappy-compressed version of j.
//
// Currently, the use of snappy compression is mandatory.
// We could add a v2 endpoint that allows the client to specify a preference,
// or we could possibly try multiple compression schemes
// and only serve the best one.
func (h *Libp2pHost) makeBlockDataHandler(j []byte) libp2pprotocol.HandlerFunc {
	c := snappy.Encode(nil, j)
	szBuf := binary.AppendVarint(nil, int64(len(c)))
	return func(_ libp2pprotocol.ID, rwc io.ReadWriteCloser) error {
		defer rwc.Close()

		if _, err := rwc.Write([]byte{snappyHeader}); err != nil {
			return err
		}

		if _, err := io.Copy(rwc, bytes.NewReader(szBuf)); err != nil {
			return err
		}

		src := bytes.NewReader(c)
		if _, err := src.WriteTo(rwc); err != nil {
			return err
		}

		return nil
	}
}

type Libp2pClient struct {
	// While we had fine-grained values in the Libp2pHost,
	// we need to hold on to the Host directly here in the client
	// because there is no apparent other interface to Connect
	// to a remote peer by its AddrInfo.

	log *slog.Logger

	h libp2phost.Host
}

func NewLibp2pClient(log *slog.Logger, host libp2phost.Host) *Libp2pClient {
	return &Libp2pClient{log: log, h: host}
}

func (c *Libp2pClient) Retrieve(
	ctx context.Context,
	ai libp2ppeer.AddrInfo,
	dataID string,
	expSize int,
) ([]byte, error) {
	// Ensure we have a connection.
	if err := c.h.Connect(ctx, ai); err != nil {
		return nil, fmt.Errorf("failed to connect to peer: %w", err)
	}

	// Open the stream to the peer.
	s, err := c.h.NewStream(ctx, ai.ID, libp2pprotocol.ID(blockDataV1Prefix+dataID))
	if err != nil {
		return nil, fmt.Errorf("failed to open stream to peer: %w", err)
	}
	defer s.Close()

	if err := s.CloseWrite(); err != nil {
		c.log.Info("Failed to close stream for write", "err", err)
		// Okay to continue anyway?
	}

	var firstByte [1]byte
	if _, err := s.Read(firstByte[:]); err != nil {
		return nil, fmt.Errorf("failed to read first byte from stream: %w", err)
	}

	switch firstByte[0] {
	case uncompressedHeader:
		panic("TODO")
	case snappyHeader:
		return c.readSnappy(expSize, s)
	default:
		return nil, fmt.Errorf("unrecognized header byte %x", firstByte[0])
	}
}

func (c *Libp2pClient) readSnappy(
	// ctx context.Context,
	expSize int,
	s libp2pnetwork.Stream,
) ([]byte, error) {
	// The next "header" section following the type header,
	// is the compressed size as a uvarint.
	//
	// To read that varint, we need an io.ByteReader.
	// The Stream type does not directly implement ByteReader,
	// so per the io.ByteReader docs we use a bufio.Reader instead.
	//
	// We only buffer the maximum size of a 64 bit varint.
	// That's probably more than we need, but it's fine.
	cSizeReader := bufio.NewReaderSize(s, binary.MaxVarintLen64)
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

	maxCSize := snappy.MaxEncodedLen(expSize)
	if cSize > maxCSize {
		return nil, fmt.Errorf(
			"invalid compressed size: %d larger than max snappy-encoded size %d of %d",
			cSize, maxCSize, expSize,
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

	r := io.MultiReader(bytes.NewReader(buffered), s)
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
	if uSize != expSize {
		return nil, fmt.Errorf(
			"compressed data's decoded length %d differed from expected size %d",
			uSize, expSize,
		)
	}

	// Decoding into nil will right-size the output buffer anyway.
	// This could potentially use a sync.Pool to reuse allocations,
	// but right now we don't have sufficient information to justify that.
	u, err := snappy.Decode(nil, cBuf)
	if err != nil {
		return nil, fmt.Errorf("failed to decode snappy data: %w", err)
	}

	if len(u) != expSize {
		return nil, fmt.Errorf(
			"impossible: decoded length %d differed from expected length %d",
			len(u), expSize,
		)
	}

	return u, nil
}
