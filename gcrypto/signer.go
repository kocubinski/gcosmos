package gcrypto

import "context"

// Signer produces cryptographic signatures against an input.
type Signer interface {
	// PubKey returns the gcrypto public key.
	PubKey() PubKey

	// Sign returns the signature for a given input.
	// It accepts a context in case the signing happens remotely.
	Sign(ctx context.Context, input []byte) (signature []byte, err error)
}
