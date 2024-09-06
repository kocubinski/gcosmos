package gp2papi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"runtime/trace"
	"sync"
	"time"

	libp2phost "github.com/libp2p/go-libp2p/core/host"
	libp2ppeer "github.com/libp2p/go-libp2p/core/peer"
	libp2pprotocol "github.com/libp2p/go-libp2p/core/protocol"
	"github.com/rollchains/gordian/internal/gchan"
	"github.com/rollchains/gordian/tm/tmcodec"
	"github.com/rollchains/gordian/tm/tmconsensus"
	"github.com/rollchains/gordian/tm/tmengine/tmelink"
)

type resumeFetchRequest struct {
	Start, Stop uint64
}

type pauseFetchRequest struct{}

type nextPeerRequest struct {
	Resp chan<- libp2ppeer.ID
}

type addPeerRequest struct {
	P libp2ppeer.ID
}

type removePeerRequest struct {
	P libp2ppeer.ID
}

type newFetchStateRequest struct {
	Ctx         context.Context
	Start, Stop uint64
}

// HeaderSyncClient manages fetching committed headers from peers
// when the engine indicates that the mirror subsystem is lagging
// behind the rest of the network.
type HeaderSyncClient struct {
	log *slog.Logger

	host        libp2phost.Host
	unmarshaler tmcodec.Unmarshaler

	// Requests that originate externally (should be from the Driver specifically),
	// via calling an exported method on HeaderSyncClient.
	resumeRequests     chan resumeFetchRequest
	pauseRequests      chan pauseFetchRequest
	addPeerRequests    chan addPeerRequest
	removePeerRequests chan removePeerRequest

	// Where we send the committed headers that have fetched.
	replayedHeaders chan<- tmelink.ReplayedHeaderRequest

	// Communication between the main loop and the fetch worker.
	newFetchStateRequests chan newFetchStateRequest
	nextPeerRequests      chan nextPeerRequest

	wg sync.WaitGroup
}

func NewHeaderSyncClient(
	ctx context.Context,
	log *slog.Logger,
	host libp2phost.Host,
	unmarshaler tmcodec.Unmarshaler,
	rhCh chan<- tmelink.ReplayedHeaderRequest,
) *HeaderSyncClient {
	c := &HeaderSyncClient{
		log: log,

		host: host,

		unmarshaler: unmarshaler,

		resumeRequests: make(chan resumeFetchRequest),
		pauseRequests:  make(chan pauseFetchRequest),

		// Arbitrarily sized.
		addPeerRequests:    make(chan addPeerRequest, 8),
		removePeerRequests: make(chan removePeerRequest, 8),

		replayedHeaders: rhCh,

		// 1-buffered so we don't block the main loop on the first send.
		newFetchStateRequests: make(chan newFetchStateRequest, 1),

		nextPeerRequests: make(chan nextPeerRequest),
	}

	c.wg.Add(2)
	go c.mainLoop(ctx)
	go c.fetchWorker(ctx)

	return c
}

