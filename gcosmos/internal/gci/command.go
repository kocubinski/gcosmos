package gci

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"

	"cosmossdk.io/simapp/v2"
	"github.com/cosmos/cosmos-sdk/server"
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
	"github.com/rs/zerolog"
	slogzerolog "github.com/samber/slog-zerolog/v2"
	"github.com/spf13/cobra"
)

func StartGordianCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "gstart",
		Short: "Start the gordian application",
		RunE: func(cmd *cobra.Command, args []string) error {
			serverCtx := server.GetServerContextFromCmd(cmd)

			// The simapp and app manager are the core SDK pieces required
			// to integrate with a consensus engine.
			sa := simapp.NewSimApp(serverCtx.Logger, serverCtx.Viper)
			am := sa.App.AppManager
			_ = am // Not actually integrated yet.

			// We don't want to run two different logging backends,
			// so use the third-party slogzerolog library to wrap the zerolog logger
			// into an slog handler for Gordian compatibility.
			// Note that the logger appears to currently suppress messages at info level.
			zlog := serverCtx.Logger.Impl().(*zerolog.Logger)
			log := slog.New(
				slogzerolog.Option{Level: slog.LevelDebug, Logger: zlog}.NewZerologHandler(),
			)

			ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt)
			defer cancel()

			// Right now, only start a tmlibp2p host, just to prove that gordian and the SDK
			// can both be imported into the same program.
			h, err := tmlibp2p.NewHost(
				ctx,
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

			log.Info("Started libp2p host", "id", h.Libp2pHost().ID().String())

			defer func() {
				if err := h.Close(); err != nil {
					log.Warn("Error closing libp2p host", "err", err)
				}
			}()

			// TODO: how to set up signer?
			var signer gcrypto.Signer

			return runStateMachine(ctx, log, h, signer)
		},
	}
	return cmd
}

func runStateMachine(
	ctx context.Context,
	log *slog.Logger,
	h *tmlibp2p.Host,
	signer gcrypto.Signer,
) error {
	const chainID = "gcosmos"
	// We need a cancelable context if we fail partway through setup.
	// Be sure to defer cancel() after other deferred
	// close and cleanup calls, for types dependent on
	// a parent context cancellation.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Just reassign ctx here because we will not have any further references to the root context,
	// other than explicit cancel calls to ensure clean shutdown.
	wd, ctx := gwatchdog.NewWatchdog(ctx, log.With("sys", "watchdog"))

	reg := new(gcrypto.Registry)
	gcrypto.RegisterEd25519(reg)
	codec := tmjson.MarshalCodec{
		CryptoRegistry: reg,
	}
	conn, err := tmlibp2p.NewConnection(
		ctx,
		log.With("sys", "libp2pconn"),
		h,
		codec,
	)
	if err != nil {
		return fmt.Errorf("failed to build libp2p connection: %w", err)
	}
	defer conn.Disconnect()
	defer cancel()

	var as *tmmemstore.ActionStore
	if signer != nil {
		as = tmmemstore.NewActionStore()
	}

	bs := tmmemstore.NewBlockStore()
	fs := tmmemstore.NewFinalizationStore()
	ms := tmmemstore.NewMirrorStore()
	rs := tmmemstore.NewRoundStore()
	vs := tmmemstore.NewValidatorStore(tmconsensustest.SimpleHashScheme{})

	blockFinCh := make(chan tmdriver.FinalizeBlockRequest)
	initChainCh := make(chan tmdriver.InitChainRequest)

	// TODO: driver instantiation, and consensus strategy, would usually go here.
	// Obviously the nop consensus strategy isn't very useful.
	var cStrat tmconsensus.ConsensusStrategy = tmconsensustest.NopConsensusStrategy{}

	var signerPubKey gcrypto.PubKey
	if signer != nil {
		signerPubKey = signer.PubKey()
		// TODO: probably pass signerPubKey to consensus strategy
		_ = signerPubKey
	}

	gs := tmgossip.NewChattyStrategy(ctx, log.With("sys", "chattygossip"), conn)

	// TODO: when should metrics be enabled?
	metricsCh := make(chan tmengine.Metrics)

	e, err := tmengine.New(
		ctx,
		log.With("sys", "engine"),
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
		tmengine.WithGossipStrategy(gs),

		tmengine.WithGenesis(&tmconsensus.ExternalGenesis{
			ChainID:           chainID,
			InitialHeight:     1,
			InitialAppState:   strings.NewReader(""), // No initial app state for echo app.
			GenesisValidators: nil,                   // TODO: where will the validators come from?
		}),

		tmengine.WithTimeoutStrategy(ctx, tmengine.LinearTimeoutStrategy{}),

		tmengine.WithBlockFinalizationChannel(blockFinCh),
		tmengine.WithInitChainChannel(initChainCh),

		tmengine.WithSigner(signer),

		tmengine.WithMetricsChannel(metricsCh),

		tmengine.WithWatchdog(wd),
	)
	if err != nil {
		return fmt.Errorf("failed to build engine: %w", err)
	}

	conn.SetConsensusHandler(ctx, tmconsensus.AcceptAllValidFeedbackMapper{
		Handler: e,
	})
	defer e.Wait()

	if signer == nil {
		log.Info("Running follower engine...")
	} else {
		log.Info("Running engine...")
	}
	<-ctx.Done()
	log.Info("Shutting down...")

	return nil
}
