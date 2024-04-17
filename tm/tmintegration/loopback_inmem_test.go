package tmintegration_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/rollchains/gordian/tm/tmgossip"
	"github.com/rollchains/gordian/tm/tmintegration"
	"github.com/rollchains/gordian/tm/tmp2p"
	"github.com/rollchains/gordian/tm/tmp2p/tmp2ptest"
)

type LoopbackInmemFactory struct {
	e *tmintegration.Env

	tmintegration.InmemStoreFactory
	tmintegration.InmemSchemeFactory
}

func (f LoopbackInmemFactory) NewNetwork(ctx context.Context, log *slog.Logger) (tmp2ptest.Network, error) {
	n := tmp2ptest.NewLoopbackNetwork(ctx, log)

	return &tmp2ptest.GenericNetwork[*tmp2ptest.LoopbackConnection]{
		Network: n,
	}, nil
}

func (f LoopbackInmemFactory) NewGossipStrategy(ctx context.Context, idx int, conn tmp2p.Connection) (tmgossip.Strategy, error) {
	return tmgossip.NewChattyStrategy(ctx, f.e.RootLogger.With("sys", "chattygossip", "idx", idx), conn.ConsensusBroadcaster()), nil
}

func TestLoopbackInmem(t *testing.T) {
	t.Parallel()

	tmintegration.RunIntegrationTest(t, func(e *tmintegration.Env) tmintegration.Factory {
		return LoopbackInmemFactory{e: e}
	})
}