func (c *HeaderSyncClient) mainLoop(ctx context.Context) {
	defer c.wg.Done()

	ctx, task := trace.NewTask(ctx, "gp2papi.HeaderSyncClient.mainLoop")
	defer task.End()

	fetchCtx, fetchCancel := context.WithCancelCause(ctx)
	defer func() {
		// Ensure the fetch is cancelled on all return paths.
		// Wrapped inside an anonymous function to avoid closing over initial value.
		fetchCancel(nil)
	}()

	peers := make(map[libp2ppeer.ID]struct{})

	var requestBlockedOnMissingPeer *nextPeerRequest
	for {
		// Normally we want to accept peer requests,
		// but if we are blocked by lack of peers,
		// then we will not accept any new next peer requests.
		nextPeerReqCh := c.nextPeerRequests
		if requestBlockedOnMissingPeer != nil {
			nextPeerReqCh = nil
		}

		select {
		case <-ctx.Done():
			c.log.Info("Stopping due to context cancellation", "cause", context.Cause(ctx))
			return

		case req := <-c.resumeRequests:
			// TODO: this could possibly block, so we should handle it blocking.
			c.newFetchStateRequests <- newFetchStateRequest{
				Ctx:   fetchCtx,
				Start: req.Start,
				Stop:  req.Stop,
			}

		case <-c.pauseRequests:
			fetchCancel(errFetchPause)

			fetchCtx, fetchCancel = context.WithCancelCause(ctx)

		case req := <-nextPeerReqCh:
			if len(peers) == 0 {
				c.log.Warn("Next peer request blocked due to lack of peers")
				requestBlockedOnMissingPeer = &req
				continue
			}

			// Rely on map iteration to send a peer at random.
			for p := range peers {
				// Response must be 1-buffered, having originated from the fetch worker goroutine.
				req.Resp <- p
				break
			}

		case req := <-c.addPeerRequests:
			peers[req.P] = struct{}{}

			if requestBlockedOnMissingPeer != nil {
				requestBlockedOnMissingPeer.Resp <- req.P
				requestBlockedOnMissingPeer = nil
			}

		case req := <-c.removePeerRequests:
			delete(peers, req.P)
		}
	}
}

var errFetchPause = errors.New("fetches paused")

// fetchWorker handles committed header fetching on a dedicated goroutine.
func (c *HeaderSyncClient) fetchWorker(ctx context.Context) {
	defer c.wg.Done()

	ctx, task := trace.NewTask(ctx, "gp2papi.HeaderSyncClient.fetchWorker")
	defer task.End()

	for {
		select {
		case <-ctx.Done():
			c.log.Info("Fetch worker stopping due to context cancellation", "cause", context.Cause(ctx))
			return

		case startReq := <-c.newFetchStateRequests:
			fetchCtx := startReq.Ctx
			c.doFetches(fetchCtx, startReq.Start, startReq.Stop)
		}
	}
}

func (c *HeaderSyncClient) doFetches(ctx context.Context, start, stop uint64) {
	defer trace.StartRegion(ctx, "doFetches").End()

	height := start
	for stop == 0 || (stop > 0 && height < stop) {
		respCh := make(chan libp2ppeer.ID, 1)
		p, ok := gchan.ReqResp(
			ctx, c.log,
			c.nextPeerRequests, nextPeerRequest{Resp: respCh},
			respCh,
			"requesting next peer for fetch",
		)
		if !ok {
			c.log.Info(
				"Committed header fetch interrupted",
				"height", height,
				"cause", context.Cause(ctx),
			)
			return
		}

		res := c.doFetch(ctx, height, p)
		if res.ExcludePeer {
			// TODO: send signal back to main loop that we don't want this peer anymore.
		}
		if !res.Success {
			// Try again with the same height,
			// and hopefully a new peer.
			continue
		}

		// We succeeded, so we can increment the height now.
		height++

		// TODO: should do a non-blocking check on c.newFetchStateRequests here,
		// in case the stop value has been adjusted.
	}
}

// fetchResult is the outcome of a call to [*HeaderSyncClient.doFetch].
type fetchResult struct {
	Success bool

	// Implies that it can be retried.
	ExcludePeer bool
}

var errFetchHeaderDeadlineExceeded = errors.New("deadline for retrieving header exceeded")

