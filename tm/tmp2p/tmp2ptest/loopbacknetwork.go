package tmp2ptest

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/rollchains/gordian/internal/glog"
	"github.com/rollchains/gordian/tm/tmconsensus"
	"github.com/rollchains/gordian/tm/tmp2p"
)

// LoopbackNetwork is a network used for testing,
// where messages never leave the current process.
type LoopbackNetwork struct {
	// The network's logger isn't actively used,
	// as that would typically be high noise.
	// But it's available and wired up if an individual test needs network debugging.
	// Just drop in your own log calls.
	log *slog.Logger

	newConnRequests chan newConnRequest
	rmConn          chan *LoopbackConnection

	incomingProposals chan loopbackProposal

	incomingPrevoteProofs   chan loopbackPrevoteProof
	incomingPrecommitProofs chan loopbackPrecommitProof

	done chan struct{}
}

type newConnRequest struct {
	conn     *LoopbackConnection
	accepted chan struct{}
}

// NewLoopbackNetwork returns an initialized LoopbackNetwork.
// Call Stop() to clean up resources.
func NewLoopbackNetwork(ctx context.Context, log *slog.Logger) *LoopbackNetwork {
	n := &LoopbackNetwork{
		log: log.With("net_idx", atomic.AddUint64(&loopbackNetworkIdxCounter, 1)),

		newConnRequests: make(chan newConnRequest, 1),
		rmConn:          make(chan *LoopbackConnection, 1),

		incomingProposals: make(chan loopbackProposal, 1),

		incomingPrevoteProofs:   make(chan loopbackPrevoteProof, 1),
		incomingPrecommitProofs: make(chan loopbackPrecommitProof, 1),

		done: make(chan struct{}),
	}
	go n.background(ctx)
	return n
}

// Wait blocks until the network is fully stopped.
// To stop the network, cancel the context used in [NewLoopbackNetwork].
func (n *LoopbackNetwork) Wait() {
	<-n.done
}

func (n *LoopbackNetwork) background(ctx context.Context) {
	defer close(n.done)

	var conns []*LoopbackConnection

	for {
		select {
		case <-ctx.Done():
			n.log.Debug("Network closing")
			// Close all outstanding connections.
			for _, c := range conns {
				c.disconnect()
			}
			for _, c := range conns {
				<-c.Disconnected()
			}
			return
		case req := <-n.newConnRequests:
			// Add a new connection.
			conns = append(conns, req.conn)
			n.log.Debug("Network added connection", "conn_idx", req.conn.idx)
			close(req.accepted)
		case c := <-n.rmConn:
			// Remove an existing connection if found.
			idx := slices.Index(conns, c)
			if idx >= 0 {
				conns = slices.Delete(conns, idx, idx+1)
			}
		case p := <-n.incomingProposals:
			n.log.Debug("Received incoming proposal", "seq", p.seq)
			// Send the proposal to everyone on the network.
			n.dispatchProposal(ctx, p, conns)

		case p := <-n.incomingPrevoteProofs:
			n.log.Debug("Received incoming prevote proof", "seq", p.seq)
			n.dispatchPrevoteProof(ctx, p, conns)

		case c := <-n.incomingPrecommitProofs:
			n.log.Debug("Received incoming precommit proof", "seq", c.seq)
			// Send the precommit to everyone on the network.
			n.dispatchPrecommitProof(ctx, c, conns)
		}
	}
}

var (
	// Atomic counter to distinguish networks.
	loopbackNetworkIdxCounter uint64

	// Atomic counter for connection indices,
	// so that when logging, different connections can be distinguished.
	loopbackConnIdxCounter uint64

	// Atomic counter for packet sequences.
	// Probably only useful for debugging.
	loopbackSequenceCounter uint64
)

func nextLoopbackSeq() uint64 {
	return atomic.AddUint64(&loopbackSequenceCounter, 1)
}

// Connect returns a new connection to the network.
func (n *LoopbackNetwork) Connect(ctx context.Context) (*LoopbackConnection, error) {
	idx := atomic.AddUint64(&loopbackConnIdxCounter, 1)

	conn := newLoopbackConnection(n.log.With("conn_idx", idx), n, idx)
	req := newConnRequest{
		conn:     conn,
		accepted: make(chan struct{}),
	}
	select {
	case n.newConnRequests <- req:
		// Okay.
	case <-ctx.Done():
		return nil, fmt.Errorf("context finished while creating connection to network: %w", context.Cause(ctx))
	}

	select {
	case <-req.accepted:
		// Okay.
	case <-ctx.Done():
		return nil, fmt.Errorf("context finished while awaiting network connection acknowledgement: %w", context.Cause(ctx))
	}

	return conn, nil
}

