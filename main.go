// Command gcosmos wraps the Cosmos SDK simappv2
// and adds a "gstart" subcommand to run parts of Gordian.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"

	clienthelpers "cosmossdk.io/client/v2/helpers"
	sdkflags "github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/server"
	svrcmd "github.com/cosmos/cosmos-sdk/server/cmd"
	"github.com/gordian-engine/gcosmos/internal/gci"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func main() {
	homeDir, err := clienthelpers.GetNodeHomeDirectory(".gcosmos")
	if err != nil {
		panic(fmt.Errorf("failed to get user home directory; use environment file or provide --home flag: %w", err))
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	var rootCmd *cobra.Command
	if gci.RunCometInsteadOfGordian {
		panic(fmt.Errorf("TODO: re-enable starting with comet (this simplifies cross-checking behavior"))
	} else {
		rootCmd = gci.NewGcosmosCommand(
			ctx,
			slog.New(slog.NewTextHandler(os.Stderr, nil)),
			homeDir,
			os.Args[1:],
		)
	}
	if err := Execute(ctx, rootCmd, "", homeDir); err != nil {
		fmt.Fprintln(rootCmd.OutOrStderr(), err)
		os.Exit(1)
	}
}

// Execute executes the root command of an application. It handles creating a
// server context object with the appropriate server and client objects injected
// into the underlying stdlib Context. It also handles adding core CLI flags,
// specifically the logging flags. It returns an error upon execution failure.
func Execute(ctx context.Context, rootCmd *cobra.Command, envPrefix, defaultHome string) error {
	ctx = svrcmd.CreateExecuteContext(ctx)

	pFlags := rootCmd.PersistentFlags()

	if pFlags.Lookup(sdkflags.FlagLogLevel) == nil {
		pFlags.String(sdkflags.FlagLogLevel, zerolog.InfoLevel.String(), "The logging level (trace|debug|info|warn|error|fatal|panic|disabled or '*:<level>,<key>:<level>')")
	}
	if pFlags.Lookup(sdkflags.FlagLogFormat) == nil {
		pFlags.String(sdkflags.FlagLogFormat, "plain", "The logging format (json|plain)")
	}
	if pFlags.Lookup(sdkflags.FlagLogNoColor) == nil {
		pFlags.Bool(sdkflags.FlagLogNoColor, false, "Disable colored logs")
	}
	if pFlags.Lookup(sdkflags.FlagHome) == nil {
		pFlags.StringP(sdkflags.FlagHome, "", defaultHome, "directory for config and data")
	}
	if pFlags.Lookup(server.FlagTrace) == nil {
		pFlags.Bool(server.FlagTrace, false, "print out full stack trace on errors")
	}

	// update the global viper with the root command's configuration
	viper.SetEnvPrefix(envPrefix)
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))
	viper.AutomaticEnv()

	return rootCmd.ExecuteContext(ctx)
}
