package main_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"cosmossdk.io/core/transaction"
	serverv2 "cosmossdk.io/server/v2"
	simdcmd "cosmossdk.io/simapp/v2/simdv2/cmd"
	svrcmd "github.com/cosmos/cosmos-sdk/server/cmd"
	"github.com/rollchains/gordian/gcosmos/internal/gci"
	"github.com/rollchains/gordian/internal/gtest"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestRootCmd(t *testing.T) {
	t.Parallel()

	e := NewRootCmd(t)

	e.Run("init", "defaultmoniker").NoError(t)
}

func TestRootCmd_checkGenesisValidators(t *testing.T) {
	t.Parallel()

	e := NewRootCmd(t)

	// It would be nice to be able to use deterministic keys for these tests,
	// but since the key subcommands use readers hanging off a "client context",
	// it is surprisingly difficult to intercept that value at the right spot.
	// Unfortunately, we can't simply set cmd.Input for this.
	const nVals = 4
	for i := range nVals {
		// Each of these validators needs its own home directory
		e.Run("init", "testmoniker", "--chain-id", t.Name()).NoError(t)
		// ...and we create this key in keychain for each of the validators
		// if we have home directory isolation then we can use the same key name
		// might also want to use the flag "--keyring-backend=test" to avoid using the OS keyring
		e.Run("keys", "add", fmt.Sprintf("val%d", i)).NoError(t)
	}

	for i := range nVals {
		// We need to get the address (cosmos1....) for each validator from the keyring in their
		// home directory and then add-genesis-account for each to a single genesis file (I normally pick the first one)
		// something like
		// e.Run(
		// 	"genesis", "add-genesis-account",
		// 	e.Run("keys", "show", "val"), "100stake", "--home", "node0home"
		// )
		e.Run(
			"genesis", "add-genesis-account",
			fmt.Sprintf("val%d", i), "100stake",
		).NoError(t)
	}

	// Move all the gentx files into node0home/config/gentx
	gentxDir := t.TempDir()

	for i := range nVals {
		vs := fmt.Sprintf("val%d", i)
		e.Run(
			"genesis", "gentx",
			"--chain-id", t.Name(),
			"--output-document", filepath.Join(gentxDir, vs+".gentx.json"),
			vs, "100stake",
		).NoError(t)
	}

	// then once all the gentx files are in the node0home gentx directory we can run collect-gentxs
	e.Run(
		"genesis", "collect-gentxs", "--gentx-dir", gentxDir,
	).NoError(t)

	// after this we need to distribute the genesis file to all the other nodes replacing the genesis file in their home directory

	// Can add a test here that checks the sha256 of the genesis file to make sure it is the same on all nodes

	// Now we can start each node one by one, and they should all connect to each other and form a network

	// The gRPC server defaults to listening on port 9090,
	// and the test will fail if the gRPC server cannot bind,
	// so just use an anonymous port.
	// https://github.com/cosmos/cosmos-sdk/issues/20819
	// tracks why we cannot set grpc-server.enable=false.
	e.Run("config", "set", "app", "grpc-server.address", "localhost:0", "--skip-validate").NoError(t)

	res := e.Run("start")
	res.NoError(t)

	pubkeys := 0
	for _, line := range strings.Split(res.Stdout.String(), "\n") {
		if strings.Contains(line, "pubkey") {
			pubkeys++
		}
	}
	require.Equal(t, nVals, pubkeys)
}

func NewRootCmd(
	t *testing.T,
) CmdEnv {
	t.Helper()

	return CmdEnv{
		log:     gtest.NewLogger(t),
		homeDir: t.TempDir(),
	}
}

type CmdEnv struct {
	log     *slog.Logger
	homeDir string
}

func (e CmdEnv) Run(args ...string) RunResult {
	return e.RunWithInput(nil, args...)
}

func (e CmdEnv) RunWithInput(in io.Reader, args ...string) RunResult {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var cmd *cobra.Command

	// Cheap toggle so I don't have to look up the command function every time
	// I need to check the delta between the two.
	const runCometInsteadOfGordian = false
	if runCometInsteadOfGordian {
		cmd = simdcmd.NewRootCmd[serverv2.AppI[transaction.Tx], transaction.Tx]()
	} else {
		cmd = gci.NewSimdRootCmdWithGordian(ctx, e.log)
	}

	// Just add the home flag directly instead of
	// relying on the comet CLI integration in the SDK.
	// Might be brittle, but should also be a little simpler.
	cmd.PersistentFlags().StringP("home", "", e.homeDir, "default test dir home, do not change")

	ctx = svrcmd.CreateExecuteContext(ctx)

	args = append(
		slices.Clone(args),
		// Putting --home before the args would probably work,
		// but put --home at the end to be a little more sure
		// that it won't get ignored due to being parsed before the subcommand name.
		"--home", e.homeDir,
	)
	cmd.SetArgs(args)

	var res RunResult
	cmd.SetOut(&res.Stdout)
	cmd.SetErr(&res.Stderr)
	cmd.SetIn(in)

	res.Err = cmd.ExecuteContext(ctx)

	return res
}

type RunResult struct {
	Stdout, Stderr bytes.Buffer
	Err            error
}

func (r RunResult) NoError(t *testing.T) {
	t.Helper()

	require.NoErrorf(t, r.Err, "OUT: %s\n\nERR: %s", r.Stdout.String(), r.Stderr.String())
}
