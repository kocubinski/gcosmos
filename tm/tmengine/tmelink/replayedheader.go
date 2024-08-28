package tmelink

import (
	"github.com/rollchains/gordian/tm/tmconsensus"
)

type ReplayedHeaderRequest struct {
	Header tmconsensus.Header
	Proof  tmconsensus.CommitProof

	Resp chan<- ReplayedHeaderResponse
}

type ReplayedHeaderResponse struct {
	// TODO: there is likely some meaningful feedback we can use here,
	// it just isn't clear exactly what that is yet.
}
