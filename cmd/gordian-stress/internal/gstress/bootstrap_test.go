package gstress_test

import (
	"context"
	"log/slog"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rollchains/gordian/cmd/gordian-stress/internal/gstress"
	"github.com/rollchains/gordian/internal/gtest"
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

type fixture struct {
	Log *slog.Logger

	Host *gstress.BootstrapHost

	serverSocketPath string
}

func newFixture(ctx context.Context, t *testing.T, cfg fixtureConfig) *fixture {
	t.Helper()

	dir := t.TempDir()
	serverSocketPath := filepath.Join(dir, "bootstrap.sock")

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
