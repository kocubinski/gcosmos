package gstress_test

import (
	"context"
	"log/slog"
	"net/textproto"
	"path/filepath"
	"testing"

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
	defer c.Stop()

	addrs, err := c.SeedAddrs()
	require.NoError(t, err)

	require.Equal(t, seedAddrs, addrs)
}

type fixture struct {
	Log *slog.Logger

	SocketPath string

	Host *gstress.BootstrapHost
}

func newFixture(ctx context.Context, t *testing.T, cfg fixtureConfig) *fixture {
	t.Helper()

	// Put the socket file in a temporary directory so we don't have to manage naming conflicts.
	dir := t.TempDir()
	socketPath := filepath.Join(dir, "test.sock")

	log := gtest.NewLogger(t)

	h, err := gstress.NewBootstrapHost(ctx, log.With("sys", "bootstraphost"), socketPath, cfg.SeedAddrs)
	if err != nil {
		t.Fatalf("failed to create bootstrap host: %v", err)
		return nil
	}

	t.Cleanup(h.Wait)

	return &fixture{
		Log:        log,
		SocketPath: socketPath,
		Host:       h,
	}
}

func (f *fixture) NewRawClientConn() *textproto.Conn {
	conn, err := textproto.Dial("unix", f.SocketPath)
	if err != nil {
		panic(err)
	}

	return conn
}

func (f *fixture) NewClient() *gstress.BootstrapClient {
	c, err := gstress.NewBootstrapClient(f.Log.With("type", "bootstrapclient"), f.SocketPath)
	if err != nil {
		panic(err)
	}

	return c
}

type fixtureConfig struct {
	SeedAddrs []string
}
