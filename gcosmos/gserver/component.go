package gserver

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"cosmossdk.io/core/transaction"
	cosmoslog "cosmossdk.io/log"
	serverv2 "cosmossdk.io/server/v2"
	"github.com/cometbft/cometbft/privval"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/libp2p/go-libp2p"
	"github.com/rollchains/gordian/gcrypto"
	"github.com/rollchains/gordian/gwatchdog"
	"github.com/rollchains/gordian/tm/tmcodec/tmjson"
	"github.com/rollchains/gordian/tm/tmconsensus"
	"github.com/rollchains/gordian/tm/tmconsensus/tmconsensustest"
	"github.com/rollchains/gordian/tm/tmdriver"
	"github.com/rollchains/gordian/tm/tmengine"
	"github.com/rollchains/gordian/tm/tmgossip"
	"github.com/rollchains/gordian/tm/tmp2p/tmlibp2p"
	"github.com/rollchains/gordian/tm/tmstore/tmmemstore"
	"github.com/spf13/viper"
)

// The various interfaces we expect a Component to satisfy.
var (
	_ serverv2.ServerComponent[serverv2.AppI[transaction.Tx], transaction.Tx] = (*Component[transaction.Tx])(nil)
)

// Component is a server component to be injected into the Cosmos SDK server module.
type Component[T transaction.Tx] struct {
	rootCtx context.Context
	cancel  context.CancelCauseFunc

	log *slog.Logger

	app serverv2.AppI[T]

	// Partially set up during Init,
	// then used during Start.
	opts []tmengine.Opt

	// Configured during Start, and needs a clean shutdown during Stop.
	h      *tmlibp2p.Host
	conn   *tmlibp2p.Connection
	e      *tmengine.Engine
	driver *driver[T]
}

// NewComponent returns a new server component
// ready to be supplied to the Cosmos SDK server module.
//
// It accepts a *slog.Logger directly to avoid dealing with SDK loggers.
func NewComponent[T transaction.Tx](
	rootCtx context.Context,
	log *slog.Logger,
) (*Component[T], error) {
	var c Component[T]
	c.rootCtx, c.cancel = context.WithCancelCause(rootCtx)
	c.log = log.With("sys", "engine")

	return &c, nil
}

func (c *Component[T]) Name() string {
	return "gordian"
}

func (c *Component[T]) Start(ctx context.Context) error {
	h, err := tmlibp2p.NewHost(
		c.rootCtx,
		tmlibp2p.HostOptions{
			Options: []libp2p.Option{
				// No explicit listen address.

				// Unsure if this is something we always want.
				// Can be controlled by a flag later if undesirable by default.
				libp2p.ForceReachabilityPublic(),
			},
		},
	)
	if err != nil {
		return fmt.Errorf("failed to create libp2p host: %w", err)
	}
	c.h = h

	c.log.Info("Started libp2p host", "id", h.Libp2pHost().ID().String())

	reg := new(gcrypto.Registry)
	gcrypto.RegisterEd25519(reg)
	codec := tmjson.MarshalCodec{
		CryptoRegistry: reg,
	}
	conn, err := tmlibp2p.NewConnection(
		c.rootCtx,
		c.log.With("sys", "libp2pconn"),
		h,
		codec,
	)
	if err != nil {
		return fmt.Errorf("failed to build libp2p connection: %w", err)
	}
	c.conn = conn

	initChainCh := make(chan tmdriver.InitChainRequest)
	d, err := newDriver(
		c.rootCtx,
		ctx,
		c.log.With("serversys", "driver"),
		c.app.GetConsensusAuthority(),
		c.app.GetAppManager(),
		initChainCh,
	)
	if err != nil {
		return fmt.Errorf("failed to create driver: %w", err)
	}
	c.driver = d

	opts := c.opts
	c.opts = nil // Don't need to reference the slice after this, so allow it to be GCed.

	// Extra options that we couldn't set earlier for whatever reason:

	// Depends on conn.
	gs := tmgossip.NewChattyStrategy(ctx, c.log.With("sys", "chattygossip"), conn)
	opts = append(opts, tmengine.WithGossipStrategy(gs))

	// No point in creating this channel before a call to Start.
	opts = append(opts, tmengine.WithInitChainChannel(initChainCh))

	// Could be sooner but it's easier to just take this context late here.
	wd, wdCtx := gwatchdog.NewWatchdog(c.rootCtx, c.log.With("sys", "watchdog"))
	opts = append(opts, tmengine.WithWatchdog(wd))

	// TODO: the block finalization channel needs to be exposed somewhere.
	blockFinCh := make(chan tmdriver.FinalizeBlockRequest)
	opts = append(opts, tmengine.WithBlockFinalizationChannel(blockFinCh))

	// The timeout strategy pairs with a context,
	// so it makes sense to delay this until we have a watchdog context available.
	opts = append(opts, tmengine.WithTimeoutStrategy(wdCtx, tmengine.LinearTimeoutStrategy{}))

	e, err := tmengine.New(wdCtx, c.log.With("sys", "engine"), opts...)
	if err != nil {
		return fmt.Errorf("failed to start engine: %w", err)
	}
	c.e = e

	// Plain context here; if canceled, this will fail, which is fine.
	conn.SetConsensusHandler(ctx, tmconsensus.AcceptAllValidFeedbackMapper{
		Handler: e,
	})

	return nil
}

