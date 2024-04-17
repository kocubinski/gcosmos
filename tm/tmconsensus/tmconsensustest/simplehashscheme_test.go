package tmconsensustest_test

import (
	"testing"

	"github.com/rollchains/gordian/gcrypto"
	"github.com/rollchains/gordian/tm/tmconsensus/tmconsensustest"
)

func TestSimpleHashScheme(t *testing.T) {
	tmconsensustest.TestHashSchemeCompliance(t, tmconsensustest.SimpleHashScheme{}, gcrypto.SimpleCommonMessageSignatureProofScheme, nil)
}
