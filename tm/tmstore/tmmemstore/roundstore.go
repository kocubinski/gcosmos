package tmmemstore

import (
	"context"
	"fmt"
	"sync"

	"github.com/rollchains/gordian/gcrypto"
	"github.com/rollchains/gordian/tm/tmconsensus"
	"github.com/rollchains/gordian/tm/tmstore"
)

type RoundStore struct {
	mu sync.RWMutex

	// Height -> Round -> Hash -> Block.
	pbs map[uint64]map[uint32]map[string]tmconsensus.ProposedBlock

	// Height -> Round -> Hash -> signature proofs.
	prevotes, precommits map[uint64]map[uint32]map[string]gcrypto.CommonMessageSignatureProof
}

func NewRoundStore() *RoundStore {
	return &RoundStore{
		pbs: make(map[uint64]map[uint32]map[string]tmconsensus.ProposedBlock),

		prevotes:   make(map[uint64]map[uint32]map[string]gcrypto.CommonMessageSignatureProof),
		precommits: make(map[uint64]map[uint32]map[string]gcrypto.CommonMessageSignatureProof),
	}
}

func (s *RoundStore) SaveProposedBlock(ctx context.Context, pb tmconsensus.ProposedBlock) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	byRound, ok := s.pbs[pb.Block.Height]
	if !ok {
		byRound = make(map[uint32]map[string]tmconsensus.ProposedBlock)
		s.pbs[pb.Block.Height] = byRound
	}

	byHash, ok := byRound[pb.Round]
	if !ok {
		byHash = make(map[string]tmconsensus.ProposedBlock)
		byRound[pb.Round] = byHash
	}

	if _, ok := byHash[string(pb.Block.Hash)]; ok {
		return tmstore.OverwriteError{
			Field: "hash",
			Value: fmt.Sprintf("%x", pb.Block.Hash),
		}
	}

	byHash[string(pb.Block.Hash)] = pb

	return nil
}

func (s *RoundStore) OverwritePrevoteProofs(
	ctx context.Context,
	height uint64,
	round uint32,
	proofs map[string]gcrypto.CommonMessageSignatureProof,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	byRound, ok := s.prevotes[height]
	if !ok {
		byRound = make(map[uint32]map[string]gcrypto.CommonMessageSignatureProof)
		s.prevotes[height] = byRound
	}

	byRound[round] = proofs
	return nil
}

func (s *RoundStore) OverwritePrecommitProofs(
	ctx context.Context,
	height uint64,
	round uint32,
	proofs map[string]gcrypto.CommonMessageSignatureProof,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	byRound, ok := s.precommits[height]
	if !ok {
		byRound = make(map[uint32]map[string]gcrypto.CommonMessageSignatureProof)
		s.precommits[height] = byRound
	}

	byRound[round] = proofs
	return nil
}

func (s *RoundStore) LoadRoundState(ctx context.Context, height uint64, round uint32) (
	pbs []tmconsensus.ProposedBlock,
	prevotes, precommits map[string]gcrypto.CommonMessageSignatureProof,
	err error,
) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if roundMap, ok := s.pbs[height]; ok {
		if hashMap, ok := roundMap[round]; ok && len(hashMap) > 0 {
			pbs = make([]tmconsensus.ProposedBlock, 0, len(hashMap))
			for _, pb := range hashMap {
				pbs = append(pbs, pb)
			}
		}
	}

	if prevoteHeightMap, ok := s.prevotes[height]; ok {
		prevotes = prevoteHeightMap[round]
	}

	if precommitHeightMap, ok := s.precommits[height]; ok {
		precommits = precommitHeightMap[round]
	}

	if pbs == nil && prevotes == nil && precommits == nil {
		return nil, nil, nil, tmconsensus.RoundUnknownError{WantHeight: height, WantRound: round}
	}

	return pbs, prevotes, precommits, nil
}
