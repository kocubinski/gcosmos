package tmmirror

type backfillCommitStatus uint8

//go:generate go run golang.org/x/tools/cmd/stringer -type backfillCommitStatus -trimprefix=backfillCommit
const (
	// Invalid zero value.
	backfillCommitInvalid backfillCommitStatus = iota

	// Commit info was successfully backfilled.
	backfillCommitAccepted

	// The commit proof's PubKeyHash doesn't match what is in the round view.
	backfillCommitPubKeyHashMismatch

	// The backfill was rejected.
	// Maybe this should be more granular to indicate the reason it was rejected.
	backfillCommitRejected
)
