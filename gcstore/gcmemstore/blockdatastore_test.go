package gcmemstore_test

import (
	"testing"

	"github.com/gordian-engine/gcosmos/gcstore"
	"github.com/gordian-engine/gcosmos/gcstore/gcmemstore"
	"github.com/gordian-engine/gcosmos/gcstore/gcstoretest"
)

func TestBlockDataStoreCompliance(t *testing.T) {
	t.Parallel()

	gcstoretest.TestBlockDataStoreCompliance(t, func() gcstore.BlockDataStore {
		return gcmemstore.NewBlockDataStore()
	})
}
