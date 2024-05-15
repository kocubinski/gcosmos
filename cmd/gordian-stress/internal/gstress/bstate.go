package gstress

import "sync"

type bState struct {
	mu sync.Mutex

	chainID string
}

func (s *bState) ChainID() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.chainID
}

func (s *bState) SetChainID(newID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.chainID = newID
}
