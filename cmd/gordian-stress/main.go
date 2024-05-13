package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"

	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	libp2phost "github.com/libp2p/go-libp2p/core/host"
	libp2ppeer "github.com/libp2p/go-libp2p/core/peer"
	"github.com/rollchains/gordian/cmd/gordian-stress/internal/gstress"
	"github.com/rollchains/gordian/tm/tmp2p/tmlibp2p"
	"github.com/spf13/cobra"
)

func main() {
	if err := mainE(); err != nil {
		os.Exit(1)
	}
}

func mainE() error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	root := NewRootCmd(logger)
	if err := root.ExecuteContext(ctx); err != nil {
		logger.Info("Failure", "err", err)
		os.Stderr.Sync()
		return err
	}

	return nil
}

func NewRootCmd(log *slog.Logger) *cobra.Command {
	rootCmd := &cobra.Command{
		Use: "gordian-stress SUBCOMMAND",

		CompletionOptions: cobra.CompletionOptions{HiddenDefaultCmd: true},

		Long: `gordian-stress is used for orchestration of fine-grained local gordian test networks.

Orchestration as in: the user provides a single configuration file describing the network,
and the tooling ensures all the described participants are started as independent processes.

Fine-grained as in: fine control of the network participants' configuration.
`,
	}

	rootCmd.AddCommand(
		newSeedCmd(log),
		newSeedAddrsCmd(log),
	)

	return rootCmd
}

func newSeedCmd(log *slog.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use: "seed [PATH_TO_SOCKET_FILE=/var/run/gstress.$PID.sock]",

		Short: "Run a \"seed node\" that provides a central location for node discovery",

		Long: `seed runs a "seed node" that acts as a central location for:
- network participants to discover each other
- operator to discover dynamic addresses of each node
- operator to send various signals, such as a watchdog termination, to each node
`,

		Args: cobra.RangeArgs(0, 1),

		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			h, err := tmlibp2p.NewHost(
				ctx,
				tmlibp2p.HostOptions{
					Options: []libp2p.Option{
						// Since unspecified, use a dynamic identity and a random listening address.
						// Specify only tcp for the protocol, just for simplicity in stack traces.
						libp2p.ListenAddrStrings("/ip4/0.0.0.0/tcp/0"),

						// Unsure if this is something we always want.
						// Can be controlled by a flag later if undesirable by default.
						libp2p.ForceReachabilityPublic(),
					},
				},
			)
			if err != nil {
				return fmt.Errorf("failed to create libp2p host: %w", err)
			}

			host := h.Libp2pHost()

			// We currently rely on the libp2p DHT for peer discovery,
			// so the seed node needs to run the DHT as well.
			if _, err := dht.New(ctx, host, dht.ProtocolPrefix("/gordian")); err != nil {
				return fmt.Errorf("failed to create DHT peer for seed: %w", err)
			}

			var socketPath string
			if len(args) == 0 {
				// Unix sockets don't work on Windows anyway (right?)
				// so just directly make the path with forward slashes.
				socketPath = fmt.Sprintf("/var/run/gstress.%d.sock", os.Getpid())
			} else {
				socketPath = args[0]
			}

			hostInfo := libp2phost.InfoFromHost(host)
			p2pAddrs, err := libp2ppeer.AddrInfoToP2pAddrs(hostInfo)
			if err != nil {
				return fmt.Errorf("failed to get host p2p addrs: %w", err)
			}

			var hostAddrs []string
			for _, a := range p2pAddrs {
				hostAddrs = append(hostAddrs, a.String())
			}
			dHost, err := gstress.NewBootstrapHost(ctx, log.With("sys", "bootstrap"), socketPath, hostAddrs)
			if err != nil {
				return fmt.Errorf("failed to initialize discovery host: %w", err)
			}
			defer dHost.Wait()

			log.Info("Seed host ready", "id", host.ID().String(), "addrs", hostAddrs)

			<-ctx.Done()
			log.Info("Received ^c")

			return nil
		},
	}

	return cmd
}

func newSeedAddrsCmd(log *slog.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use: "seed-addrs PATH_TO_SOCKET_FILE",

		Short: "Read the libp2p seed addresses from the seed node's Unix socket",

		Args: cobra.ExactArgs(1),

		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := gstress.NewBootstrapClient(log, args[0])
			if err != nil {
				return fmt.Errorf("failed to create bootstrap client: %w", err)
			}
			defer c.Stop()

			addrs, err := c.SeedAddrs()
			if err != nil {
				return fmt.Errorf("failed to retrieve seed addresses: %w", err)
			}

			for _, addr := range addrs {
				// Logs go to stderr, but the read addresses go to stdout.
				fmt.Println(addr)
			}

			return nil
		},
	}

	return cmd
}
