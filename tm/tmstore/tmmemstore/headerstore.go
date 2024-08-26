package tmmemstore

import (
	"context"
	"sync"

	"github.com/rollchains/gordian/tm/tmconsensus"
)

type HeaderStore struct {
	mu sync.RWMutex

	chs map[uint64]tmconsensus.CommittedHeader
}

func NewHeaderStore() *HeaderStore {
	return &HeaderStore{
		chs: make(map[uint64]tmconsensus.CommittedHeader),
	}
}

func (s *HeaderStore) SaveHeader(_ context.Context, ch tmconsensus.CommittedHeader) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.chs[ch.Header.Height] = ch

	return nil
}

func (s *HeaderStore) LoadHeader(_ context.Context, height uint64) (tmconsensus.CommittedHeader, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ch, ok := s.chs[height]
	if !ok {
		return tmconsensus.CommittedHeader{}, tmconsensus.HeightUnknownError{Want: height}
	}

	return ch, nil
}
