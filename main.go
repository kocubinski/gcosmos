// Command gcosmos wraps the Cosmos SDK simappv2
// and adds a "gstart" subcommand to run parts of Gordian.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"

	clienthelpers "cosmossdk.io/client/v2/helpers"
	sdkflags "github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/gordian-engine/gcosmos/internal/gci"
	"github.com/spf13/cobra"
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
	pFlags := rootCmd.PersistentFlags()
	if pFlags.Lookup(sdkflags.FlagTrace) == nil {
		pFlags.Bool(sdkflags.FlagTrace, false, "print out full stack trace on errors")
	}

	return rootCmd.ExecuteContext(ctx)
}
