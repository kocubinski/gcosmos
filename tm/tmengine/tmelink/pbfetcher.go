package tmelink

import (
	"context"

	"github.com/rollchains/gordian/tm/tmconsensus"
)

// ProposedBlockFetcher contains the input and output channels to fetch proposed blocks.
// The engine uses this when there are sufficient votes for a proposed block,
// and the engine does not have the proposed block.
type ProposedBlockFetcher struct {
	// ProposedBlockFetchRequests is the channel for the engine
	// to send requests to fetch a proposed block
	// at a given height and with a given hash.
	//
	// Once a proposed block matching the height and hash has been found,
	// that block is sent to the FetchedProposedBlocks channel.
	//
	// The ProposedBlockFetchRequest struct has a context field.
	// The context is associated with the fetch for this particular block.
	// Canceling the context will abort any in-progress requests to find the block.
	// If the context is cancelled after the proposed block has been enqueued into FetchedProposedBlocks,
	// there is no effect.
	//
	// A ProposedBlockFetcher should have an upper limit on the number of outstanding fetch requests.
	// If the number of in-flight requests is at its limit,
	// the send to this channel will block.
	FetchRequests chan<- ProposedBlockFetchRequest

	// FetchedProposedBlocks is the single channel that sends any block
	// discovered as a result of a call to FetchProposedBlock.
	FetchedProposedBlocks <-chan tmconsensus.ProposedBlock
}

// ProposedBlockFetchRequest is used to make requests to fetch missed proposed blocks.
type ProposedBlockFetchRequest struct {
	// Context associated with the request.
	// Canceling this context will abort the request, if it is still in-flight.
	Ctx context.Context

	// The height to search for the proposed block.
	Height uint64

	// The hash of the block to search for.
	BlockHash string
}
