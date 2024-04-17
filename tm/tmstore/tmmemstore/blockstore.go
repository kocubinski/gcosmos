package tmmemstore

import (
	"context"
	"sync"

	"github.com/rollchains/gordian/tm/tmconsensus"
)

type BlockStore struct {
	mu sync.RWMutex

	cbs map[uint64]tmconsensus.CommittedBlock
}

func NewBlockStore() *BlockStore {
	return &BlockStore{
		cbs: make(map[uint64]tmconsensus.CommittedBlock),
	}
}

func (s *BlockStore) SaveBlock(_ context.Context, cb tmconsensus.CommittedBlock) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.cbs[cb.Block.Height] = cb

	return nil
}

func (s *BlockStore) LoadBlock(_ context.Context, height uint64) (tmconsensus.CommittedBlock, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cb, ok := s.cbs[height]
	if !ok {
		return tmconsensus.CommittedBlock{}, tmconsensus.HeightUnknownError{Want: height}
	}

	return cb, nil
}
