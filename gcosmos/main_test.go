package main_test

import (
	"bytes"
	"context"
	"io"
	"testing"

	simdcmd "cosmossdk.io/simapp/v2/simdv2/cmd"
	svrcmd "github.com/cosmos/cosmos-sdk/server/cmd"
	"github.com/rollchains/gordian/gcosmos/internal/gci"
	"github.com/stretchr/testify/require"
)

func TestRootCmd(t *testing.T) {
	t.Parallel()

	e := NewRootCmd(t)

	res := e.Run("init", "defaultmoniker")

	res.NoError(t)
}

func NewRootCmd(
	t *testing.T,
) CmdEnv {
	t.Helper()

	return CmdEnv{homeDir: t.TempDir()}
}

type CmdEnv struct {
	homeDir string
}

func (e CmdEnv) Run(args ...string) RunResult {
	return e.RunWithInput(nil, args...)
}

func (e CmdEnv) RunWithInput(in io.Reader, args ...string) RunResult {
	cmd := simdcmd.NewRootCmd()
	cmd.AddCommand(gci.StartGordianCommand())

	// Just add the home flag directly instead of
	// relying on the comet CLI integration in the SDK.
	// Might be brittle, but should also be a little simpler.
	cmd.PersistentFlags().StringP("home", "", e.homeDir, "default test dir home, do not change")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ctx = svrcmd.CreateExecuteContext(ctx)

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
