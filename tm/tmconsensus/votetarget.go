package tmconsensus

// VoteTarget is the reference of the block targeted for a prevote or precommit.
type VoteTarget struct {
	Height uint64
	Round  uint32

	// While the block hash is conventionally []byte,
	// we use a string here for simpler map keys
	// and because the hash is intended to be immutable after creation.
	// Note that an empty string indicates a nil vote.
	BlockHash string
}
