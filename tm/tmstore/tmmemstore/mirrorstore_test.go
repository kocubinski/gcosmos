package tmmemstore_test

import (
	"testing"

	"github.com/rollchains/gordian/tm/tmstore"
	"github.com/rollchains/gordian/tm/tmstore/tmmemstore"
	"github.com/rollchains/gordian/tm/tmstore/tmstoretest"
)

func TestMemMirrorStore(t *testing.T) {
	t.Parallel()

	tmstoretest.TestMirrorStoreCompliance(t, func(func(func())) (tmstore.MirrorStore, error) {
		return tmmemstore.NewMirrorStore(), nil
	})
}
