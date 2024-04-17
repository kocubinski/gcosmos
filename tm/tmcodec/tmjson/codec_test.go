package tmjson_test

import (
	"testing"

	"github.com/rollchains/gordian/gcrypto"
	"github.com/rollchains/gordian/tm/tmcodec"
	"github.com/rollchains/gordian/tm/tmcodec/tmcodectest"
	"github.com/rollchains/gordian/tm/tmcodec/tmjson"
)

func TestMarshalCodec(t *testing.T) {
	tmcodectest.TestMarshalCodecCompliance(t, func() tmcodec.MarshalCodec {
		reg := new(gcrypto.Registry)
		gcrypto.RegisterEd25519(reg)
		return tmjson.MarshalCodec{
			CryptoRegistry: reg,
		}
	})
}
