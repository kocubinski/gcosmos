package gp2papi

import (
	"context"
	"encoding/json"
	"log/slog"
	"strconv"
	"strings"
	"time"

	libp2phost "github.com/libp2p/go-libp2p/core/host"
	libp2pnetwork "github.com/libp2p/go-libp2p/core/network"
	libp2pprotocol "github.com/libp2p/go-libp2p/core/protocol"
	"github.com/rollchains/gordian/tm/tmcodec"
	"github.com/rollchains/gordian/tm/tmstore"
)

type DataHost struct {
	// We unfortunately store the life context of the DataHost as a field,
	// because there does not appear to be a way to get
	// a context associated with a particular stream.
	ctx context.Context

	log *slog.Logger

	host libp2phost.Host

	hs tmstore.HeaderStore

	codec tmcodec.MarshalCodec

	done chan struct{}
}

const headerV1HeightPrefix = "/gcosmos/committed_headers/v1/height/"

func NewDataHost(
	ctx context.Context,
	log *slog.Logger,
	host libp2phost.Host,
	hs tmstore.HeaderStore,
	codec tmcodec.MarshalCodec,
) *DataHost {
	h := &DataHost{
		ctx:   ctx,
		log:   log,
		host:  host,
		hs:    hs,
		codec: codec,

		done: make(chan struct{}),
	}

	go h.waitForCancellation()

	h.host.SetStreamHandlerMatch(
		libp2pprotocol.ID(headerV1HeightPrefix),
		func(id libp2pprotocol.ID) bool {
			heightS, ok := strings.CutPrefix(string(id), headerV1HeightPrefix)
			if !ok {
				return false
			}

			if len(heightS) == 0 {
				return false
			}

			for _, rn := range heightS {
				if rn < '0' || rn > '9' {
					return false
				}
			}

			return true
		},
		h.handleCommittedHeaderStream,
	)

	return h
}

func (h *DataHost) Wait() {
	<-h.done
}

func (h *DataHost) waitForCancellation() {
	// Removing the handler probably isn't strictly necessary,
	// but this would allow us to dynamically enable the service
	// without shutting down the whole process.
	<-h.ctx.Done()
	h.host.RemoveStreamHandler(libp2pprotocol.ID(headerV1HeightPrefix))
	close(h.done)
}

func (h *DataHost) handleCommittedHeaderStream(s libp2pnetwork.Stream) {
	// TODO: this is unfortunate that we need to distinguish errors from actual results
	// and we are stuck with JSON for now.
	// There isn't necessarily a clear way to generically handle the wrapped errors;
	// I don't think we can assume analogs to json.RawMessage in other encodings.
	// Or we adjust the tmcodec package to allow an OrError variant.
	//
	// We can live with this for now in any case.

	defer s.Close()

	// We don't read any input on this path.
	_ = s.CloseRead()

	// The stream value apparently has no way to retrieve a corresponding context,
	// so we use the root context associated with the DataHost
	// and add a 1-second timeout, which ought to suffice for any reasonable client.
	ctx, cancel := context.WithTimeout(h.ctx, time.Second)
	defer cancel()

	heightS, ok := strings.CutPrefix(string(s.Protocol()), headerV1HeightPrefix)
	if !ok {
		_ = json.NewEncoder(s).Encode(JSONResult{
			Err: "invalid protocol",
		})
		return
	}

	height, err := strconv.ParseUint(heightS, 10, 64)
	if err != nil {
		_ = json.NewEncoder(s).Encode(JSONResult{
			Err: "invalid height",
		})
		return
	}

	ch, err := h.hs.LoadHeader(ctx, height)
	if err != nil {
		// TODO: There are probably certain cases we do want to expose.
		// For now, just mask it.
		_ = json.NewEncoder(s).Encode(JSONResult{
			Err: "failed to load header at height",
		})
		return
	}

	b, err := h.codec.MarshalCommittedHeader(ch)
	if err != nil {
		h.log.Info(
			"Failed to marshal requested committed header",
			// TODO: include height in log.
			"err", err,
		)
		_ = json.NewEncoder(s).Encode(JSONResult{
			Err: "internal serialization error",
		})
		return
	}

	_ = json.NewEncoder(s).Encode(JSONResult{
		Result: b,
	})
}

// JSONResult is currently used to wrap results from gp2papi calls.
// It is likely to be superseded by something else soon.
type JSONResult struct {
	Result json.RawMessage `json:",omitempty"`
	Err    string          `json:",omitempty"`
}
