package tmelinktest

import (
	"github.com/rollchains/gordian/tm/tmconsensus"
	"github.com/rollchains/gordian/tm/tmengine/tmelink"
)

// PBFetcher can be used in tests where a [tmelink.ProposedBlockFetcher] is needed.
type PBFetcher struct {
	// Bidirectional request channel,
	// so tests can inspect requests.
	ReqCh chan tmelink.ProposedBlockFetchRequest

	// Bidirectional output channel,
	// so tests can send responses.
	FetchedCh chan tmconsensus.ProposedBlock
}

func NewPBFetcher(reqSz, fetchedSz int) PBFetcher {
	return PBFetcher{
		ReqCh:     make(chan tmelink.ProposedBlockFetchRequest, reqSz),
		FetchedCh: make(chan tmconsensus.ProposedBlock, fetchedSz),
	}
}

// ProposedBlockFetcher returns a ProposedBlockFetcher struct,
// as the engine and its internal components expect.
func (f PBFetcher) ProposedBlockFetcher() tmelink.ProposedBlockFetcher {
	return tmelink.ProposedBlockFetcher{
		FetchRequests:         f.ReqCh,
		FetchedProposedBlocks: f.FetchedCh,
	}
}
