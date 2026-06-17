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

package revbump

import (
	"go.uber.org/zap"

	"go.etcd.io/etcd/server/v3/storage/backend"
	"go.etcd.io/etcd/server/v3/storage/mvcc"
	"go.etcd.io/etcd/server/v3/storage/schema"
)

func UnsafeModifyLastRevision(lg *zap.Logger, bumpAmount uint64, be backend.Backend) error {
	defer be.ForceCommit()

	tx := be.BatchTx()
	tx.LockOutsideApply()
	defer tx.Unlock()

	latest, err := unsafeGetLatestRevision(tx)
	if err != nil {
		return err
	}

	latest = unsafeBumpRevision(lg, tx, latest, int64(bumpAmount))
	unsafeMarkRevisionCompacted(lg, tx, latest)
	return nil
}

func unsafeBumpRevision(lg *zap.Logger, tx backend.BatchTx, latest revision, amount int64) revision {
	lg.Info(
		"bumping latest revision",
		zap.Int64("latest-revision", latest.main),
		zap.Int64("bump-amount", amount),
		zap.Int64("new-latest-revision", latest.main+amount),
	)

	latest.main += amount
	latest.sub = 0
	k := make([]byte, revBytesLen)
	revToBytes(k, latest)
	tx.UnsafePut(schema.Key, k, []byte{})

	return latest
}

func unsafeMarkRevisionCompacted(lg *zap.Logger, tx backend.BatchTx, latest revision) {
	lg.Info(
		"marking revision compacted",
		zap.Int64("revision", latest.main),
	)

	mvcc.UnsafeSetScheduledCompact(tx, latest.main)
}

func unsafeGetLatestRevision(tx backend.BatchTx) (revision, error) {
	var latest revision
	err := tx.UnsafeForEach(schema.Key, func(k, _ []byte) (err error) {
		rev := bytesToRev(k)

		if rev.GreaterThan(latest) {
			latest = rev
		}

		return nil
	})
	return latest, err
}
