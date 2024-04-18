package tmconsensus

import (
	"bytes"
	"io"
	"sync"
)

// SignatureScheme determines the content to be signed, for consensus messages.
//
// Rather than returning a slice of bytes, its methods write to an io.Writer.
// This enables the caller to potentially reduce allocations,
// for example by reusing a bytes.Buffer:
//
//	var scheme SignatureScheme = getSignatureScheme()
//	var buf bytes.Buffer
//	for i, pv := range prevotes {
//	  buf.Reset()
//	  _, err := scheme.WritePrevoteSigningContent(&buf, pv)
//	  if err != nil {
//	    panic(err)
//	  }
//	  checkSignature(buf.Bytes(), pv.Signature, publicKeys[i])
//	}
type SignatureScheme interface {
	WriteProposalSigningContent(w io.Writer, b Block, round uint32, pbAnnotations Annotations) (int, error)

	WritePrevoteSigningContent(io.Writer, VoteTarget) (int, error)

	WritePrecommitSigningContent(io.Writer, VoteTarget) (int, error)
}

var sigBufPool = sync.Pool{
	New: func() any {
		return new(bytes.Buffer)
	},
}

// ProposalSignBytes returns a new byte slice containing
// the proposal sign bytes for b, as defined by s.
//
// Use this function for one-off calls, but prefer to maintain
// a local bytes.Buffer in loops involving signatures.
func ProposalSignBytes(b Block, round uint32, pbAnnotations Annotations, s SignatureScheme) ([]byte, error) {
	buf := sigBufPool.Get().(*bytes.Buffer)
	defer sigBufPool.Put(buf)

	buf.Reset()
	_, err := s.WriteProposalSigningContent(buf, b, round, pbAnnotations)
	if err != nil {
		return nil, err
	}

	return bytes.Clone(buf.Bytes()), nil
}

// PrevoteSignBytes returns a new byte slice containing
// the prevote sign bytes for v, as defined by s.
//
// Use this function for one-off calls, but prefer to maintain
// a local bytes.Buffer in loops involving signatures.
func PrevoteSignBytes(vt VoteTarget, s SignatureScheme) ([]byte, error) {
	buf := sigBufPool.Get().(*bytes.Buffer)
	defer sigBufPool.Put(buf)

	buf.Reset()
	_, err := s.WritePrevoteSigningContent(buf, vt)
	if err != nil {
		return nil, err
	}

	return bytes.Clone(buf.Bytes()), nil
}

// PrecommitSignBytes returns a new byte slice containing
// the precommit sign bytes for v, as defined by s.
//
// Use this function for one-off calls, but prefer to maintain
// a local bytes.Buffer in loops involving signatures.
func PrecommitSignBytes(vt VoteTarget, s SignatureScheme) ([]byte, error) {
	buf := sigBufPool.Get().(*bytes.Buffer)
	defer sigBufPool.Put(buf)

	buf.Reset()
	_, err := s.WritePrecommitSigningContent(buf, vt)
	if err != nil {
		return nil, err
	}

	return bytes.Clone(buf.Bytes()), nil
}
