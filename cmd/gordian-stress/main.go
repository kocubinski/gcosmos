package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/rpc"
	"os"
	"os/signal"
	"strconv"

	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	libp2phost "github.com/libp2p/go-libp2p/core/host"
	libp2ppeer "github.com/libp2p/go-libp2p/core/peer"
	"github.com/rollchains/gordian/cmd/gordian-stress/internal/gstress"
	"github.com/rollchains/gordian/cmd/internal/gcmd"
	"github.com/rollchains/gordian/tm/tmconsensus"
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
		logger.Error("Failure", "err", err)
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
		newValidatorCmd(log),
		newRegisterValidatorCmd(log),
		newStartCmd(log),
	)

	return rootCmd
}

func newSeedCmd(log *slog.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use: "seed [PATH_TO_SOCKET_FILE=/var/run/gstress.$PID.sock]",

		Short: "Run a \"seed node\" that provides a central location for node discovery and coordination",

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
			bh, err := gstress.NewBootstrapHost(ctx, log.With("sys", "bootstrap"), socketPath, hostAddrs)
			if err != nil {
				return fmt.Errorf("failed to initialize discovery host: %w", err)
			}
			defer bh.Wait()

			_ = gstress.NewSeedService(log.With("svc", "seed"), host, bh)

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

			addrs, err := c.SeedAddrs()
			if err != nil {
				return fmt.Errorf("failed to retrieve seed addresses: %w", err)
			}

			for _, addr := range addrs {
				// Logs go to stderr, but the read addresses go to stdout.
				fmt.Fprintln(cmd.OutOrStdout(), addr)
			}

			return nil
		},
	}

	return cmd
}

func newValidatorCmd(log *slog.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use: "validator BOOTSTRAP_SOCKET_FILE INSECURE_PASSPHRASE",

		Short: "Run a validator whose key is derived from the given insecure passphrase",

		Args: cobra.ExactArgs(2),

		RunE: func(cmd *cobra.Command, args []string) error {
			socketPath := args[0]
			insecurePassphrase := args[1]

			c, err := gstress.NewBootstrapClient(log.With("sys", "bootstrapclient"), socketPath)
			if err != nil {
				return fmt.Errorf("failed to create bootstrap client (make sure you ran 'gordian-stress seed' first): %w", err)
			}

			// Need the seed addresses to connect to the seed RPC soon.
			seedAddrs, err := c.SeedAddrs()
			if err != nil {
				return fmt.Errorf("failed to retrieve seed addresses (make sure you ran 'gordian-stress seed' first): %w", err)
			}

			signer, err := gcmd.SignerFromInsecurePassphrase("gordian-stress|", insecurePassphrase)
			if err != nil {
				return fmt.Errorf("failed to build signer from insecure passphrase: %w", err)
			}

			// Log a warning if our key is not in the list.
			curVals, err := c.Validators()
			if err != nil {
				return fmt.Errorf("failed to get validators from seed: %w", err)
			}
			isRegistered := false
			for _, v := range curVals {
				if v.PubKey.Equal(signer.PubKey()) {
					isRegistered = true
					break
				}
			}
			if !isRegistered {
				log.Warn("This validator is not registered; if unintentional, run 'gordian-stress register-validator' to include this validator in genesis")
			}

			ctx := cmd.Context()

			netPrivKey, err := gcmd.Libp2pKeyFromInsecurePassphrase("gordian-stress:network|", insecurePassphrase)
			if err != nil {
				return fmt.Errorf("failed to generate libp2p network key: %w", err)
			}

			h, err := tmlibp2p.NewHost(
				ctx,
				tmlibp2p.HostOptions{
					Options: []libp2p.Option{
						libp2p.Identity(netPrivKey),

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

			defer func() {
				if err := h.Close(); err != nil {
					log.Warn("Error closing libp2p host", "err", err)
				}
			}()

			// Connect to the first possible seed address.
			var connectedID libp2ppeer.ID
			for _, sa := range seedAddrs {
				ai, err := libp2ppeer.AddrInfoFromString(sa)
				if err != nil {
					log.Warn("Failed to parse seed address", "addr", sa, "err", err)
					continue
				}

				if err := h.Libp2pHost().Connect(ctx, *ai); err != nil {
					log.Warn("Failed to connect to seed address", "addr", sa, "err", err)
					continue
				}

				connectedID = ai.ID
				break
			}
			if connectedID == "" {
				return fmt.Errorf("Failed to connect to any of %d seed address(es)", len(seedAddrs))
			}

			// Now that we are connected over libp2p, open the seed RPC.
			seedRPCStream, err := h.Libp2pHost().NewStream(
				ctx, connectedID, gstress.SeedServiceProtocolID,
			)
			if err != nil {
				return fmt.Errorf("failed to open seed stream: %w", err)
			}
			rpcClient := rpc.NewClient(seedRPCStream)

			log.Info("Awaiting genesis...")
			resp := new(gstress.RPCGenesisResponse)
			select {
			case <-ctx.Done():
				return context.Cause(ctx)

			case call := <-rpcClient.Go("SeedRPC.Genesis", gstress.RPCGenesisRequest{}, resp, nil).Done:
				if call.Error != nil {
					return fmt.Errorf("error during Genesis RPC: %w", err)
				}
				// Otherwise, resp has been populated.
			}

			log.Info("Got genesis data", "app", resp.App, "chain_id", resp.ChainID, "validators", resp.Validators)

			return nil
		},
	}

	return cmd
}

func newRegisterValidatorCmd(log *slog.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "register-validator PATH_TO_SOCKET_FILE INSECURE_PASSPHRASE VOTE_POWER",
		Aliases: []string{"rv"},

		Short: "Register a validator with the seed node",

		Args: cobra.ExactArgs(3),

		RunE: func(cmd *cobra.Command, args []string) error {
			socketPath := args[0]
			insecurePassphrase := args[1]

			pow, err := strconv.ParseUint(args[2], 10, 64)
			if err != nil {
				return fmt.Errorf("failed to parse vote power: %w", err)
			}

			signer, err := gcmd.SignerFromInsecurePassphrase("gordian-stress|", insecurePassphrase)
			if err != nil {
				return fmt.Errorf("failed to build signer from insecure passphrase: %w", err)
			}

			c, err := gstress.NewBootstrapClient(log, socketPath)
			if err != nil {
				return fmt.Errorf("failed to create bootstrap client: %w", err)
			}

			return c.RegisterValidator(tmconsensus.Validator{
				PubKey: signer.PubKey(),
				Power:  pow,
			})
		},
	}

	return cmd
}

func newStartCmd(log *slog.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use: "start PATH_TO_SOCKET_FILE",

		Short: "Tell the seed node to start the chain with all connected validators",

		Args: cobra.ExactArgs(1),

		RunE: func(cmd *cobra.Command, args []string) error {
			socketPath := args[0]

			c, err := gstress.NewBootstrapClient(log, socketPath)
			if err != nil {
				return fmt.Errorf("failed to create bootstrap client: %w", err)
			}

			return c.Start()
		},
	}

	return cmd
}
