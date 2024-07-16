package main_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"cosmossdk.io/core/transaction"
	serverv2 "cosmossdk.io/server/v2"
	simdcmd "cosmossdk.io/simapp/v2/simdv2/cmd"
	svrcmd "github.com/cosmos/cosmos-sdk/server/cmd"
	"github.com/rollchains/gordian/gcosmos/internal/gci"
	"github.com/rollchains/gordian/internal/gtest"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

// Cheap toggle at build time to quickly switch to running comet,
// for cases where the difference in behavior needs to be inspected.
const runCometInsteadOfGordian = false

func TestRootCmd(t *testing.T) {
	t.Parallel()

	e := NewRootCmd(t)

	e.Run("init", "defaultmoniker").NoError(t)
}

func TestRootCmd_startWithGordian(t *testing.T) {
	t.Parallel()

	chainID := t.Name()

	const nVals = 1
	rootCmds := make([]CmdEnv, nVals)
	keyAddresses := make([]string, nVals)

	for i := range nVals {
		// Each validator gets its own command environment
		// and therefore its own home directory.
		e := NewRootCmd(t)
		rootCmds[i] = e

		// Each validator needs its own initialized config and genesis.
		valName := fmt.Sprintf("val%d", i)
		e.Run("init", valName, "--chain-id", chainID).NoError(t)

		// Each validator needs its own key.
		res := e.Run("keys", "add", valName, "--output=json")
		res.NoError(t)

		// Collect the addresses from the JSON output.
		var keyAddOutput struct {
			Address string
		}
		require.NoError(t, json.Unmarshal(res.Stdout.Bytes(), &keyAddOutput))
		keyAddresses[i] = keyAddOutput.Address
	}

	// Add each key as a genesis account on the first validator's environment.
	for _, a := range keyAddresses {
		rootCmds[0].Run(
			"genesis", "add-genesis-account",
			a, "10000000000stake",
		).NoError(t)
	}

	// Add a gentx for every validator, to a temporary gentx directory.
	gentxDir := t.TempDir()
	for i, e := range rootCmds {
		vs := fmt.Sprintf("val%d", i)
		if i > 0 {
			// The first validator has every genesis account already,
			// but each validator needs its own address as a genesis account
			// in order to do a gentx.
			e.Run(
				"genesis", "add-genesis-account",
				vs, "10000000000stake",
			).NoError(t)
		}
		e.Run(
			"genesis", "gentx",
			"--chain-id", t.Name(),
			"--output-document", filepath.Join(gentxDir, vs+".gentx.json"),
			vs, "10000000000stake",
		).NoError(t)
	}

	// Collect gentxs on the first validator.
	rootCmds[0].Run(
		"genesis", "collect-gentxs", "--gentx-dir", gentxDir,
	).NoError(t)

	// The gRPC server defaults to listening on port 9090,
	// and the test will fail if the gRPC server cannot bind,
	// so just use an anonymous port.
	// https://github.com/cosmos/cosmos-sdk/issues/20819
	// tracks why we cannot set grpc.enable=false.
	rootCmds[0].Run("config", "set", "app", "grpc.address", "localhost:0", "--skip-validate").NoError(t)

	var startCmd = []string{"start"}
	var gHTTPAddrFile string
	if !runCometInsteadOfGordian {
		gHTTPAddrFile = filepath.Join(t.TempDir(), "http_addr.txt")
		// Then include the HTTP server flags.
		startCmd = append(
			startCmd,
			"--g-http-addr", "127.0.0.1:0",
			"--g-http-addr-file", gHTTPAddrFile,
		)
	}

	// TODO: we need a better way to synchronize the backgrounded start command.
	// This will run at least until the end of the test.
	// Just providing the context to a new RunC method may suffice here.
	go func() {
		_ = rootCmds[0].Run(startCmd...)
	}()

	// Get the HTTP address, which may require a few tries,
	// depending on how quickly the start command begins.
	if !runCometInsteadOfGordian {
		// Gratuitously long deadline.
		var httpAddr string
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			a, err := os.ReadFile(gHTTPAddrFile)
			if err != nil {
				// Swallow the error and delay.
				time.Sleep(25 * time.Millisecond)
				continue
			}
			if !bytes.HasSuffix(a, []byte("\n")) {
				// Very unlikely incomplete write/read.
				time.Sleep(25 * time.Millisecond)
				continue
			}

			httpAddr = strings.TrimSuffix(string(a), "\n")
			break
		}

		if httpAddr == "" {
			t.Fatalf("did not read http address from %s in time", gHTTPAddrFile)
		}

		u := "http://" + httpAddr + "/blocks/watermark"
		// TODO: we might need to delay until we get a non-error HTTP response.

		deadline = time.Now().Add(10 * time.Second)
		var maxHeight uint
		for time.Now().Before(deadline) {
			resp, err := http.Get(u)
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, resp.StatusCode)

			var m map[string]uint
			require.NoError(t, json.NewDecoder(resp.Body).Decode(&m))
			resp.Body.Close()

			maxHeight = m["VotingHeight"]
			if maxHeight < 3 {
				time.Sleep(100 * time.Millisecond)
				continue
			}

			// We got at least to height 3, so quit the loop.
			break
		}

		require.GreaterOrEqual(t, maxHeight, uint(3))
	}
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

	// Compile-time flag declared near top of this file.
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
