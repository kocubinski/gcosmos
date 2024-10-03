package tmconsensus

import "github.com/rollchains/gordian/gcrypto"

// SparseSignatureCollection is a network- and store-optimized collection of [gcrypto.SparseSignature].
//
// A [gcrypto.SparseSignatureProof] is one public key hash
// with a collection of signatures,
// without a clear relationship to any particular block hash.
// Using a map[string]gcrypto.SparseSignatureProof
// requires the caller to verify every public key hash in every proof.
// So instead, with the SparseSignatureCollection,
// we raise the public key hash up a level so it only exists once.
//
// This type lives in tmconsensus instead of gcrypto
// due to its larger awareness of block hashes,
// which are outside the scope of the gcrypto package.
type SparseSignatureCollection struct {
	// There is exactly one public key hash
	// the keys and signatures in the entire collection.
	// Consumers must assume this slice is shared across many goroutines,
	// and therefore must never modify the slice.
	PubKeyHash []byte

	// Mapping of block hash to sparse signatures.
	// The empty string represents a vote for nil.
	BlockSignatures map[string][]gcrypto.SparseSignature
}
