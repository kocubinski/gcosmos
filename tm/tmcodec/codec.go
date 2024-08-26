package tmcodec

import (
	"github.com/rollchains/gordian/tm/tmconsensus"
)

// Marshal serializes tmconsensus values to byte slices.
type Marshaler interface {
	MarshalConsensusMessage(ConsensusMessage) ([]byte, error)

	MarshalHeader(tmconsensus.Header) ([]byte, error)
	MarshalProposedHeader(tmconsensus.ProposedHeader) ([]byte, error)

	MarshalPrevoteProof(tmconsensus.PrevoteSparseProof) ([]byte, error)
	MarshalPrecommitProof(tmconsensus.PrecommitSparseProof) ([]byte, error)
}

// Marshal deserializes byte slices into tmconsensus values.
type Unmarshaler interface {
	UnmarshalConsensusMessage([]byte, *ConsensusMessage) error

	UnmarshalHeader([]byte, *tmconsensus.Header) error
	UnmarshalProposedHeader([]byte, *tmconsensus.ProposedHeader) error

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
	ProposedHeader *tmconsensus.ProposedHeader

	PrevoteProof   *tmconsensus.PrevoteSparseProof
	PrecommitProof *tmconsensus.PrecommitSparseProof
}