func (c *Component[T]) Stop(_ context.Context) error {
	c.cancel(errors.New("stopped via SDK server module"))
	if c.e != nil {
		c.e.Wait()
	}
	if c.driver != nil {
		c.driver.Wait()
	}
	if c.conn != nil {
		c.conn.Disconnect()
	}
	if c.h != nil {
		if err := c.h.Close(); err != nil {
			c.log.Warn("Error closing tmp2p host", "err", err)
		}
	}
	return nil
}

func (c *Component[T]) Init(app serverv2.AppI[T], v *viper.Viper, log cosmoslog.Logger) error {
	if c.log == nil {
		l, ok := log.Impl().(*slog.Logger)
		if !ok {
			return errors.New("(*gserver.Component).Init: log must be set during gserver.NewServerModule, or Init log must be implemented by *slog.Logger")
		}
		c.log = l
	}

	c.app = app

	// Load the comet config, in order to read the privval key from disk.
	// We don't really care about the state file,
	// but we need to to call LoadFilePV,
	// to get to the FilePVKey,
	// which gives us the PrivKey.
	cometConfig := client.GetConfigFromViper(v)
	fpv := privval.LoadFilePV(cometConfig.PrivValidatorKeyFile(), cometConfig.PrivValidatorStateFile())
	privKey := fpv.Key.PrivKey
	if privKey.Type() != "ed25519" {
		panic(fmt.Errorf(
			"gcosmos only understands ed25519 signing keys; got %q",
			privKey.Type(),
		))
	}

	var signer gcrypto.Signer
	// TODO: we should allow a way to explicitly NOT provide a signer.
	signer = gcrypto.NewEd25519Signer(ed25519.PrivateKey(privKey.Bytes()))

	var as *tmmemstore.ActionStore
	if signer != nil {
		as = tmmemstore.NewActionStore()
	}

	bs := tmmemstore.NewBlockStore()
	fs := tmmemstore.NewFinalizationStore()
	ms := tmmemstore.NewMirrorStore()
	rs := tmmemstore.NewRoundStore()
	vs := tmmemstore.NewValidatorStore(tmconsensustest.SimpleHashScheme{})

	const chainID = "TODO:TEMPORARY_CHAIN_ID" // Need to get this from the SDK.

	// TODO: driver instantiation, and consensus strategy, would usually go here.
	// Obviously the nop consensus strategy isn't very useful.
	var cStrat tmconsensus.ConsensusStrategy = tmconsensustest.NopConsensusStrategy{}

	c.opts = []tmengine.Opt{
		tmengine.WithSigner(signer),

		tmengine.WithActionStore(as),
		tmengine.WithBlockStore(bs),
		tmengine.WithFinalizationStore(fs),
		tmengine.WithMirrorStore(ms),
		tmengine.WithRoundStore(rs),
		tmengine.WithValidatorStore(vs),

		tmengine.WithHashScheme(tmconsensustest.SimpleHashScheme{}),
		tmengine.WithSignatureScheme(tmconsensustest.SimpleSignatureScheme{}),
		tmengine.WithCommonMessageSignatureProofScheme(gcrypto.SimpleCommonMessageSignatureProofScheme),

		tmengine.WithConsensusStrategy(cStrat),

		tmengine.WithGenesis(&tmconsensus.ExternalGenesis{
			ChainID:           chainID,
			InitialHeight:     1,
			InitialAppState:   strings.NewReader(""), // No initial app state for echo app.
			GenesisValidators: nil,                   // TODO: where will the validators come from?
		}),

		// NOTE: there are remaining required options that we shouldn't initialize here,
		// but instead they will be added during the Start call.
	}

	return nil
}

// WriteDefaultConfigAt satisfies an undocumented interface,
// and here we emulate what Comet does in order to get past some error expecting this file to exist.
func (c *Component[T]) WriteDefaultConfigAt(configPath string) error {
	f, err := os.Create(filepath.Join(configPath, "config.toml"))
	if err != nil {
		return fmt.Errorf("could not create empty config file: %w", err)
	}
	return f.Close()
}