// dispatchProposal sends the proposal to every connection on the network except the sender.
func (n *LoopbackNetwork) dispatchProposal(ctx context.Context, p loopbackProposal, conns []*LoopbackConnection) {
	for _, c := range conns {
		if p.sender == c {
			// Don't send back to self.
			continue
		}

		select {
		case c.incomingProposals <- p.p:
			n.log.Debug("Dispatched proposed block", "conn_idx", c.idx, "seq", p.seq, "block_hash", glog.Hex(p.p.Block.Hash))
			// Okay.
		case <-ctx.Done():
			// Respect early quit.
			return
		}
	}
}

// dispatchPrevoteProof sends the prevote proof to every connection on the network except the sender.
func (n *LoopbackNetwork) dispatchPrevoteProof(ctx context.Context, p loopbackPrevoteProof, conns []*LoopbackConnection) {
	for _, c := range conns {
		if p.sender == c {
			// Don't send back to self.
			continue
		}

		select {
		case c.incomingPrevoteProofs <- p.p:
			n.log.Debug("Dispatched prevote proof", "conn_idx", c.idx, "seq", p.seq)
			// Okay.
		case <-ctx.Done():
			// Respect early quit.
			return
		}
	}
}

// dispatchPrecommitProof sends the precommit to every connection on the network except the sender.
func (n *LoopbackNetwork) dispatchPrecommitProof(ctx context.Context, p loopbackPrecommitProof, conns []*LoopbackConnection) {
	for _, conn := range conns {
		if p.sender == conn {
			// Don't send back to self.
			continue
		}

		select {
		case conn.incomingPrecommitProofs <- p.proof:
			n.log.Debug("Dispatched precommit proof", "conn_idx", conn.idx, "seq", p.seq)
			// Okay.
		case <-ctx.Done():
			// Respect early quit.
			return
		}
	}
}

// The loopback network does not require any stabilization steps,
// so Stabilize is a no-op.
func (n *LoopbackNetwork) Stabilize(context.Context) error {
	return nil
}

// LoopbackConnection is a connection to a LoopbackNetwork.
type LoopbackConnection struct {
	log *slog.Logger

	net *LoopbackNetwork
	idx uint64

	incomingProposals, outgoingProposals chan tmconsensus.ProposedBlock

	incomingPrevoteProofs, outgoingPrevoteProofs     chan tmconsensus.PrevoteSparseProof
	incomingPrecommitProofs, outgoingPrecommitProofs chan tmconsensus.PrecommitSparseProof

	setConsensusHandlerRequests chan setConsensusHandlerRequest

	handleFuncs chan func()

	disconnectOnce     sync.Once
	quit, disconnected chan struct{}
	wg                 sync.WaitGroup
}

func newLoopbackConnection(log *slog.Logger, n *LoopbackNetwork, idx uint64) *LoopbackConnection {
	c := &LoopbackConnection{
		log: log,

		net: n,
		idx: idx,

		incomingProposals: make(chan tmconsensus.ProposedBlock, 1),
		outgoingProposals: make(chan tmconsensus.ProposedBlock, 1),

		incomingPrevoteProofs: make(chan tmconsensus.PrevoteSparseProof, 1),
		outgoingPrevoteProofs: make(chan tmconsensus.PrevoteSparseProof, 1),

		incomingPrecommitProofs: make(chan tmconsensus.PrecommitSparseProof, 1),
		outgoingPrecommitProofs: make(chan tmconsensus.PrecommitSparseProof, 1),

		setConsensusHandlerRequests: make(chan setConsensusHandlerRequest, 1),

		handleFuncs: make(chan func(), 4), // Slightly more buffered.

		quit:         make(chan struct{}),
		disconnected: make(chan struct{}),
	}
	c.wg.Add(1)
	go c.background()

	for i := 0; i < cap(c.handleFuncs); i++ {
		c.wg.Add(1)
		go c.handleAsync()
	}

	return c
}

