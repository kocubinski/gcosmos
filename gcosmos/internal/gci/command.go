package gci

import (
	"fmt"
	"log/slog"
	"os"
	"os/signal"

	"cosmossdk.io/simapp/v2"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/libp2p/go-libp2p"
	"github.com/rollchains/gordian/tm/tmp2p/tmlibp2p"
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

			fmt.Println("Waiting for ^c")
			<-ctx.Done()
			fmt.Println("Got ^c. Stopping now.")

			return nil
		},
	}
	return cmd
}
