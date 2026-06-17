// Copyright 2025 The etcd Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package discover_etcd_initial_cluster

import (
	"errors"
	"path/filepath"

	"go.etcd.io/etcd/server/v3/etcdserver/api/snap"
	"go.etcd.io/etcd/server/v3/storage/datadir"

	pb "go.etcd.io/etcd/api/v3/etcdserverpb"
	"go.etcd.io/etcd/client/pkg/v3/types"
	"go.etcd.io/etcd/pkg/v3/pbutil"
	"go.etcd.io/etcd/server/v3/storage/wal"
	"go.etcd.io/etcd/server/v3/storage/wal/walpb"

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
