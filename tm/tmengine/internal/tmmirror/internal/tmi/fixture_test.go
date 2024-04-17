package tmi_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/rollchains/gordian/internal/gtest"
	"github.com/rollchains/gordian/tm/tmconsensus"
	"github.com/rollchains/gordian/tm/tmconsensus/tmconsensustest"
	"github.com/rollchains/gordian/tm/tmengine/internal/tmmirror/internal/tmi"
	"github.com/rollchains/gordian/tm/tmstore/tmmemstore"
)

// KernelFixture is a fixture to simplify Kernel construction.
//
// This type may move to a tmitest package if scope grows enough.
// But for now it is just an exported declaration in the _test package.
type KernelFixture struct {
	Log *slog.Logger

	Fx *tmconsensustest.StandardFixture

	// These channels are bidirectional in the fixture,
	// because they are write-only in the config.
	VotingViewOutCh, CommittingViewOutCh chan tmconsensus.VersionedRoundView

	NHRRequests        chan chan tmi.NetworkHeightRound
	SnapshotRequests   chan tmi.SnapshotRequest
	ViewLookupRequests chan tmi.ViewLookupRequest

	AddPBRequests        chan tmconsensus.ProposedBlock
	AddPrevoteRequests   chan tmi.AddPrevoteRequest
	AddPrecommitRequests chan tmi.AddPrecommitRequest

	Cfg tmi.KernelConfig
}

func NewKernelFixture(t *testing.T, nVals int) *KernelFixture {
	fx := tmconsensustest.NewStandardFixture(nVals)

	votingViewOutCh := make(chan tmconsensus.VersionedRoundView)     // Unbuffered like in production.
	committingViewOutCh := make(chan tmconsensus.VersionedRoundView) // Unbuffered like in production.

	// 1-buffered like production:
	// "because it is possible that the caller
	// may initiate the request and do work before reading the response."
	nhrRequests := make(chan chan tmi.NetworkHeightRound, 1)
	snapshotRequests := make(chan tmi.SnapshotRequest, 1)
	viewLookupRequests := make(chan tmi.ViewLookupRequest, 1)

	// "Arbitrarily sized to allow some concurrent requests,
	// with low likelihood of blocking."
	addPBRequests := make(chan tmconsensus.ProposedBlock, 8)

	// "The calling method blocks on the response regardless,
	// so no point in buffering these."
	addPrevoteRequests := make(chan tmi.AddPrevoteRequest)
	addPrecommitRequests := make(chan tmi.AddPrecommitRequest)

	return &KernelFixture{
		Log: gtest.NewLogger(t),

		Fx: fx,

		VotingViewOutCh:     votingViewOutCh,
		CommittingViewOutCh: committingViewOutCh,

		NHRRequests:        nhrRequests,
		SnapshotRequests:   snapshotRequests,
		ViewLookupRequests: viewLookupRequests,

		AddPBRequests:        addPBRequests,
		AddPrevoteRequests:   addPrevoteRequests,
		AddPrecommitRequests: addPrecommitRequests,

		Cfg: tmi.KernelConfig{
			Store:          tmmemstore.NewMirrorStore(),
			BlockStore:     tmmemstore.NewBlockStore(),
			RoundStore:     tmmemstore.NewRoundStore(),
			ValidatorStore: tmmemstore.NewValidatorStore(fx.HashScheme),

			InitialHeight:     1,
			InitialValidators: fx.Vals(),

			HashScheme:                        fx.HashScheme,
			SignatureScheme:                   fx.SignatureScheme,
			CommonMessageSignatureProofScheme: fx.CommonMessageSignatureProofScheme,

			VotingViewOut:     votingViewOutCh,
			CommittingViewOut: committingViewOutCh,

			NHRRequests:        nhrRequests,
			SnapshotRequests:   snapshotRequests,
			ViewLookupRequests: viewLookupRequests,

			AddPBRequests:        addPBRequests,
			AddPrevoteRequests:   addPrevoteRequests,
			AddPrecommitRequests: addPrecommitRequests,
		},
	}
}

func (f *KernelFixture) NewKernel(ctx context.Context) *tmi.Kernel {
	k, err := tmi.NewKernel(ctx, f.Log, f.Cfg)
	if err != nil {
		panic(err)
	}

	return k
}
