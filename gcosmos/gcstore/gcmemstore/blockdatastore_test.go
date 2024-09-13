package gcmemstore_test

import (
	"testing"

	"github.com/rollchains/gordian/gcosmos/gcstore"
	"github.com/rollchains/gordian/gcosmos/gcstore/gcmemstore"
	"github.com/rollchains/gordian/gcosmos/gcstore/gcstoretest"
)

func TestBlockDataStoreCompliance(t *testing.T) {
	t.Parallel()

	gcstoretest.TestBlockDataStoreCompliance(t, func() gcstore.BlockDataStore {
		return gcmemstore.NewBlockDataStore()
	})
}
