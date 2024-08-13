package gdatapool

import (
	"context"
	"errors"
	"log/slog"
	"runtime/trace"
	"sync"

	"github.com/rollchains/gordian/internal/gchan"
)

// Pool manages the retrieval of proposed block data
// from potentially many remote hosts.
//
// Methods on Pool are not safe for concurrent use, unless explicitly described as such.
// If used as designed from within a [tmconsensus.ConsensusStrategy],
// only one goroutine will ever access a Pool method at one time,
// avoiding any potential data races.
type Pool[T any] struct {
	log *slog.Logger

	// Keyed by data ID.
	curRequests map[string]retrieveRequest[T]

	curH uint64
	curR uint32

	retrieveRequests   chan retrieveRequest[T]
	enterRoundRequests chan enterRoundRequest

	workerWG sync.WaitGroup

	done chan struct{}
}

type enterRoundRequest struct {
	H uint64
	R uint32

	Handled chan struct{}
}

func New[T any](
	ctx context.Context,
	log *slog.Logger,
	nWorkers int,
	retriever Retriever[T],
) *Pool[T] {
	p := &Pool[T]{
		log: log,

		curRequests: make(map[string]retrieveRequest[T]),

		retrieveRequests:   make(chan retrieveRequest[T], nWorkers),
		enterRoundRequests: make(chan enterRoundRequest), // Unbuffered.

		done: make(chan struct{}),
	}

	go p.kernel(ctx, nWorkers, retriever)

	return p
}

func (p *Pool[T]) kernel(
	ctx context.Context,
	nWorkers int,
	retriever Retriever[T],
) {
	defer close(p.done)

	ctx, task := trace.NewTask(ctx, "gdatapool.Pool.kernel")
	defer task.End()

	// Special handling on first enter round request,
	// so that we don't start any workers before we need them.
	// This is probably more meaningful in test than production.
	var rounds *roundList
	var roundCancel context.CancelCauseFunc
	select {
	case <-ctx.Done():
		p.log.Info(
			"Data pool kernel stopping before first call to EnterRound",
			"cause", context.Cause(ctx),
		)
		return

	case req := <-p.enterRoundRequests:
		// We need a populated round list to seed the workers.
		rounds = &roundList{Height: req.H, Round: req.R}
		rounds.Ctx, roundCancel = context.WithCancelCause(ctx)

		p.workerWG.Add(nWorkers)
		for i := range nWorkers {
			w := &worker[T]{
				log:       p.log.With("w_id", i),
				roundList: rounds,
				retriever: retriever,
			}
			go w.Run(&p.workerWG, p.retrieveRequests)
		}
		close(req.Handled)
	}

	// Now we handle enter round requests until the root context is cancelled.
	for {
		select {
		case <-ctx.Done():
			p.log.Info("Data pool kernel stopping", "cause", context.Cause(ctx))
			return

		case req := <-p.enterRoundRequests:
			// Any previous requests are eligible for GC now.
			//
			// If any workers are about to write to a request result,
			// that is fine; the request will not be referenced from anywhere else.
			//
			// This is a hypothetical data race with any calls to Need or SetAvailable,
			// but that will not occur if EnterRound requests are only arriving through the Consensus Strategy.
			clear(p.curRequests)

			nextRound := &roundList{
				Height: req.H,
				Round:  req.R,
			}

			// Seed the next round before canceling the current round,
			// as cancellation causes a round advancement.
			var nextRoundCancel context.CancelCauseFunc
			nextRound.Ctx, nextRoundCancel = context.WithCancelCause(ctx)
			rounds.Next = nextRound

			roundCancel(errRoundOver)
			roundCancel = nextRoundCancel

			rounds = nextRound

			close(req.Handled)
		}
	}
}

// Wait blocks until the background work in p has finished.
func (p *Pool[T]) Wait() {
	// Wait for the pool's kernel goroutine to finish first.
	// On the unlikely chance that Wait is called before the pool kernel fully starts,
	// this should avoid a data race in Add and Wait being called concurrently.
	<-p.done
	p.workerWG.Wait()
}

// Need marks a data ID as required.
// The formats of both the dataID and encodedAddrs are implementation-specific.
// Subsequent calls with the same dataID are ignored,
// until a new call to EnterRound.
func (p *Pool[T]) Need(height uint64, round uint32, dataID string, metadata []byte) {
	if _, have := p.curRequests[dataID]; have {
		p.log.Debug("Ignoring repeated request for same data ID", "data_id", dataID)
		return
	}

	req := retrieveRequest[T]{
		Height: height,
		Round:  round,

		DataID:   dataID,
		Metadata: metadata,

		Ready:  make(chan struct{}),
		Result: new(retrieveResult[T]),
	}

	p.curRequests[dataID] = req

	p.retrieveRequests <- req
}

func (p *Pool[T]) Have(dataID string) (value T, err error, have bool) {
	req, ok := p.curRequests[dataID]
	if !ok {
		panic(errors.New("BUG: Have called before a call to Need or SetAvailable -- " + dataID))
	}

	select {
	case <-req.Ready:
		return req.Result.Value, req.Result.Error, true
	default:
		// All zero values, including have=false.
		return
	}
}

// SetAvailable adds the value to the local pool,
// with the given dataID and metadata.
//
// This should only be called from a consensus strategy that proposes a block.
func (p *Pool[T]) SetAvailable(dataID string, metadata []byte, value T) {
	if _, have := p.curRequests[dataID]; have {
		panic(errors.New(
			"BUG: SetAvailable called on data ID used in previous call to Need or SetAvailable",
		))
	}

	// Construct a pre-finished request and add it to the current requests.
	req := retrieveRequest[T]{
		DataID:   dataID,
		Metadata: metadata,

		Ready: make(chan struct{}),
		Result: &retrieveResult[T]{
			Value: value,
		},
	}
	close(req.Ready)
	p.curRequests[dataID] = req

	// Not sending this request to workers.
}

func (p *Pool[T]) EnterRound(ctx context.Context, height uint64, round uint32) {
	req := enterRoundRequest{H: height, R: round, Handled: make(chan struct{})}
	_, _ = gchan.ReqResp(
		ctx, p.log,
		p.enterRoundRequests, req,
		req.Handled,
		"sending enter round request to data pool kernel",
	)
}

type retrieveRequest[T any] struct {
	Height uint64
	Round  uint32

	DataID   string
	Metadata []byte

	Ready  chan struct{}
	Result *retrieveResult[T]
}

type retrieveResult[T any] struct {
	Value T
	Error error
}

// Retriever handles retrieving block data given a data ID and metadata.
// The [Pool] calls into the retriever on background worker goroutines.
// The Retrieve method must be safe for concurrent use.
type Retriever[T any] interface {
	Retrieve(ctx context.Context, dataID string, metadata []byte) (T, error)
}
