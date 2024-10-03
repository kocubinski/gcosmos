package tmmemstore

import (
	"context"
	"fmt"
	"sync"

	"github.com/rollchains/gordian/tm/tmconsensus"
	"github.com/rollchains/gordian/tm/tmstore"
)

type RoundStore struct {
	mu sync.RWMutex

	// Height -> Round -> Hash -> Headers.
	// There will almost always be a single proposed header under the hash,
	// but with multiple proposers, it is possible that two independent proposers
	// will propose an identical header,
	// differing only by the proposal metadata.
	phs map[uint64]map[uint32]map[string][]tmconsensus.ProposedHeader

	// Height -> Round -> collection.
	prevotes, precommits map[uint64]map[uint32]tmconsensus.SparseSignatureCollection
}

func NewRoundStore() *RoundStore {
	return &RoundStore{
		phs: make(map[uint64]map[uint32]map[string][]tmconsensus.ProposedHeader),

		prevotes:   make(map[uint64]map[uint32]tmconsensus.SparseSignatureCollection),
		precommits: make(map[uint64]map[uint32]tmconsensus.SparseSignatureCollection),
	}
}

func (s *RoundStore) SaveRoundProposedHeader(ctx context.Context, ph tmconsensus.ProposedHeader) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	byRound, ok := s.phs[ph.Header.Height]
	if !ok {
		byRound = make(map[uint32]map[string][]tmconsensus.ProposedHeader)
		s.phs[ph.Header.Height] = byRound
	}

	byHash, ok := byRound[ph.Round]
	if !ok {
		byHash = make(map[string][]tmconsensus.ProposedHeader)
		byRound[ph.Round] = byHash
	}

	if havePHs, ok := byHash[string(ph.Header.Hash)]; ok {
		// Already have a proposed header for this hash.
		// See if it is from the same proposer.
		for _, have := range havePHs {
			if ph.ProposerPubKey.Equal(have.ProposerPubKey) {
				return tmstore.OverwriteError{
					Field: "pubkey",
					Value: fmt.Sprintf("%x", ph.ProposerPubKey.PubKeyBytes()),
				}
			}
		}
	}

	byHash[string(ph.Header.Hash)] = append(byHash[string(ph.Header.Hash)], ph)

	return nil
}

func (s *RoundStore) OverwriteRoundPrevoteProofs(
	ctx context.Context,
	height uint64,
	round uint32,
	proofs tmconsensus.SparseSignatureCollection,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	byRound, ok := s.prevotes[height]
	if !ok {
		byRound = make(map[uint32]tmconsensus.SparseSignatureCollection)
		s.prevotes[height] = byRound
	}

	byRound[round] = proofs
	return nil
}

func (s *RoundStore) OverwriteRoundPrecommitProofs(
	ctx context.Context,
	height uint64,
	round uint32,
	proofs tmconsensus.SparseSignatureCollection,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	byRound, ok := s.precommits[height]
	if !ok {
		byRound = make(map[uint32]tmconsensus.SparseSignatureCollection)
		s.precommits[height] = byRound
	}

	byRound[round] = proofs
	return nil
}

func (s *RoundStore) LoadRoundState(ctx context.Context, height uint64, round uint32) (
	phs []tmconsensus.ProposedHeader,
	prevotes, precommits tmconsensus.SparseSignatureCollection,
	err error,
) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if roundMap, ok := s.phs[height]; ok {
		if hashMap, ok := roundMap[round]; ok && len(hashMap) > 0 {
			phs = make([]tmconsensus.ProposedHeader, 0, len(hashMap))
			for _, phsForHash := range hashMap {
				phs = append(phs, phsForHash...)
			}
		}
	}

	if prevoteHeightMap, ok := s.prevotes[height]; ok {
		prevotes = prevoteHeightMap[round]
	}

	if precommitHeightMap, ok := s.precommits[height]; ok {
		precommits = precommitHeightMap[round]
	}

	if phs == nil && prevotes.BlockSignatures == nil && precommits.BlockSignatures == nil {
		return nil, prevotes, precommits, tmconsensus.RoundUnknownError{WantHeight: height, WantRound: round}
	}

	return phs, prevotes, precommits, nil
}