// doFetch executes a single committed header fetch,
// at the given height, from the given peer.
func (c *HeaderSyncClient) doFetch(ctx context.Context, height uint64, p libp2ppeer.ID) fetchResult {
	defer trace.StartRegion(ctx, "doFetch").End()

	const timeout = 2 * time.Second // Arbitrarily chosen.
	streamCtx, cancel := context.WithTimeoutCause(
		ctx, timeout, errFetchHeaderDeadlineExceeded,
	)
	defer cancel()

	s, err := c.host.NewStream(streamCtx, p, libp2pprotocol.ID(
		fmt.Sprintf("%s%d", headerV1HeightPrefix, height),
	))
	if err != nil {
		c.log.Info("Failed to open stream to peer", "peer_id", p, "err", err)
		return fetchResult{
			ExcludePeer: true,
		}
	}
	defer s.Close()

	// We have a stream to the right protocol, let's parse the result.
	// Arbitrary limit on header size.
	var res JSONResult
	err = json.NewDecoder(io.LimitReader(s, 4096)).Decode(&res)
	_ = s.Close() // Nothing left to do with stream.
	cancel()      // And free up any streamCtx resources as early as possible.
	if err != nil {
		c.log.Info(
			"Failed to parse stream response from peer",
			"peer_id", p,
			"height", height,
			"err", err,
		)
		return fetchResult{
			ExcludePeer: true,
		}
	}

	if res.Err != "" {

		if res.Err == "height unknown" {
			// Special case of the height isn't ready.
			// Assume we trust this peer.
			// Just back off slightly.
			// Nothing to log in this case.
			t := time.NewTimer(time.Second)
			defer t.Stop()
			select {
			case <-ctx.Done():
			case <-t.C:
			}
		} else {
			c.log.Info(
				"Got error response from peer",
				"peer_id", p,
				"height", height,
				"err", res.Err,
			)
		}

		return fetchResult{
			// They sent a valid error back,
			// so we aren't going to exclude them on these grounds.
		}
	}

	// Parsed the entire result,
	// now parse the committed header.
	var ch tmconsensus.CommittedHeader
	if err := c.unmarshaler.UnmarshalCommittedHeader(res.Result, &ch); err != nil {
		c.log.Info(
			"Failed to parse header from result",
			"peer_id", p,
			"height", height,
			"err", err,
		)
		return fetchResult{
			ExcludePeer: true,
		}
	}

	// Now we have a committed header, so we have to send it to the engine.
	respCh := make(chan tmelink.ReplayedHeaderResponse, 1)
	req := tmelink.ReplayedHeaderRequest{
		Header: ch.Header,
		Proof:  ch.Proof,
		Resp:   respCh,
	}
	resp, ok := gchan.ReqResp(
		ctx, c.log,
		c.replayedHeaders, req,
		respCh,
		"sending replayed header to engine",
	)
	if !ok {
		// Context was cancelled, so result is meaningless here.
		return fetchResult{}
	}

	if resp.Err != nil {
		// TODO: this should special case the expected error types
		// that replayed header requests are documented to return.
		c.log.Info(
			"Got error when applying replayed header",
			"peer_id", p,
			"height", height,
			"err", resp.Err,
		)
		return fetchResult{
			ExcludePeer: true,
		}
	}

	return fetchResult{
		Success: true,
	}
}

func (c *HeaderSyncClient) Wait() {
	c.wg.Wait()
}

// ResumeFetching requests that committed header fetches are restarted,
// or it can indicate that the start and stop height values have changed.
func (c *HeaderSyncClient) ResumeFetching(
	ctx context.Context,
	startHeight, stopHeight uint64,
) (ok bool) {
	return gchan.SendC(
		ctx, c.log,
		c.resumeRequests, resumeFetchRequest{
			Start: startHeight,
			Stop:  stopHeight,
		},
		"making resume fetch request",
	)
}

// PauseFetching requests that outstanding committed header fetches are interrupted.
func (c *HeaderSyncClient) PauseFetching(ctx context.Context) (ok bool) {
	return gchan.SendC(
		ctx, c.log,
		c.pauseRequests, pauseFetchRequest{},
		"making pause fetch request",
	)
}

// AddPeer requests to add the given peer ID as a candidate peer
// for fetching committed headers.
func (c *HeaderSyncClient) AddPeer(ctx context.Context, p libp2ppeer.ID) (ok bool) {
	return gchan.SendC(
		ctx, c.log,
		c.addPeerRequests, addPeerRequest{P: p},
		"making add peer request",
	)
}

// RemovePeer requests to remove the given peer ID as a candidate peer
// for fetching committed headers.
func (c *HeaderSyncClient) RemovePeer(ctx context.Context, p libp2ppeer.ID) (ok bool) {
	return gchan.SendC(
		ctx, c.log,
		c.removePeerRequests, removePeerRequest{P: p},
		"making remove peer request",
	)
}
