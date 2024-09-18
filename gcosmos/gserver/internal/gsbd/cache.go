package gsbd

import (
	"fmt"
	"sync"

	"cosmossdk.io/core/transaction"
)

// RequestCache is an in-memory cache for in-flight block data requests.
//
// The RequestCache only operates in driver-space.
// A [BlockDataRequest] entry is added to the RequestCache in the following circumstances:
//
//  1. The consensus strategy is proposing a block,
//     and it is immediately marking the data available.
//  2. The consensus strategy is deciding whether to vote on a proposed block,
//     and it is initiating a request to the proposer's specified addresses
//     in the proposal annotations, in order to evaluate
//     whether the proposed block can be applied.
//  3. The mirror is performing catchup,
//     and the block data may or may not be immediately available.
//
// The first downside of the RequestCache is that entries must be manually purged.
// If multiple proposed blocks are received, the driver is designed to only purge
// the block data for the block it finalizes.
// This means the consensus strategy should purge the other blocks;
// otherwise the other block data will never be garbage collected.
// (Perhaps this implies this cache is a good candidate for a weak map in Go.)
//
// There are some possible race conditions if a block data request
// may be created from multiple originators, or if the same block data is present in different requests.
// The latter case could be addressed by including a height and round in the map key.
type RequestCache struct {
	// We generally avoid mutexes in production Gordian code,
	// but this is an exception because there is little practical state in the map access.
	// The mutex is only held while adding or removing elements from the map;
	// the mutex is held for a predictably short time.
	mu sync.Mutex
	rs map[string]BlockDataRequest
}

// BlockDataRequest is the information associated with a request for block data,
// and a channel for synchronization upon the block data being ready.
type BlockDataRequest struct {
	// The Ready channel is closed once the block data is available to read.
	// This lets the consumer synchronize on its availability.
	Ready <-chan struct{}

	// The deserialized transactions.
	Transactions []transaction.Tx

	// The raw data from which the Transactions were derived.
	// This value is used to write into [gcstore.BlockDataStore],
	// so identical data may be hosted to other peers.
	EncodedTransactions []byte
}

func NewRequestCache() *RequestCache {
	return &RequestCache{
		rs: make(map[string]BlockDataRequest),
	}
}

// SetInFlight adds the given BlockDataRequest to the cache,
// under the key specified by dataID.
// If there is already an entry for this dataID,
// SetInFlight panics.
func (c *RequestCache) SetInFlight(dataID string, req BlockDataRequest) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, ok := c.rs[dataID]; ok {
		panic(fmt.Errorf("BUG: attempted to add data ID %q which was already in the cache", dataID))
	}

	c.rs[dataID] = req
}

// SetImmediatelyAvailable marks the given dataID as ready to read,
// with the given txs and encodedTxs.
// This is equivalent to calling [*RequestCache.SetInFlight] with a BlockDataRequest
// with those fields and whose Ready channel is already closed.
func (c *RequestCache) SetImmediatelyAvailable(
	dataID string, txs []transaction.Tx, encodedTxs []byte,
) {
	ch := make(chan struct{})
	close(ch)
	req := BlockDataRequest{
		Ready:               ch,
		Transactions:        txs,
		EncodedTransactions: encodedTxs,
	}

	c.SetInFlight(dataID, req)
}

// Purge removes the entry keyed by dataID from the cache.
// If no such entry exists, Purge panics.
func (c *RequestCache) Purge(dataID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, ok := c.rs[dataID]; !ok {
		panic(fmt.Errorf("BUG: attempted to purge nonexistent data ID %q", dataID))
	}

	delete(c.rs, dataID)
}

// Get returns the BlockDataRequest corresponding to dataID.
// The ok return value follows the idiomatic "comma, ok" pattern in Go.
func (c *RequestCache) Get(dataID string) (r BlockDataRequest, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	r, ok = c.rs[dataID]
	return r, ok
}
