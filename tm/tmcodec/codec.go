package tmcodec

import (
	"github.com/rollchains/gordian/tm/tmconsensus"
)

// Marshal serializes tmconsensus values to byte slices.
type Marshaler interface {
	MarshalConsensusMessage(ConsensusMessage) ([]byte, error)

	MarshalBlock(tmconsensus.Block) ([]byte, error)
	MarshalProposedBlock(tmconsensus.ProposedBlock) ([]byte, error)

	MarshalPrevoteProof(tmconsensus.PrevoteSparseProof) ([]byte, error)
	MarshalPrecommitProof(tmconsensus.PrecommitSparseProof) ([]byte, error)
}

// Marshal deserializes byte slices into tmconsensus values.
type Unmarshaler interface {
	UnmarshalConsensusMessage([]byte, *ConsensusMessage) error

	UnmarshalBlock([]byte, *tmconsensus.Block) error
	UnmarshalProposedBlock([]byte, *tmconsensus.ProposedBlock) error

	UnmarshalPrevoteProof([]byte, *tmconsensus.PrevoteSparseProof) error
	UnmarshalPrecommitProof([]byte, *tmconsensus.PrecommitSparseProof) error
}

// MarshalCodec marshals and unmarshals tmconsensus values, producing byte slices.
// In the future we may have a plain Codec type that operates against an io.Writer.
type MarshalCodec interface {
	Marshaler
	Unmarshaler
}

// ConsensusMessage is a wrapper around the three types of consensus values sent during rounds.
// Exactly one of the fields must be set.
// If zero or multiple fields are set, behavior is undefined.
type ConsensusMessage struct {
	ProposedBlock *tmconsensus.ProposedBlock

	PrevoteProof   *tmconsensus.PrevoteSparseProof
	PrecommitProof *tmconsensus.PrecommitSparseProof
}
