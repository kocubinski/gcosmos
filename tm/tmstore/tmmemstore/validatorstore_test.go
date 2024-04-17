package tmmemstore_test

import (
	"testing"

	"github.com/rollchains/gordian/tm/tmconsensus/tmconsensustest"
	"github.com/rollchains/gordian/tm/tmstore"
	"github.com/rollchains/gordian/tm/tmstore/tmmemstore"
	"github.com/rollchains/gordian/tm/tmstore/tmstoretest"
)

func TestMemValidatorStore(t *testing.T) {
	t.Parallel()

	tmstoretest.TestValidatorStoreCompliance(t, func(func(func())) (tmstore.ValidatorStore, error) {
		return tmmemstore.NewValidatorStore(tmconsensustest.SimpleHashScheme{}), nil
	})
}
