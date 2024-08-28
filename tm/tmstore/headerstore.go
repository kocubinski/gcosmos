package tmstore

import (
	"context"

	"github.com/rollchains/gordian/tm/tmconsensus"
)

// HeaderStore is the store that the Engine's Mirror uses for committed block headers.
// The committed headers always lag the voting round by two heights.
//
// The proofs associated with the committed headers must be considered subjective.
// That is, while the proof is expected to represent >2/3 voting power for the header,
// it is not necessarily the same set of signatures
// as in the subsequent block's PrevCommitProof field.
type HeaderStore interface {
	SaveHeader(ctx context.Context, ch tmconsensus.CommittedHeader) error

	LoadHeader(ctx context.Context, height uint64) (tmconsensus.CommittedHeader, error)
}
