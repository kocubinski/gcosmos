package gstress_test

import (
	"context"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rollchains/gordian/cmd/gordian-stress/internal/gstress"
	"github.com/rollchains/gordian/internal/gtest"
	"github.com/rollchains/gordian/tm/tmconsensus/tmconsensustest"
	"github.com/stretchr/testify/require"
)

func TestBootstrap_seedAddrs(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	seedAddrs := []string{"a", "b", "c"}

	bfx := newFixture(ctx, t, fixtureConfig{SeedAddrs: seedAddrs})

	c := bfx.NewClient()

	addrs, err := c.SeedAddrs()
	require.NoError(t, err)

	require.Equal(t, seedAddrs, addrs)
}

func TestBootstrap_chainID(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	beforeSec := time.Now().Unix()
	bfx := newFixture(ctx, t, fixtureConfig{SeedAddrs: []string{"a"}})
	afterSec := time.Now().Unix()

	c := bfx.NewClient()

	// Confirm the default ID,
	// which should be formatted as "gstress%d" % time.Now().Unix().
	chainID, err := c.ChainID()
	require.NoError(t, err)

	sec, err := strconv.ParseInt(strings.TrimPrefix(chainID, "gstress"), 10, 64)
	require.NoError(t, err)

	require.LessOrEqual(t, beforeSec, sec)
	require.GreaterOrEqual(t, afterSec, sec)

	// Change the chain ID.
	require.NoError(t, c.SetChainID("foo"))

	chainID, err = c.ChainID()
	require.NoError(t, err)
	require.Equal(t, "foo", chainID)
}

func TestBootstrap_app(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bfx := newFixture(ctx, t, fixtureConfig{SeedAddrs: []string{"a"}})

	c := bfx.NewClient()

	// The default app is echo.
	app, err := c.App()
	require.NoError(t, err)
	require.Equal(t, "echo", app)

	// Setting the app to anything else currently fails,
	// because we don't yet support any other built-in app.
	// Change the chain ID.
	require.Error(t, c.SetApp("foo"))

	// Confirm it didn't change.
	app, err = c.App()
	require.NoError(t, err)
	require.Equal(t, "echo", app)
}

func TestBootstrap_registerValidator(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bfx := newFixture(ctx, t, fixtureConfig{SeedAddrs: []string{"a"}})

	c := bfx.NewClient()

	// Registering a validator once succeeds.
	v := tmconsensustest.DeterministicValidatorsEd25519(1)[0].CVal
	require.NoError(t, c.RegisterValidator(v))

	// Attempting again will cause a panic on one of the bootstrap host goroutines,
	// so we aren't going to do that in test.

	got, err := c.Validators()
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, v, got[0])
}

type fixture struct {
	Log *slog.Logger

	Host *gstress.BootstrapHost

	serverSocketPath string
}

func newFixture(ctx context.Context, t *testing.T, cfg fixtureConfig) *fixture {
	t.Helper()

	// Normally we would use t.TempDir() to create a temporary diretory for the socket file.
	// However, that will use a longer-than-necessary path on macOS, e.g.
	// /var/folders/_m/h25_32y958gbgk67m97141400000gq/T/TestBootstrap_registerVali532062248/001/bootstrap.sock
	// And it turns out that there is a hardcoded limit to the file paths associated with sockets,
	// at least on macOS, but probably also on other Unix variants.
	// So, instead of the longer t.TempDir(), we just use os.CreateTemp()
	// with the default location of os.TempDir(), resulting in a full socket path like this:
	// /var/folders/_m/h25_32y958gbgk67m97141400000gq/T/510191397.sock
	//
	// We could be a little more considerate by injecting a specific directory for these tests,
	// but if that is strictly necessary, the user can set the TEMPDIR environment variable
	// to override the default location where the temporary socket files go.
	f, err := os.CreateTemp("", "*.sock")
	if err != nil {
		panic(err)
	}

	serverSocketPath := f.Name()

	if err := f.Close(); err != nil {
		panic(err)
	}
	if err := os.Remove(f.Name()); err != nil {
		panic(err)
	}

	log := gtest.NewLogger(t)

	h, err := gstress.NewBootstrapHost(ctx, log.With("sys", "bootstraphost"), serverSocketPath, cfg.SeedAddrs)
	if err != nil {
		t.Fatalf("failed to create bootstrap host: %v", err)
		return nil
	}

	t.Cleanup(h.Wait)

	return &fixture{
		Log:  log,
		Host: h,

		serverSocketPath: serverSocketPath,
	}
}

func (f *fixture) NewClient() *gstress.BootstrapClient {
	c, err := gstress.NewBootstrapClient(f.Log.With("type", "bootstrapclient"), f.serverSocketPath)
	if err != nil {
		panic(err)
	}

	return c
}

type fixtureConfig struct {
	SeedAddrs []string
}
