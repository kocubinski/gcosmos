package tmmemstore_test

import (
	"testing"

	"github.com/rollchains/gordian/tm/tmstore"
	"github.com/rollchains/gordian/tm/tmstore/tmmemstore"
	"github.com/rollchains/gordian/tm/tmstore/tmstoretest"
)

func TestMemHeaderStore(t *testing.T) {
	t.Parallel()

	tmstoretest.TestHeaderStoreCompliance(t, func(func(func())) (tmstore.HeaderStore, error) {
		return tmmemstore.NewHeaderStore(), nil
	})
}
