package tmmemstore_test

import (
	"context"
	"testing"

	"github.com/rollchains/gordian/tm/tmstore"
	"github.com/rollchains/gordian/tm/tmstore/tmmemstore"
	"github.com/rollchains/gordian/tm/tmstore/tmstoretest"
)

func TestStateMachineStore(t *testing.T) {
	t.Parallel()

	tmstoretest.TestStateMachineStoreCompliance(t, func(ctx context.Context, _ func(func())) (tmstore.StateMachineStore, error) {
		return tmmemstore.NewStateMachineStore(), nil
	})
}
