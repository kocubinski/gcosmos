package tmmemstore_test

import (
	"testing"

	"github.com/rollchains/gordian/tm/tmstore"
	"github.com/rollchains/gordian/tm/tmstore/tmmemstore"
	"github.com/rollchains/gordian/tm/tmstore/tmstoretest"
)

func TestMemCommittedHeaderStore(t *testing.T) {
	t.Parallel()

	tmstoretest.TestCommittedHeaderStoreCompliance(t, func(func(func())) (tmstore.CommittedHeaderStore, error) {
		return tmmemstore.NewCommittedHeaderStore(), nil
	})
}
