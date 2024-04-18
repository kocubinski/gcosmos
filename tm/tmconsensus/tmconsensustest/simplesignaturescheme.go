package tmconsensustest

import (
	"fmt"
	"io"

	"github.com/rollchains/gordian/tm/tmconsensus"
)

// SimpleSignatureScheme is a very basic signature scheme used for tests.
// Its produced signing content is intended to be human-readable
// and delimited across multiple lines,
// so that if unexpected content is being signed,
// it ought to be straightforward to determine what incorrect content was used.
//
// If this scheme were used in production,
// it could be used for replay attacks on other chains
// that reuse the same validator private keys;
// at a minimum, the chain ID would need to be included.
type SimpleSignatureScheme struct{}

var _ tmconsensus.SignatureScheme = SimpleSignatureScheme{}

func (s SimpleSignatureScheme) WriteProposalSigningContent(w io.Writer, b tmconsensus.Block, round uint32) (int, error) {
	return fmt.Fprintf(w, `PROPOSAL:
Height=%d
Round=%d
PrevBlockHash=%x
PrevAppStateHash=%x
DataID=%x
`, b.Height, round, b.PrevBlockHash, b.PrevAppStateHash, b.DataID)
}

func (s SimpleSignatureScheme) WritePrevoteSigningContent(w io.Writer, vt tmconsensus.VoteTarget) (int, error) {
	return fmt.Fprintf(w, `PREVOTE:
Height=%d
Round=%d
BlockHash=%x
`, vt.Height, vt.Round, vt.BlockHash)
}

func (s SimpleSignatureScheme) WriteNilPrevoteSigningContent(w io.Writer, vt tmconsensus.VoteTarget) (int, error) {
	return fmt.Fprintf(w, `NIL PREVOTE:
Height=%d
Round=%d
`, vt.Height, vt.Round)
}

func (s SimpleSignatureScheme) WritePrecommitSigningContent(w io.Writer, vt tmconsensus.VoteTarget) (int, error) {
	return fmt.Fprintf(w, `PRECOMMIT:
Height=%d
Round=%d
BlockHash=%x
`, vt.Height, vt.Round, vt.BlockHash)
}

func (s SimpleSignatureScheme) WriteNilPrecommitSigningContent(w io.Writer, vt tmconsensus.VoteTarget) (int, error) {
	return fmt.Fprintf(w, `NIL PRECOMMIT:
Height=%d
Round=%d
`, vt.Height, vt.Round)
}
