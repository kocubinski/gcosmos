package datapool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"runtime/trace"
	"sync"

	"cosmossdk.io/core/transaction"
	libp2ppeer "github.com/libp2p/go-libp2p/core/peer"
	"github.com/rollchains/gordian/gcosmos/gserver/internal/gsbd"
	"github.com/rollchains/gordian/internal/gchan"
)

type Pool struct {
	log *slog.Logger

	// TODO: this should be an interface, not the concrete type.
	client *gsbd.Libp2pClient

	enterRoundRequests chan enterRoundRequest

	workerWG sync.WaitGroup

	done chan struct{}
}

type RetrieveBlockDataRequest struct {
	Height uint64
	Round  uint32

	DataID    string
	DataSize  int
	Locations []gsbd.Location

	// When the worker has retrieved the data successfully,
	// it writes to *Txs and closes the Ready channel,
	// signaling that a read of Txs is now safe.
	Ready chan<- struct{}
	Txs   *[]transaction.Tx
}

type enterRoundRequest struct {
	h uint64
	r uint32
}

// New initializes and returns a new Pool.
//
// The ctx argument controls the lifecycle of the pool.
func New(
	ctx context.Context,
	log *slog.Logger,
	nWorkers int,
	retrieveRequests <-chan RetrieveBlockDataRequest,
	client *gsbd.Libp2pClient,
) *Pool {
	p := &Pool{
		log: log,

		enterRoundRequests: make(chan enterRoundRequest), // Unbuffered.

		done: make(chan struct{}),
	}

	go p.kernel(ctx, nWorkers, retrieveRequests, client)

	return p
}

func (p *Pool) kernel(
	ctx context.Context,
	nWorkers int,
	retrieveRequests <-chan RetrieveBlockDataRequest,
	client *gsbd.Libp2pClient,
) {
	defer close(p.done)

	ctx, task := trace.NewTask(ctx, "datapool.kernel")
	defer task.End()

	// Special handling for the first enter round request.
	var rounds *roundList
	var roundCancel context.CancelCauseFunc
	select {
	case <-ctx.Done():
		p.log.Info(
			"Data pool kernel stopping before the first call to EnterRound",
			"cause", context.Cause(ctx),
		)
		return

	case req := <-p.enterRoundRequests:

		// Now initialize the round list,
		// which we need to seed the workers.
		rounds = &roundList{
			Height: req.h,
			Round:  req.r,
		}

		rounds.Ctx, roundCancel = context.WithCancelCause(ctx)

		p.workerWG.Add(nWorkers)
		for i := range nWorkers {
			w := &worker{
				log:       p.log.With("w_id", i),
				client:    client,
				roundList: rounds,
			}
			go w.Run(&p.workerWG, retrieveRequests)
		}
	}

	// Now we can wait for further enter round requests.
	for {
		select {
		case <-ctx.Done():
			p.log.Info("Data pool kernel stopping", "cause", context.Cause(ctx))
			return

		case req := <-p.enterRoundRequests:
			nextRound := &roundList{
				Height: req.h,
				Round:  req.r,
			}

			var nextRoundCancel context.CancelCauseFunc
			nextRound.Ctx, nextRoundCancel = context.WithCancelCause(ctx)
			rounds.Next = nextRound

			roundCancel(errRoundOver)
			roundCancel = nextRoundCancel

			rounds = nextRound
		}
	}
}

func (p *Pool) Wait() {
	// Wait for the pool's kernel goroutine to finish first.
	// On the unlikely chance that Wait is called before the pool fully initializes,
	// this should avoid a data race in Add and Wait being called concurrently.
	<-p.done
	p.workerWG.Wait()
}

// Sentinel error to signal the end of a round,
// to be distinguished from a normal context cancellation.
var errRoundOver = errors.New("round over")

type roundList struct {
	Height uint64
	Round  uint32

	Ctx context.Context

	Next *roundList
}

func (p *Pool) EnterRound(ctx context.Context, h uint64, r uint32) {
	_ = gchan.SendC(
		ctx, p.log,
		p.enterRoundRequests, enterRoundRequest{h: h, r: r},
		"sending enter round request to data pool kernel",
	)
}

