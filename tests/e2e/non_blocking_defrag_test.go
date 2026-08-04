package e2e

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"go.etcd.io/etcd/tests/v3/framework/config"
	"go.etcd.io/etcd/tests/v3/framework/e2e"
)

// TestNonBlockingDefragNoSpace verifies that a failed non-blocking defrag
// does not corrupt data and the server continues to operate.
func TestNonBlockingDefragNoSpace(t *testing.T) {
	tests := []struct {
		name      string
		failpoint string
		err       string
	}{
		{
			name:      "defragOpenFileError",
			failpoint: "defragOpenFileError",
			err:       "no space",
		},
		{
			name:      "defragdbFail",
			failpoint: "defragdbFail",
			err:       "some random error",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e2e.BeforeTest(t)

			clus, err := e2e.NewEtcdProcessCluster(context.TODO(), t,
				e2e.WithClusterSize(1),
				e2e.WithGoFailEnabled(true),
				e2e.WithServerFeatureGate("NonBlockingDefrag", true),
			)
			require.NoError(t, err)
			t.Cleanup(func() { clus.Stop() })

			member := clus.Procs[0]

			// Write data before defrag
			for i := range 10 {
				require.NoError(t, member.Etcdctl().Put(context.Background(), fmt.Sprintf("key_%d", i), "value", config.PutOptions{}))
			}

			// Trigger a failed defrag
			require.NoError(t, member.Failpoints().SetupHTTP(context.Background(), tc.failpoint, fmt.Sprintf(`return("%s")`, tc.err)))
			require.ErrorContains(t, member.Etcdctl().Defragment(context.Background(), config.DefragOption{Timeout: time.Minute}), tc.err)

			// Verify server is still functional after failed defrag
			require.NoError(t, member.Etcdctl().Put(context.Background(), "after_defrag", "ok", config.PutOptions{}))
			value, err := member.Etcdctl().Get(context.Background(), "after_defrag", config.GetOptions{})
			require.NoError(t, err)
			require.Len(t, value.Kvs, 1)
			require.Equal(t, "ok", string(value.Kvs[0].Value))

			// Verify pre-defrag data survived
			for i := range 10 {
				value, err := member.Etcdctl().Get(context.Background(), fmt.Sprintf("key_%d", i), config.GetOptions{})
				require.NoError(t, err)
				require.Lenf(t, value.Kvs, 1, "key_%d should exist after failed defrag", i)
				require.Equalf(t, "value", string(value.Kvs[0].Value), "key_%d should have correct value after failed defrag", i)
			}
		})
	}
}

// TestNonBlockingDefragSuccess verifies that a successful non-blocking
// defrag preserves all data written before and during the defrag.
func TestNonBlockingDefragSuccess(t *testing.T) {
	e2e.BeforeTest(t)
	clus, err := e2e.NewEtcdProcessCluster(context.TODO(), t,
		e2e.WithClusterSize(1),
		e2e.WithServerFeatureGate("NonBlockingDefrag", true),
	)
	require.NoError(t, err)
	t.Cleanup(func() { clus.Stop() })

	member := clus.Procs[0]

	// Write data before defrag
	for i := range 100 {
		require.NoError(t, member.Etcdctl().Put(context.Background(), fmt.Sprintf("key_%04d", i), "value", config.PutOptions{}))
	}

	// Delete half to make defrag meaningful
	for i := range 50 {
		_, err := member.Etcdctl().Delete(context.Background(), fmt.Sprintf("key_%04d", i), config.DeleteOptions{})
		require.NoError(t, err)
	}

	// Defrag should succeed
	require.NoError(t, member.Etcdctl().Defragment(context.Background(), config.DefragOption{Timeout: time.Minute}))

	// Verify surviving keys
	for i := 50; i < 100; i++ {
		value, err := member.Etcdctl().Get(context.Background(), fmt.Sprintf("key_%04d", i), config.GetOptions{})
		require.NoError(t, err)
		require.Lenf(t, value.Kvs, 1, "key_%04d should survive defrag", i)
	}

	// Verify deleted keys are gone
	for i := 0; i < 50; i++ {
		value, err := member.Etcdctl().Get(context.Background(), fmt.Sprintf("key_%04d", i), config.GetOptions{})
		require.NoError(t, err)
		require.Emptyf(t, value.Kvs, "key_%04d should be deleted after defrag", i)
	}

	// Write new data after defrag
	require.NoError(t, member.Etcdctl().Put(context.Background(), "post_defrag", "ok", config.PutOptions{}))
	value, err := member.Etcdctl().Get(context.Background(), "post_defrag", config.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, "ok", string(value.Kvs[0].Value))
}
