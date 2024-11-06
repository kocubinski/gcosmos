package ggrpctest

import (
	"context"
	"net"
	"testing"

	"github.com/gordian-engine/gcosmos/gserver/internal/ggrpc"
	"github.com/gordian-engine/gcosmos/internal/copy/gtest"
	"github.com/gordian-engine/gcosmos/txstore/txmemstore"
	"github.com/gordian-engine/gordian/tm/tmstore/tmmemstore"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

// Fixture contains a gRPC server and client,
// ready to use for testing the gRPC services.
type Fixture struct {
	Server *ggrpc.GordianGRPC
	Client ggrpc.GordianGRPCClient

	// TODO: either pull up the rest of the necessary fields from GRPCServerConfig
	// or just outright expose GRPCServerConfig.
	FinalizationStore *tmmemstore.FinalizationStore
	MirrorStore       *tmmemstore.MirrorStore
	TxStore           *txmemstore.Store
}

// NewFixture returns a new fixture whose lifecycle is associated
// with the given context.
// It registers cleanup through t.Cleanup.
// The caller must be sure to defer context cancellation
// in order for the fixture to shut down properly.
func NewFixture(t *testing.T, ctx context.Context) Fixture {
	t.Helper()

	log := gtest.NewLogger(t)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = ln.Close()
	})

	fs := tmmemstore.NewFinalizationStore()
	ms := tmmemstore.NewMirrorStore()
	txs := txmemstore.NewStore(log)

	srv := ggrpc.NewGordianGRPCServer(
		ctx,
		log.With("sys", "server"),
		ggrpc.GRPCServerConfig{
			Listener: ln,

			FinalizationStore: fs,
			MirrorStore:       ms,
			TxStore:           txs,
		},
	)
	t.Cleanup(srv.Wait)

	gc, err := grpc.NewClient(
		ln.Addr().String(),
		grpc.WithInsecure(),
	)
	require.NoError(t, err)
	c := ggrpc.NewGordianGRPCClient(gc)

	return Fixture{
		Server: srv,
		Client: c,

		FinalizationStore: fs,
		MirrorStore:       ms,
		TxStore:           txs,
	}
}
