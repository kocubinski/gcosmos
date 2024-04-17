package gcrypto_test

import (
	"testing"

	"github.com/rollchains/gordian/gcrypto"
	"github.com/rollchains/gordian/gcrypto/gcryptotest"
)

func TestSimpleCommonMessageSignatureProof(t *testing.T) {
	gcryptotest.TestCommonMessageSignatureProofCompliance_Ed25519(
		t,
		gcrypto.SimpleCommonMessageSignatureProofScheme,
	)
}
