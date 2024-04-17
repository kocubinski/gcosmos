package tmmemstore_test

import (
	"testing"

	"github.com/rollchains/gordian/tm/tmstore"
	"github.com/rollchains/gordian/tm/tmstore/tmmemstore"
	"github.com/rollchains/gordian/tm/tmstore/tmstoretest"
)

func TestActionStore(t *testing.T) {
	t.Parallel()

	tmstoretest.TestActionStoreCompliance(t, func(func(func())) (tmstore.ActionStore, error) {
		return tmmemstore.NewActionStore(), nil
	})
}
