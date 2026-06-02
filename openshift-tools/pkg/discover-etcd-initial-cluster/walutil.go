package discover_etcd_initial_cluster

import (
	"errors"
	"go.etcd.io/etcd/server/v3/datadir"
	"go.etcd.io/etcd/server/v3/etcdserver/api/snap"
	"path/filepath"

	pb "go.etcd.io/etcd/api/v3/etcdserverpb"
	"go.etcd.io/etcd/client/pkg/v3/types"
	"go.etcd.io/etcd/pkg/v3/pbutil"
	"go.etcd.io/etcd/server/v3/wal"
	"go.etcd.io/etcd/server/v3/wal/walpb"

	"go.uber.org/zap"
)

func readClusterIdFromWAL(lg *zap.Logger, dataDir string) (cid types.ID, err error) {
	walDir := datadir.ToWalDir(dataDir)
	snapDir := filepath.Join(datadir.ToMemberDir(dataDir), "snap")

	// Find a snapshot to start/restart a raft node
	ss := snap.New(lg, snapDir)

	var walSnaps []walpb.Snapshot
	walSnaps, err = wal.ValidSnapshotEntries(lg, walDir)
	if err != nil {
		return 0, err
	}

	snapshot, err := ss.LoadNewestAvailable(walSnaps)
	if err != nil && !errors.Is(err, snap.ErrNoSnapshot) {
		return 0, err
	}

	var walSnap walpb.Snapshot
	if snapshot != nil {
		walSnap.Index, walSnap.Term = snapshot.Metadata.Index, snapshot.Metadata.Term
	}

	w, err := wal.Open(lg, walDir, walSnap)
	if err != nil {
		return 0, err
	}

	defer func() {
		err = errors.Join(err, w.Close())
	}()

	walMeta, _, _, err := w.ReadAll()
	if err != nil {
		return 0, err
	}

	var metadata pb.Metadata
	pbutil.MustUnmarshal(&metadata, walMeta)
	return types.ID(metadata.ClusterID), nil
}
