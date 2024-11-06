package txmemstore

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"cosmossdk.io/core/server"
	"cosmossdk.io/core/transaction"
	"github.com/gordian-engine/gcosmos/internal/copy/glog"
)

type Store struct {
	log *slog.Logger

	mu        sync.Mutex
	txsByHash map[[32]byte]indexedTx
}

type indexedTx struct {
	Height uint64

	Bytes  []byte
	Result server.TxResult
}

func NewStore(log *slog.Logger) *Store {
	return &Store{
		log: log,

		txsByHash: make(map[[32]byte]indexedTx),
	}
}

var _ = glog.Hex(nil)

func (s *Store) SaveBlockResult(
	ctx context.Context,
	req server.BlockRequest[transaction.Tx],
	resp server.BlockResponse,
) error {
	txs := req.Txs
	if len(txs) != len(resp.TxResults) {
		panic(fmt.Errorf(
			"transaction count mismatch: %d transaction(s) and %d results",
			len(txs), len(resp.TxResults),
		))
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for i, tx := range txs {
		s.txsByHash[tx.Hash()] = indexedTx{
			Height: req.Height,

			Bytes: tx.Bytes(),

			Result: resp.TxResults[i],
		}
	}

	return nil
}

func (s *Store) LoadTxByHash(_ context.Context, hash []byte) (
	height uint64,
	txBytes []byte,
	result server.TxResult,
	err error,
) {
	if len(hash) != 32 {
		return 0, nil, server.TxResult{}, fmt.Errorf("invalid hash length (want 32, got %d)", len(hash))
	}

	var ha [32]byte
	_ = append(ha[:0], hash...)

	s.mu.Lock()
	defer s.mu.Unlock()

	itx, ok := s.txsByHash[ha]
	if !ok {
		return 0, nil, server.TxResult{}, fmt.Errorf("unknown hash %x", hash)
	}

	return itx.Height, itx.Bytes, itx.Result, nil
}
