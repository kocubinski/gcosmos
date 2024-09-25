package tmmemstore_test

import (
	"testing"

	"github.com/rollchains/gordian/tm/tmstore"
	"github.com/rollchains/gordian/tm/tmstore/tmmemstore"
	"github.com/rollchains/gordian/tm/tmstore/tmstoretest"
)

func TestMemFinalizationStore(t *testing.T) {
	t.Parallel()

	tmstoretest.TestFinalizationStoreCompliance(t, func(func(func())) (tmstore.FinalizationStore, error) {
		return tmmemstore.NewFinalizationStore(), nil
	})
}
