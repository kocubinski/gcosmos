package tmmemstore_test

import (
	"testing"

	"github.com/rollchains/gordian/tm/tmstore"
	"github.com/rollchains/gordian/tm/tmstore/tmmemstore"
	"github.com/rollchains/gordian/tm/tmstore/tmstoretest"
)

func TestMemRoundStore(t *testing.T) {
	t.Parallel()

	tmstoretest.TestRoundStoreCompliance(t, func(func(func())) (tmstore.RoundStore, error) {
		return tmmemstore.NewRoundStore(), nil
	})
}
