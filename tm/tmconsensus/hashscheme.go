package tmconsensus

import "github.com/rollchains/gordian/gcrypto"

// HashScheme defines ways to determine various hashes in a consensus engine.
type HashScheme interface {
	// Block calculates and returns the hash of a block,
	// without consulting or modifying [Block.Hash].
	Block(Block) ([]byte, error)

	// PubKeys calculates and returns the hash of the ordered set of public keys.
	PubKeys([]gcrypto.PubKey) ([]byte, error)

	// VotePowers calculates and returns the hash of the ordered set of voting power,
	// mapped 1:1 with the ordered set of public keys.
	VotePowers([]uint64) ([]byte, error)
}
