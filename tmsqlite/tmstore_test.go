package tmsqlite_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/rollchains/gordian/gcrypto"
	"github.com/rollchains/gordian/tm/tmconsensus/tmconsensustest"
	"github.com/rollchains/gordian/tm/tmstore"
	"github.com/rollchains/gordian/tm/tmstore/tmstoretest"
	"github.com/rollchains/gordian/tmsqlite"
	"github.com/stretchr/testify/require"
)

var reg gcrypto.Registry

func init() {
	gcrypto.RegisterEd25519(&reg)
}

func TestNew(t *testing.T) {
	t.Parallel()

	// Just create the database and close it successfully.
	s, err := tmsqlite.NewTMStore(context.Background(), ":memory:", tmconsensustest.SimpleHashScheme{}, &reg)
	require.NoError(t, err)
	require.NotNil(t, s)

	// Helpful output in the simplest test, if there is uncertainty which type was built.
	t.Logf("Tests are for build type %s", s.BuildType)

	require.NoError(t, s.Close())
}

func TestMigrate(t *testing.T) {
	t.Run("empty database", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "db.sqlite")
		s1, err := tmsqlite.NewTMStore(context.Background(), path, tmconsensustest.SimpleHashScheme{}, &reg)
		require.NoError(t, err)
		require.NotNil(t, s1)
		require.NoError(t, s1.Close())

		s2, err:= tmsqlite.NewTMStore(context.Background(), path, tmconsensustest.SimpleHashScheme{}, &reg)
		require.NoError(t, err)
		require.NotNil(t, s2)
		require.NoError(t, s2.Close())
	})
}

func TestMirrorStoreCompliance(t *testing.T) {
	t.Parallel()

	tmstoretest.TestMirrorStoreCompliance(t, func(cleanup func(func())) (tmstore.MirrorStore, error) {
		s, err := tmsqlite.NewTMStore(context.Background(), ":memory:", tmconsensustest.SimpleHashScheme{}, &reg)
		if err != nil {
			return nil, err
		}
		cleanup(func() {
			require.NoError(t, s.Close())
		})
		return s, nil
	})
}

func TestValidatorStoreCompliance(t *testing.T) {
	t.Parallel()

	tmstoretest.TestValidatorStoreCompliance(t, func(cleanup func(func())) (tmstore.ValidatorStore, error) {
		s, err := tmsqlite.NewTMStore(context.Background(), ":memory:", tmconsensustest.SimpleHashScheme{}, &reg)
		if err != nil {
			return nil, err
		}
		cleanup(func() {
			require.NoError(t, s.Close())
		})
		return s, nil
	})
}
