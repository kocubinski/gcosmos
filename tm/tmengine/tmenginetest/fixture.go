package tmenginetest

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/rollchains/gordian/gassert/gasserttest"
	"github.com/rollchains/gordian/gwatchdog"
	"github.com/rollchains/gordian/internal/gtest"
	"github.com/rollchains/gordian/tm/tmconsensus"
	"github.com/rollchains/gordian/tm/tmconsensus/tmconsensustest"
	"github.com/rollchains/gordian/tm/tmdriver"
	"github.com/rollchains/gordian/tm/tmengine"
	"github.com/rollchains/gordian/tm/tmengine/internal/tmstate/tmstatetest"
	"github.com/rollchains/gordian/tm/tmgossip/tmgossiptest"
	"github.com/rollchains/gordian/tm/tmstore/tmmemstore"
)

type Fixture struct {
	Log *slog.Logger

	Fx *tmconsensustest.StandardFixture

	ConsensusStrategy *tmconsensustest.MockConsensusStrategy
	GossipStrategy    *tmgossiptest.PassThroughStrategy

	ActionStore       *tmmemstore.ActionStore
	FinalizationStore *tmmemstore.FinalizationStore
	HeaderStore       *tmmemstore.HeaderStore
	MirrorStore       *tmmemstore.MirrorStore
	RoundStore        *tmmemstore.RoundStore
	ValidatorStore    *tmmemstore.ValidatorStore

	InitChainCh chan tmdriver.InitChainRequest

	FinalizeBlockRequests chan tmdriver.FinalizeBlockRequest

	RoundTimer *tmstatetest.MockRoundTimer

	Watchdog    *gwatchdog.Watchdog
	WatchdogCtx context.Context
}

func NewFixture(ctx context.Context, t *testing.T, nVals int) *Fixture {
	fx := tmconsensustest.NewStandardFixture(nVals)

	log := gtest.NewLogger(t)

	wd, wCtx := gwatchdog.NewNopWatchdog(ctx, log.With("sys", "watchdog"))

	// Ensure the watchdog doesn't log after test completion.
	// There ought to be a defer cancel before the call to NewFixture anyway.
	t.Cleanup(wd.Wait)

	return &Fixture{
		Log: log,

		Fx: fx,

		ConsensusStrategy: tmconsensustest.NewMockConsensusStrategy(),
		GossipStrategy:    tmgossiptest.NewPassThroughStrategy(),

		ActionStore:       tmmemstore.NewActionStore(),
		FinalizationStore: tmmemstore.NewFinalizationStore(),
		HeaderStore:       tmmemstore.NewHeaderStore(),
		MirrorStore:       tmmemstore.NewMirrorStore(),
		RoundStore:        tmmemstore.NewRoundStore(),
		ValidatorStore:    fx.NewMemValidatorStore(),

		InitChainCh: make(chan tmdriver.InitChainRequest, 1),

		FinalizeBlockRequests: make(chan tmdriver.FinalizeBlockRequest, 1),

		RoundTimer: new(tmstatetest.MockRoundTimer),

		Watchdog:    wd,
		WatchdogCtx: wCtx,
	}
}

func (f *Fixture) MustNewEngine(opts ...tmengine.Opt) *tmengine.Engine {
	e, err := tmengine.New(f.WatchdogCtx, f.Log, opts...)
	if err != nil {
		panic(err)
	}

	return e
}

// OptionMap is a map of string names to option values.
// This allows the caller to remove or override specific options in test,
// which is not a use case one would see in a production build.
type OptionMap map[string]tmengine.Opt

func (m OptionMap) ToSlice() []tmengine.Opt {
	opts := make([]tmengine.Opt, 0, len(m))
	for _, v := range m {
		opts = append(opts, v)
	}

	return opts
}

func (f *Fixture) BaseOptionMap() OptionMap {
	eg := &tmconsensus.ExternalGenesis{
		ChainID:             "my-chain",
		InitialHeight:       1,
		InitialAppState:     new(bytes.Buffer),
		GenesisValidatorSet: f.Fx.ValSet(),
	}

	return OptionMap{
		"WithGenesis": tmengine.WithGenesis(eg),

		"WithFinalizationStore": tmengine.WithFinalizationStore(f.FinalizationStore),
		"WithHeaderStore":       tmengine.WithHeaderStore(f.HeaderStore),
		"WithMirrorStore":       tmengine.WithMirrorStore(f.MirrorStore),
		"WithRoundStore":        tmengine.WithRoundStore(f.RoundStore),
		"WithValidatorStore":    tmengine.WithValidatorStore(f.ValidatorStore),

		"WithHashScheme":                        tmengine.WithHashScheme(f.Fx.HashScheme),
		"WithSignatureScheme":                   tmengine.WithSignatureScheme(f.Fx.SignatureScheme),
		"WithCommonMessageSignatureProofScheme": tmengine.WithCommonMessageSignatureProofScheme(f.Fx.CommonMessageSignatureProofScheme),

		"WithGossipStrategy":    tmengine.WithGossipStrategy(f.GossipStrategy),
		"WithConsensusStrategy": tmengine.WithConsensusStrategy(f.ConsensusStrategy),

		"WithInitChainChannel":         tmengine.WithInitChainChannel(f.InitChainCh),
		"WithBlockFinalizationChannel": tmengine.WithBlockFinalizationChannel(f.FinalizeBlockRequests),

		"WithInternalRoundTimer": tmengine.WithInternalRoundTimer(f.RoundTimer),

		"WithWatchdog": tmengine.WithWatchdog(f.Watchdog),

		"WithAssertEnv": tmengine.WithAssertEnv(gasserttest.DefaultEnv()),
	}
}

func (f *Fixture) SigningOptionMap() OptionMap {
	m := f.BaseOptionMap()

	m["WithActionStore"] = tmengine.WithActionStore(f.ActionStore)
	m["WithSigner"] = tmengine.WithSigner(tmconsensus.PassthroughSigner{
		Signer:          f.Fx.PrivVals[0].Signer,
		SignatureScheme: f.Fx.SignatureScheme,
	})

	return m
}
