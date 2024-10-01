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

	// Height -> Round -> Hash -> Header.
	phs map[uint64]map[uint32]map[string]tmconsensus.ProposedHeader

	// Height -> Round -> Hash -> signature proofs.
	prevotes, precommits map[uint64]map[uint32]map[string]gcrypto.CommonMessageSignatureProof
}

func NewRoundStore() *RoundStore {
	return &RoundStore{
		phs: make(map[uint64]map[uint32]map[string]tmconsensus.ProposedHeader),

		prevotes:   make(map[uint64]map[uint32]map[string]gcrypto.CommonMessageSignatureProof),
		precommits: make(map[uint64]map[uint32]map[string]gcrypto.CommonMessageSignatureProof),
	}
}

func (s *RoundStore) SaveRoundProposedHeader(ctx context.Context, ph tmconsensus.ProposedHeader) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	byRound, ok := s.phs[ph.Header.Height]
	if !ok {
		byRound = make(map[uint32]map[string]tmconsensus.ProposedHeader)
		s.phs[ph.Header.Height] = byRound
	}

	byHash, ok := byRound[ph.Round]
	if !ok {
		byHash = make(map[string]tmconsensus.ProposedHeader)
		byRound[ph.Round] = byHash
	}

	if _, ok := byHash[string(ph.Header.Hash)]; ok {
		return tmstore.OverwriteError{
			Field: "hash",
			Value: fmt.Sprintf("%x", ph.Header.Hash),
		}
	}

	byHash[string(ph.Header.Hash)] = ph

	return nil
}

func (s *RoundStore) OverwriteRoundPrevoteProofs(
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

func (s *RoundStore) OverwriteRoundPrecommitProofs(
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
	phs []tmconsensus.ProposedHeader,
	prevotes, precommits map[string]gcrypto.CommonMessageSignatureProof,
	err error,
) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if roundMap, ok := s.phs[height]; ok {
		if hashMap, ok := roundMap[round]; ok && len(hashMap) > 0 {
			phs = make([]tmconsensus.ProposedHeader, 0, len(hashMap))
			for _, ph := range hashMap {
				phs = append(phs, ph)
			}
		}
	}

	if prevoteHeightMap, ok := s.prevotes[height]; ok {
		prevotes = prevoteHeightMap[round]
	}

	if precommitHeightMap, ok := s.precommits[height]; ok {
		precommits = precommitHeightMap[round]
	}

	if phs == nil && prevotes == nil && precommits == nil {
		return nil, nil, nil, tmconsensus.RoundUnknownError{WantHeight: height, WantRound: round}
	}

	return phs, prevotes, precommits, nil
}