func (c *LoopbackConnection) background() {
	defer c.wg.Done()

	var h2 tmconsensus.ConsensusHandler

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		<-c.quit
		cancel()
	}()

	for {
		select {
		case <-c.quit:
			c.log.Debug("Connection closing")
			close(c.handleFuncs)
			return
		case p := <-c.outgoingProposals:
			proposal := loopbackProposal{
				sender: c,
				p:      p,
				seq:    nextLoopbackSeq(),
			}
			c.log.Debug(
				"Sending proposal out to network",
				"height", p.Block.Height,
				"round", p.Round,
				"seq", proposal.seq,
			)
			select {
			case c.net.incomingProposals <- proposal:
			case <-c.quit:
			}
		case p := <-c.incomingProposals:
			if h2 != nil {
				select {
				case c.handleFuncs <- func() {
					// TODO: set appropriate context, probably respect feedback value
					_ = h2.HandleProposedBlock(ctx, p)
				}:
				case <-c.quit:
				}
			}

		case p := <-c.incomingPrevoteProofs:
			c.log.Debug("Received incoming prevote proof")
			if h2 != nil {
				select {
				case c.handleFuncs <- func() {
					_ = h2.HandlePrevoteProofs(ctx, p)
				}:
				case <-c.quit:
				}
			}
		case p := <-c.outgoingPrevoteProofs:
			prevoteProof := loopbackPrevoteProof{
				sender: c,
				p:      p,
				seq:    nextLoopbackSeq(),
			}
			c.log.Debug(
				"Sending prevote proof out to network",
				"height", p.Height,
				"round", p.Round,
				"seq", prevoteProof.seq,
			)
			select {
			case c.net.incomingPrevoteProofs <- prevoteProof:
			case <-c.quit:
			}

		case p := <-c.incomingPrecommitProofs:
			if h2 != nil {
				select {
				case c.handleFuncs <- func() {
					// TODO: set appropriate context, probably respect feedback value
					_ = h2.HandlePrecommitProofs(ctx, p)
				}:
				case <-c.quit:
				}
			}
		case p := <-c.outgoingPrecommitProofs:
			precommitProof := loopbackPrecommitProof{
				sender: c,
				proof:  p,
				seq:    nextLoopbackSeq(),
			}
			c.log.Debug(
				"Sending precommit proof out to network",
				"height", p.Height,
				"round", p.Round,
				"seq", precommitProof.seq,
			)
			select {
			case c.net.incomingPrecommitProofs <- precommitProof:
			case <-c.quit:
			}

		case req := <-c.setConsensusHandlerRequests:
			h2 = req.Handler
			close(req.Ready)
		}
	}
}

func (c *LoopbackConnection) handleAsync() {
	defer c.wg.Done()

	for fn := range c.handleFuncs {
		fn()
	}
}

// Disconnect removes this connection from the network,
// and closes all of the connection's channels.
func (c *LoopbackConnection) Disconnect() {
	c.net.rmConn <- c
	c.disconnect()
}

func (c *LoopbackConnection) disconnect() {
	c.disconnectOnce.Do(func() {
		close(c.quit)
		c.wg.Wait()
		close(c.disconnected)
	})
}

// Disconnected returns a channel that is closed once Disconnect completes.
func (c *LoopbackConnection) Disconnected() <-chan struct{} {
	return c.disconnected
}

// ConsensusBroadcaster returns c, which already satisfies the ConsensusBroadcaster interface.
func (c *LoopbackConnection) ConsensusBroadcaster() tmp2p.ConsensusBroadcaster {
	return c
}

// OutgoingProposals is a channel that accepts proposed blocks
// to be broadcast to the rest of the network.
func (c *LoopbackConnection) OutgoingProposals() chan<- tmconsensus.ProposedBlock {
	return c.outgoingProposals
}

// OutgoingPrevoteProofs is a channel that accepts prevote proofs
// to be broadcast to the rest of the network.
func (c *LoopbackConnection) OutgoingPrevoteProofs() chan<- tmconsensus.PrevoteSparseProof {
	return c.outgoingPrevoteProofs
}

// OutgoingPrecommitProofs is a channel that accepts precommit proofs
// to be broadcast to the rest of the network.
func (c *LoopbackConnection) OutgoingPrecommitProofs() chan<- tmconsensus.PrecommitSparseProof {
	return c.outgoingPrecommitProofs
}

func (c *LoopbackConnection) OutgoingProposedBlocks() chan<- tmconsensus.ProposedBlock {
	return c.OutgoingProposals()
}

func (c *LoopbackConnection) SetConsensusHandler(h tmconsensus.ConsensusHandler) {
	ready := make(chan struct{})
	req := setConsensusHandlerRequest{
		Handler: h,
		Ready:   ready,
	}

	c.setConsensusHandlerRequests <- req
	<-ready
}

type loopbackProposal struct {
	sender *LoopbackConnection
	p      tmconsensus.ProposedBlock
	seq    uint64
}

type loopbackPrevoteProof struct {
	sender *LoopbackConnection
	p      tmconsensus.PrevoteSparseProof
	seq    uint64
}

type loopbackPrecommitProof struct {
	sender *LoopbackConnection
	proof  tmconsensus.PrecommitSparseProof
	seq    uint64
}

type setConsensusHandlerRequest struct {
	Handler tmconsensus.ConsensusHandler
	Ready   chan struct{}
}
