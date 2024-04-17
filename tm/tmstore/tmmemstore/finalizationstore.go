package tmmemstore

import (
	"context"
	"slices"
	"sync"

	"github.com/rollchains/gordian/tm/tmconsensus"
)

type FinalizationStore struct {
	mu sync.RWMutex

	byHeight map[uint64]fin
}

type fin struct {
	H            uint64
	R            uint32
	BlockHash    string
	Vals         []tmconsensus.Validator
	AppStateHash string
}

func NewFinalizationStore() *FinalizationStore {
	return &FinalizationStore{
		byHeight: make(map[uint64]fin),
	}
}

func (s *FinalizationStore) SaveFinalization(
	ctx context.Context,
	height uint64, round uint32,
	blockHash string,
	vals []tmconsensus.Validator,
	appStateHash string,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.byHeight[height] = fin{
		H: height, R: round,
		BlockHash:    blockHash,
		Vals:         slices.Clone(vals),
		AppStateHash: appStateHash,
	}

	return nil
}

func (s *FinalizationStore) LoadFinalizationByHeight(ctx context.Context, height uint64) (
	round uint32,
	blockHash string,
	vals []tmconsensus.Validator,
	appStateHash string,
	err error,
) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	fin, ok := s.byHeight[height]
	if !ok {
		return 0, "", nil, "", tmconsensus.HeightUnknownError{Want: height}
	}

	return fin.R, fin.BlockHash, slices.Clone(fin.Vals), fin.AppStateHash, nil
}
