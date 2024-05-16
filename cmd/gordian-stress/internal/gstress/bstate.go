package gstress

import "sync"

type bState struct {
	mu sync.Mutex

	app     string
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

func (s *bState) App() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.app
}

func (s *bState) SetApp(a string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.app = a
}
