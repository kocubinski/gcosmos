package tmi

import (
	"github.com/rollchains/gordian/gcrypto"
	"github.com/rollchains/gordian/tm/tmconsensus"
)

type PHCheckRequest struct {
	PH   tmconsensus.ProposedHeader
	Resp chan PHCheckResponse
}

type PHCheckResponse struct {
	Status PHCheckStatus

	// If the status is PHCheckAcceptable, this is the matching public key
	// so that the calling goroutine can validate the proposed header signature.
	ProposerPubKey gcrypto.PubKey

	// If the status is PHCheckNextHeight, this is a clone of the voting view.
	VotingRoundView *tmconsensus.RoundView
}

type PHCheckStatus uint8

//go:generate go run golang.org/x/tools/cmd/stringer -type PHCheckStatus -trimprefix=PHCheck
const (
	// Invalid value for 0.
	PHCheckInvalid PHCheckStatus = iota

	// We don't have the proposed header and it looks like it could be applied.
	// The calling goroutine from the Mirror (not the Kernel)
	// must still perform signature, hash, and any other validation.
	PHCheckAcceptable

	// Special case: we need to apply the previous commit info into the voting height.
	PHCheckNextHeight

	// We already have a proposed header with this signature.
	// It is possible that the proposed header is maliciously crafted,
	// with an invalid signature that matches an existing valid signature.
	// If we do propagate this through the network,
	// a node missing the proposed header will reject the original sender.
	PHCheckAlreadyHaveSignature

	// The header would have possibly been acceptable,
	// but the reported proposer public key did not match the known validators for that height.
	PHCheckSignerUnrecognized

	// The proposed header references an out-of-bounds round that is too old.
	PHCheckRoundTooOld

	// The proposed header references an out-of-bounds round that is too far in the future.
	PHCheckRoundTooFarInFuture
)