type worker struct {
	log *slog.Logger

	client *gsbd.Libp2pClient

	roundList *roundList
}

func (w *worker) Run(
	wg *sync.WaitGroup,
	retrieveRequests <-chan RetrieveBlockDataRequest,
) {
	defer wg.Done()

	for {
		select {
		case <-w.roundList.Ctx.Done():
			if cause := context.Cause(w.roundList.Ctx); cause != errRoundOver {
				w.log.Info("Stopping due to context cancellation", "cause", cause)
				return
			}

			// If it was round over, we can definitely advance the round list once.

			w.roundList = w.roundList.Next

			// Now see how many more times we can skip ahead.
			// It's unlikely that we were more than one behind,
			// but we should advance anyway.
			if _, quit := w.advanceRoundList(); quit {
				return
			}

		case req := <-retrieveRequests:
			// Incoming retrieve request.

			// If this looks old, discard it.
			if req.Height < w.roundList.Height || (req.Height == w.roundList.Height && req.Round < w.roundList.Round) {
				continue
			}

			// If it looks like it's in the future, advance the round list.
			if req.Height > w.roundList.Height || (req.Height == w.roundList.Height && req.Round > w.roundList.Round) {
				didAdvance, quit := w.advanceRoundList()
				if quit {
					return
				}

				// If we did advance, check if we advanced too far.
				if didAdvance {
					if req.Height < w.roundList.Height || (req.Height == w.roundList.Height && req.Round < w.roundList.Round) {
						continue
					}
				}

				// And if we didn't advance, then the request is still in the future, so we have to drop it.
				if !didAdvance {
					w.log.Warn(
						"Received request to retrieve block data for future height/round; dropping the request",
						"recv_h", req.Height, "recv_r", req.Round,
						"h", w.roundList.Height, "r", w.roundList.Round,
					)
					continue
				}
			}

			// If we got this far, then whether we advanced or not,
			// we should be on the matching height and round.
			// Temporary panic to assert that is the case.
			if req.Height != w.roundList.Height || req.Round != w.roundList.Round {
				panic(fmt.Errorf(
					"BUG: would have handled retrieval request for %d/%d when actually on %d/%d",
					req.Height, req.Round, w.roundList.Height, w.roundList.Round,
				))
			}

			// Now we want to do the retrieval in the foreground.
			// If the context is cancelled while
			w.retrieveData(req)
		}
	}
}

func (w *worker) advanceRoundList() (didAdvance, quit bool) {
	for {
		select {
		case <-w.roundList.Ctx.Done():
			if cause := context.Cause(w.roundList.Ctx); cause != errRoundOver {
				w.log.Info("Stopping due to context cancellation", "cause", cause)
				return false, true // Doesn't matter if we advanced.
			}

			w.roundList = w.roundList.Next
			didAdvance = true
			continue

		default:
			// We've reached the current round.
			// Report whether we advanced.
			return didAdvance, false
		}
	}
}

func (w *worker) retrieveData(req RetrieveBlockDataRequest) {
	ctx := w.roundList.Ctx

	for _, loc := range req.Locations {
		if loc.Scheme != gsbd.Libp2pScheme {
			// Silently skip anything that isn't libp2p.
			continue
		}

		// If it is a libp2p scheme,
		// then the Addr field should be a JSON-encoded libp2p addr info.
		var ai libp2ppeer.AddrInfo
		if err := json.Unmarshal([]byte(loc.Addr), &ai); err != nil {
			w.log.Debug(
				"Skipping retrieval location due to failure to unmarshal AddrInfo",
				"err", err,
			)
			continue
		}

		txs, err := w.client.Retrieve(ctx, ai, req.DataID, req.DataSize)
		if err != nil {
			// On error to retrieve the data, there is nothing we can really do.
			w.log.Warn("Error while attempting to retrieve data", "err", err)
			continue
		}

		// We have successfully got the transactions,
		// and they have been validated according to the data ID.
		// Now we can write it back to the request,
		// signaling that writes are done and reads may proceed.
		*req.Txs = txs
		close(req.Ready)
		return
	}

	// If we got past the locations loop,
	// we failed to retrieve any data.
	w.log.Warn(
		"Failed to retrieve any data",
		"id", req.DataID,
	)
}
