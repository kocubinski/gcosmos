package tmlibp2ptest_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/rollchains/gordian/gcrypto"
	"github.com/rollchains/gordian/tm/tmcodec/tmjson"
	"github.com/rollchains/gordian/tm/tmp2p/tmlibp2p"
	"github.com/rollchains/gordian/tm/tmp2p/tmlibp2p/tmlibp2ptest"
	"github.com/rollchains/gordian/tm/tmp2p/tmp2ptest"
)

func TestLibp2pNetwork_Compliance(t *testing.T) {
	tmp2ptest.TestNetworkCompliance(
		t,
		func(ctx context.Context, log *slog.Logger) (tmp2ptest.Network, error) {
			reg := new(gcrypto.Registry)
			gcrypto.RegisterEd25519(reg)
			codec := tmjson.MarshalCodec{
				CryptoRegistry: reg,
			}
			n, err := tmlibp2ptest.NewNetwork(ctx, log, codec)
			if err != nil {
				return nil, err
			}
			return &tmp2ptest.GenericNetwork[*tmlibp2p.Connection]{
				Network: n,
			}, nil
		},
	)
}
