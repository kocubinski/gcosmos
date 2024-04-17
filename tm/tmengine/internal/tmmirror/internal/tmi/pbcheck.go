package tmi

import (
	"github.com/rollchains/gordian/gcrypto"
	"github.com/rollchains/gordian/tm/tmconsensus"
)

type PBCheckRequest struct {
	PB   tmconsensus.ProposedBlock
	Resp chan PBCheckResponse
}

type PBCheckResponse struct {
	Status PBCheckStatus

	// If the status is PBCheckAcceptable, this is the matching public key
	// so that the calling goroutine can validate the proposed block signature.
	ProposerPubKey gcrypto.PubKey

	// If the status is PBCheckNextHeight, this is a clone of the voting view.
	VotingRoundView *tmconsensus.RoundView
}

type PBCheckStatus uint8

//go:generate go run golang.org/x/tools/cmd/stringer -type PBCheckStatus -trimprefix=PBCheck
const (
	// Invalid value for 0.
	PBCheckInvalid PBCheckStatus = iota

	// We don't have the proposed block and it looks like it could be applied.
	// The calling goroutine from the Mirror (not the Kernel)
	// must still perform signature, hash, and any other validation.
	PBCheckAcceptable

	// Special case: we need to apply the previous commit info into the voting height.
	PBCheckNextHeight

	// We already have a proposed block with this signature.
	// It is possible that the proposed block is maliciously crafted,
	// with an invalid signature that matches an existing valid signature.
	// If we do propagate this through the network,
	// a node missing the proposed block will reject the original sender.
	PBCheckAlreadyHaveSignature

	// The block would have possibly been acceptable,
	// but the reported proposer public key did not match the known validators for that height.
	PBCheckSignerUnrecognized

	// The proposed block references an out-of-bounds round that is too old.
	PBCheckRoundTooOld

	// The proposed block references an out-of-bounds round that is too far in the future.
	PBCheckRoundTooFarInFuture
)
