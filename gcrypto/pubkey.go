package gcrypto

// PubKey is the interface for an instance of a public key.
type PubKey interface {
	// The raw bytes constituting the public key.
	// Implementers are free to return the same underlying slice on every call,
	// and callers must not modify the slice;
	// callers may also assume the slice will never be modified.
	PubKeyBytes() []byte

	// Equal reports whether the other key is of the same type
	// and has the same public key bytes.
	Equal(other PubKey) bool

	// Verify reports whether the signature is authentic,
	// for the given message against this public key.
	Verify(msg, sig []byte) bool

	// The internal name of this key's type.
	// This must be a valid ASCII string of length 8 bytes or fewer,
	// and it must be an identical string for every instance of this type.
	TypeName() string
}
