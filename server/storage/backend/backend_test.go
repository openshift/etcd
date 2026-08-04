// Copyright 2015 The etcd Authors
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

package backend_test

import (
	"fmt"
	"os"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	bolt "go.etcd.io/bbolt"
	"go.etcd.io/etcd/server/v3/storage/backend"
	betesting "go.etcd.io/etcd/server/v3/storage/backend/testing"
	"go.etcd.io/etcd/server/v3/storage/schema"
)

// --- Defrag test helpers ---

// newDefragBackend creates a default backend with non-blocking defrag enabled.
func newDefragBackend(t testing.TB) backend.Backend {
	b, _ := betesting.NewDefaultTmpBackend(t)
	t.Cleanup(func() { betesting.Close(t, b) })
	backend.SetNonBlockingDefragForTest(b, true)
	return b
}

// populateKeys creates the bucket, writes count keys (0..count-1) using keyFmt, and commits.
func populateKeys(t testing.TB, b backend.Backend, bucket backend.Bucket, keyFmt string, value []byte, count int) {
	tx := b.BatchTx()
	tx.Lock()
	tx.UnsafeCreateBucket(bucket)
	for i := 0; i < count; i++ {
		tx.UnsafePut(bucket, []byte(fmt.Sprintf(keyFmt, i)), value)
	}
	tx.Unlock()
	b.ForceCommit()
}

// deleteKeyRange removes keys [start, end) using keyFmt from the bucket and commits.
func deleteKeyRange(t testing.TB, b backend.Backend, bucket backend.Bucket, keyFmt string, start, end int) {
	tx := b.BatchTx()
	tx.Lock()
	for i := start; i < end; i++ {
		tx.UnsafeDelete(bucket, []byte(fmt.Sprintf(keyFmt, i)))
	}
	tx.Unlock()
	b.ForceCommit()
}

// requireKeysExist checks that keys [start, end) exist in the bucket.
// The caller must hold the lock on tx.
func requireKeysExist(t testing.TB, tx backend.BatchTx, bucket backend.Bucket, keyFmt string, start, end int) {
	t.Helper()
	for i := start; i < end; i++ {
		key := fmt.Sprintf(keyFmt, i)
		keys, _ := tx.UnsafeRange(bucket, []byte(key), nil, 0)
		require.Lenf(t, keys, 1, "%s should exist", key)
	}
}

// requireKeysAbsent checks that keys [start, end) do NOT exist in the bucket.
// The caller must hold the lock on tx.
func requireKeysAbsent(t testing.TB, tx backend.BatchTx, bucket backend.Bucket, keyFmt string, start, end int) {
	t.Helper()
	for i := start; i < end; i++ {
		key := fmt.Sprintf(keyFmt, i)
		keys, _ := tx.UnsafeRange(bucket, []byte(key), nil, 0)
		assert.Emptyf(t, keys, "%s should not exist", key)
	}
}

// requireKeysHaveValue checks that keys [start, end) exist and have expectedVal.
// The caller must hold the lock on tx.
func requireKeysHaveValue(t testing.TB, tx backend.BatchTx, bucket backend.Bucket, keyFmt string, start, end int, expectedVal []byte) {
	t.Helper()
	for i := start; i < end; i++ {
		key := fmt.Sprintf(keyFmt, i)
		keys, vals := tx.UnsafeRange(bucket, []byte(key), nil, 0)
		require.Lenf(t, keys, 1, "%s should exist", key)
		assert.Equalf(t, expectedVal, vals[0], "%s has wrong value", key)
	}
}

// startConcurrentWriter starts a goroutine that continuously puts keys to the
// bucket until stop is closed. Returns a WaitGroup and an atomic counter.
func startConcurrentWriter(b backend.Backend, bucket backend.Bucket, keyFmt string, value []byte, stop <-chan struct{}) (*sync.WaitGroup, *atomic.Int32) {
	var wg sync.WaitGroup
	var count atomic.Int32
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			tx := b.BatchTx()
			tx.Lock()
			tx.UnsafePut(bucket, []byte(fmt.Sprintf(keyFmt, i)), value)
			tx.Unlock()
			count.Add(1)
		}
	}()
	return &wg, &count
}

func TestBackendClose(t *testing.T) {
	b, _ := betesting.NewTmpBackend(t, time.Hour, 10000)

	// check close could work
	done := make(chan struct{}, 1)
	go func() {
		err := b.Close()
		if err != nil {
			t.Errorf("close error = %v, want nil", err)
		}
		done <- struct{}{}
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Errorf("failed to close database in 10s")
	}
}

func TestBackendSnapshot(t *testing.T) {
	b, _ := betesting.NewTmpBackend(t, time.Hour, 10000)
	defer betesting.Close(t, b)

	tx := b.BatchTx()
	tx.Lock()
	tx.UnsafeCreateBucket(schema.Test)
	tx.UnsafePut(schema.Test, []byte("foo"), []byte("bar"))
	tx.Unlock()
	b.ForceCommit()

	// write snapshot to a new file
	f, err := os.CreateTemp(t.TempDir(), "etcd_backend_test")
	if err != nil {
		t.Fatal(err)
	}
	snap := b.Snapshot()
	defer func() { assert.NoError(t, snap.Close()) }()
	if _, err := snap.WriteTo(f); err != nil {
		t.Fatal(err)
	}
	require.NoError(t, f.Close())

	// bootstrap new backend from the snapshot
	bcfg := backend.DefaultBackendConfig(zaptest.NewLogger(t))
	bcfg.Path, bcfg.BatchInterval, bcfg.BatchLimit = f.Name(), time.Hour, 10000
	nb := backend.New(bcfg)
	defer betesting.Close(t, nb)

	newTx := nb.BatchTx()
	newTx.Lock()
	ks, _ := newTx.UnsafeRange(schema.Test, []byte("foo"), []byte("goo"), 0)
	if len(ks) != 1 {
		t.Errorf("len(kvs) = %d, want 1", len(ks))
	}
	newTx.Unlock()
}

func TestBackendBatchIntervalCommit(t *testing.T) {
	// start backend with super short batch interval so
	// we do not need to wait long before commit to happen.
	b, _ := betesting.NewTmpBackend(t, time.Nanosecond, 10000)
	defer betesting.Close(t, b)

	pc := backend.CommitsForTest(b)

	tx := b.BatchTx()
	tx.Lock()
	tx.UnsafeCreateBucket(schema.Test)
	tx.UnsafePut(schema.Test, []byte("foo"), []byte("bar"))
	tx.Unlock()

	for i := 0; i < 10; i++ {
		if backend.CommitsForTest(b) >= pc+1 {
			break
		}
		time.Sleep(time.Duration(i*100) * time.Millisecond)
	}

	// check whether put happens via db view
	assert.NoError(t, backend.DbFromBackendForTest(b).View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte("test"))
		if bucket == nil {
			t.Errorf("bucket test does not exit")
			return nil
		}
		v := bucket.Get([]byte("foo"))
		if v == nil {
			t.Errorf("foo key failed to written in backend")
		}
		return nil
	}))
}

func TestBackendDefrag(t *testing.T) {
	bcfg := backend.DefaultBackendConfig(zaptest.NewLogger(t))
	// Make sure we change BackendFreelistType
	// The goal is to verify that we restore config option after defrag.
	if bcfg.BackendFreelistType == bolt.FreelistMapType {
		bcfg.BackendFreelistType = bolt.FreelistArrayType
	} else {
		bcfg.BackendFreelistType = bolt.FreelistMapType
	}

	b, _ := betesting.NewTmpBackendFromCfg(t, bcfg)

	defer betesting.Close(t, b)

	tx := b.BatchTx()
	tx.Lock()
	tx.UnsafeCreateBucket(schema.Test)
	for i := 0; i < backend.DefragLimitForTest()+100; i++ {
		tx.UnsafePut(schema.Test, []byte(fmt.Sprintf("foo_%d", i)), []byte("bar"))
	}
	tx.Unlock()
	b.ForceCommit()

	// remove some keys to ensure the disk space will be reclaimed after defrag
	tx = b.BatchTx()
	tx.Lock()
	for i := 0; i < 50; i++ {
		tx.UnsafeDelete(schema.Test, []byte(fmt.Sprintf("foo_%d", i)))
	}
	tx.Unlock()
	b.ForceCommit()

	size := b.Size()

	// shrink and check hash
	oh, err := b.Hash(nil)
	if err != nil {
		t.Fatal(err)
	}

	err = b.Defrag()
	if err != nil {
		t.Fatal(err)
	}

	nh, err := b.Hash(nil)
	if err != nil {
		t.Fatal(err)
	}
	if oh != nh {
		t.Errorf("hash = %v, want %v", nh, oh)
	}

	nsize := b.Size()
	if nsize >= size {
		t.Errorf("new size = %v, want < %d", nsize, size)
	}
	db := backend.DbFromBackendForTest(b)
	if db.FreelistType != bcfg.BackendFreelistType {
		t.Errorf("db FreelistType = [%v], want [%v]", db.FreelistType, bcfg.BackendFreelistType)
	}

	// try put more keys after shrink.
	tx = b.BatchTx()
	tx.Lock()
	tx.UnsafeCreateBucket(schema.Test)
	tx.UnsafePut(schema.Test, []byte("more"), []byte("bar"))
	tx.Unlock()
	b.ForceCommit()
}

// TestBackendWriteback ensures writes are stored to the read txn on write txn unlock.
func TestBackendWriteback(t *testing.T) {
	b, _ := betesting.NewDefaultTmpBackend(t)
	defer betesting.Close(t, b)

	tx := b.BatchTx()
	tx.Lock()
	tx.UnsafeCreateBucket(schema.Key)
	tx.UnsafePut(schema.Key, []byte("abc"), []byte("bar"))
	tx.UnsafePut(schema.Key, []byte("def"), []byte("baz"))
	tx.UnsafePut(schema.Key, []byte("overwrite"), []byte("1"))
	tx.Unlock()

	// overwrites should be propagated too
	tx.Lock()
	tx.UnsafePut(schema.Key, []byte("overwrite"), []byte("2"))
	tx.Unlock()

	keys := []struct {
		key   []byte
		end   []byte
		limit int64

		wkey [][]byte
		wval [][]byte
	}{
		{
			key: []byte("abc"),
			end: nil,

			wkey: [][]byte{[]byte("abc")},
			wval: [][]byte{[]byte("bar")},
		},
		{
			key: []byte("abc"),
			end: []byte("def"),

			wkey: [][]byte{[]byte("abc")},
			wval: [][]byte{[]byte("bar")},
		},
		{
			key: []byte("abc"),
			end: []byte("deg"),

			wkey: [][]byte{[]byte("abc"), []byte("def")},
			wval: [][]byte{[]byte("bar"), []byte("baz")},
		},
		{
			key:   []byte("abc"),
			end:   []byte("\xff"),
			limit: 1,

			wkey: [][]byte{[]byte("abc")},
			wval: [][]byte{[]byte("bar")},
		},
		{
			key: []byte("abc"),
			end: []byte("\xff"),

			wkey: [][]byte{[]byte("abc"), []byte("def"), []byte("overwrite")},
			wval: [][]byte{[]byte("bar"), []byte("baz"), []byte("2")},
		},
	}
	rtx := b.ReadTx()
	for i, tt := range keys {
		func() {
			rtx.RLock()
			defer rtx.RUnlock()
			k, v := rtx.UnsafeRange(schema.Key, tt.key, tt.end, tt.limit)
			if !reflect.DeepEqual(tt.wkey, k) || !reflect.DeepEqual(tt.wval, v) {
				t.Errorf("#%d: want k=%+v, v=%+v; got k=%+v, v=%+v", i, tt.wkey, tt.wval, k, v)
			}
		}()
	}
}

// TestConcurrentReadTx ensures that current read transaction can see all prior writes stored in read buffer
func TestConcurrentReadTx(t *testing.T) {
	b, _ := betesting.NewTmpBackend(t, time.Hour, 10000)
	defer betesting.Close(t, b)

	wtx1 := b.BatchTx()
	wtx1.Lock()
	wtx1.UnsafeCreateBucket(schema.Key)
	wtx1.UnsafePut(schema.Key, []byte("abc"), []byte("ABC"))
	wtx1.UnsafePut(schema.Key, []byte("overwrite"), []byte("1"))
	wtx1.Unlock()

	wtx2 := b.BatchTx()
	wtx2.Lock()
	wtx2.UnsafePut(schema.Key, []byte("def"), []byte("DEF"))
	wtx2.UnsafePut(schema.Key, []byte("overwrite"), []byte("2"))
	wtx2.Unlock()

	rtx := b.ConcurrentReadTx()
	rtx.RLock() // no-op
	k, v := rtx.UnsafeRange(schema.Key, []byte("abc"), []byte("\xff"), 0)
	rtx.RUnlock()
	wKey := [][]byte{[]byte("abc"), []byte("def"), []byte("overwrite")}
	wVal := [][]byte{[]byte("ABC"), []byte("DEF"), []byte("2")}
	if !reflect.DeepEqual(wKey, k) || !reflect.DeepEqual(wVal, v) {
		t.Errorf("want k=%+v, v=%+v; got k=%+v, v=%+v", wKey, wVal, k, v)
	}
}

// TestBackendWritebackForEach checks that partially written / buffered
// data is visited in the same order as fully committed data.
func TestBackendWritebackForEach(t *testing.T) {
	b, _ := betesting.NewTmpBackend(t, time.Hour, 10000)
	defer betesting.Close(t, b)

	tx := b.BatchTx()
	tx.Lock()
	tx.UnsafeCreateBucket(schema.Key)
	for i := 0; i < 5; i++ {
		k := []byte(fmt.Sprintf("%04d", i))
		tx.UnsafePut(schema.Key, k, []byte("bar"))
	}
	tx.Unlock()

	// writeback
	b.ForceCommit()

	tx.Lock()
	tx.UnsafeCreateBucket(schema.Key)
	for i := 5; i < 20; i++ {
		k := []byte(fmt.Sprintf("%04d", i))
		tx.UnsafePut(schema.Key, k, []byte("bar"))
	}
	tx.Unlock()

	seq := ""
	getSeq := func(k, v []byte) error {
		seq += string(k)
		return nil
	}
	rtx := b.ReadTx()
	rtx.RLock()
	require.NoError(t, rtx.UnsafeForEach(schema.Key, getSeq))
	rtx.RUnlock()

	partialSeq := seq

	seq = ""
	b.ForceCommit()

	tx.Lock()
	require.NoError(t, tx.UnsafeForEach(schema.Key, getSeq))
	tx.Unlock()

	if seq != partialSeq {
		t.Fatalf("expected %q, got %q", seq, partialSeq)
	}
}

func TestBackendDefragConcurrentWrites(t *testing.T) {
	b := newDefragBackend(t)

	largeVal := make([]byte, 1024)
	populateKeys(t, b, schema.Test, "pre_%05d", largeVal, 5000)

	// delete some keys to create reclaimable space
	deleteKeyRange(t, b, schema.Test, "pre_%05d", 0, 2500)

	// Writer waits until defrag is about to start, then writes concurrently.
	var wg sync.WaitGroup
	stop := make(chan struct{})
	defragStarted := make(chan struct{})
	var opsDone atomic.Int32

	wg.Add(1)
	go func() {
		defer wg.Done()
		<-defragStarted
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			tx := b.BatchTx()
			tx.Lock()
			tx.UnsafePut(schema.Test, []byte(fmt.Sprintf("concurrent_%04d", i)), []byte("new"))
			tx.Unlock()
			opsDone.Add(1)
		}
	}()

	defragDone := make(chan error, 1)
	close(defragStarted)
	go func() {
		defragDone <- b.Defrag()
	}()
	err := <-defragDone
	close(stop)
	wg.Wait()

	require.NoError(t, err)
	require.Greater(t, opsDone.Load(), int32(0), "at least one write must complete during defrag")

	// verify pre-existing keys that weren't deleted still exist
	rtx := b.BatchTx()
	rtx.Lock()
	requireKeysHaveValue(t, rtx, schema.Test, "pre_%05d", 2500, 5000, largeVal)
	requireKeysAbsent(t, rtx, schema.Test, "pre_%05d", 0, 2500)
	rtx.Unlock()

	// verify at least some concurrent writes are present
	b.ForceCommit()
	rtx.Lock()
	var found int
	for i := 0; i < 10000; i++ {
		keys, _ := rtx.UnsafeRange(schema.Test, []byte(fmt.Sprintf("concurrent_%04d", i)), nil, 0)
		if len(keys) > 0 {
			found++
		}
	}
	rtx.Unlock()
	assert.Greater(t, found, 0, "at least some concurrent writes should be present after defrag")
}

func TestBackendDefragConcurrentReads(t *testing.T) {
	b := newDefragBackend(t)

	populateKeys(t, b, schema.Test, "key_%04d", []byte("value"), 1000)

	var wg sync.WaitGroup
	stop := make(chan struct{})
	readErrors := make(chan error, 100)

	// concurrent reader
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			rtx := b.ConcurrentReadTx()
			rtx.RLock()
			keys, _ := rtx.UnsafeRange(schema.Test, []byte("key_0500"), nil, 0)
			if len(keys) == 0 {
				readErrors <- fmt.Errorf("key_0500 not found during defrag")
			}
			rtx.RUnlock()
		}
	}()

	err := b.Defrag()
	close(stop)
	wg.Wait()
	close(readErrors)

	require.NoError(t, err)
	for readErr := range readErrors {
		t.Error(readErr)
	}
}

func TestBackendDefragWriteAvailability(t *testing.T) {
	b := newDefragBackend(t)

	populateKeys(t, b, schema.Test, "key_%06d", []byte("value"), backend.DefragLimitForTest()+100)

	// delete half the keys to make defrag meaningful
	deleteKeyRange(t, b, schema.Test, "key_%06d", 0, backend.DefragLimitForTest()/2)

	// track write latencies during defrag
	var wg sync.WaitGroup
	stop := make(chan struct{})
	var maxWriteLatency time.Duration
	var mu sync.Mutex

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			start := time.Now()
			wtx := b.BatchTx()
			wtx.Lock()
			wtx.UnsafePut(schema.Test, []byte(fmt.Sprintf("during_%06d", i)), []byte("val"))
			wtx.Unlock()
			elapsed := time.Since(start)

			mu.Lock()
			if elapsed > maxWriteLatency {
				maxWriteLatency = elapsed
			}
			mu.Unlock()
		}
	}()

	err := b.Defrag()
	close(stop)
	wg.Wait()

	require.NoError(t, err)
	t.Logf("max write latency during defrag: %v", maxWriteLatency)
	// The max latency should be well under a second since the copy phase
	// doesn't hold the batchTx lock. Writes only block during the brief
	// replay + switchover phases.
	assert.Less(t, maxWriteLatency, 5*time.Second,
		"write latency during defrag should be bounded")
}

func TestBackendDefragConcurrentReadsAndWrites(t *testing.T) {
	b := newDefragBackend(t)

	populateKeys(t, b, schema.Test, "key_%04d", []byte("original"), 500)

	var wg sync.WaitGroup
	stop := make(chan struct{})

	writerWg, _ := startConcurrentWriter(b, schema.Test, "write_%04d", []byte("new"), stop)

	// concurrent reader using single-key lookups (schema.Test is not a safe-range bucket)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			rtx := b.ConcurrentReadTx()
			rtx.RLock()
			rtx.UnsafeRange(schema.Test, []byte("key_0250"), nil, 0)
			rtx.RUnlock()
		}
	}()

	err := b.Defrag()
	close(stop)
	writerWg.Wait()
	wg.Wait()

	require.NoError(t, err)

	// verify original data survived
	b.ForceCommit()
	rtx := b.BatchTx()
	rtx.Lock()
	requireKeysHaveValue(t, rtx, schema.Test, "key_%04d", 0, 500, []byte("original"))
	rtx.Unlock()
}

func TestBackendDefragDeletesDuringDefrag(t *testing.T) {
	b := newDefragBackend(t)

	populateKeys(t, b, schema.Test, "key_%04d", []byte("value"), 100)

	// delete keys concurrently during defrag
	var wg sync.WaitGroup
	stop := make(chan struct{})

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			select {
			case <-stop:
				return
			default:
			}
			tx := b.BatchTx()
			tx.Lock()
			tx.UnsafeDelete(schema.Test, []byte(fmt.Sprintf("key_%04d", i)))
			tx.Unlock()
		}
	}()

	err := b.Defrag()
	close(stop)
	wg.Wait()

	require.NoError(t, err)

	// keys 50-99 should still exist
	b.ForceCommit()
	rtx := b.BatchTx()
	rtx.Lock()
	requireKeysExist(t, rtx, schema.Test, "key_%04d", 50, 100)
	rtx.Unlock()
}

func TestBackendDefragLogicalConsistency(t *testing.T) {
	b := newDefragBackend(t)

	tx := b.BatchTx()
	tx.Lock()
	tx.UnsafeCreateBucket(schema.Test)
	tx.UnsafeCreateBucket(schema.Key)
	for i := 0; i < 500; i++ {
		tx.UnsafePut(schema.Test, []byte(fmt.Sprintf("test_%04d", i)), []byte(fmt.Sprintf("val_%04d", i)))
	}
	for i := 0; i < 300; i++ {
		tx.UnsafeSeqPut(schema.Key, []byte(fmt.Sprintf("rev_%06d", i)), []byte(fmt.Sprintf("data_%06d", i)))
	}
	tx.Unlock()
	b.ForceCommit()

	// delete some keys to create fragmentation
	deleteKeyRange(t, b, schema.Test, "test_%04d", 0, 200)

	// capture logical hash before defrag
	hashBefore, err := b.Hash(nil)
	require.NoError(t, err)
	sizeBefore := b.Size()

	err = b.Defrag()
	require.NoError(t, err)

	// hash must be identical — same logical content
	hashAfter, err := b.Hash(nil)
	require.NoError(t, err)
	assert.Equal(t, hashBefore, hashAfter, "logical hash must match after defrag")

	// physical size should have shrunk
	sizeAfter := b.Size()
	assert.Less(t, sizeAfter, sizeBefore, "defrag should reclaim space")

	// verify all surviving keys individually
	b.ForceCommit()
	rtx := b.BatchTx()
	rtx.Lock()
	for i := 200; i < 500; i++ {
		keys, vals := rtx.UnsafeRange(schema.Test, []byte(fmt.Sprintf("test_%04d", i)), nil, 0)
		require.Lenf(t, keys, 1, "test_%04d should exist", i)
		assert.Equal(t, []byte(fmt.Sprintf("val_%04d", i)), vals[0])
	}
	for i := 0; i < 300; i++ {
		keys, vals := rtx.UnsafeRange(schema.Key, []byte(fmt.Sprintf("rev_%06d", i)), nil, 0)
		require.Lenf(t, keys, 1, "rev_%06d should exist", i)
		assert.Equal(t, []byte(fmt.Sprintf("data_%06d", i)), vals[0])
	}
	// deleted keys must be gone
	requireKeysAbsent(t, rtx, schema.Test, "test_%04d", 0, 200)
	rtx.Unlock()
}

func TestBackendDefragOverwriteDuringCopy(t *testing.T) {
	b := newDefragBackend(t)

	populateKeys(t, b, schema.Test, "key_%04d", []byte("original"), 100)

	// overwrite keys concurrently during defrag
	var wg sync.WaitGroup
	stop := make(chan struct{})

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			wtx := b.BatchTx()
			wtx.Lock()
			for i := 0; i < 50; i++ {
				wtx.UnsafePut(schema.Test, []byte(fmt.Sprintf("key_%04d", i)), []byte("updated"))
			}
			wtx.Unlock()
		}
	}()

	err := b.Defrag()
	close(stop)
	wg.Wait()
	require.NoError(t, err)

	// all 50 overwritten keys must have the new value
	b.ForceCommit()
	rtx := b.BatchTx()
	rtx.Lock()
	requireKeysHaveValue(t, rtx, schema.Test, "key_%04d", 0, 50, []byte("updated"))
	// keys 50-99 should still have original value
	requireKeysHaveValue(t, rtx, schema.Test, "key_%04d", 50, 100, []byte("original"))
	rtx.Unlock()
}

func TestBackendDefragNewBucketDuringCopy(t *testing.T) {
	b := newDefragBackend(t)

	populateKeys(t, b, schema.Test, "key_%04d", []byte("value"), 100)

	// create a new bucket and write to it during defrag
	var wg sync.WaitGroup
	done := make(chan struct{})

	wg.Add(1)
	go func() {
		defer wg.Done()
		wtx := b.BatchTx()
		wtx.Lock()
		wtx.UnsafeCreateBucket(schema.Key)
		for i := 0; i < 50; i++ {
			wtx.UnsafeSeqPut(schema.Key, []byte(fmt.Sprintf("newkey_%04d", i)), []byte("newval"))
		}
		wtx.Unlock()
		close(done)
	}()

	err := b.Defrag()
	<-done
	wg.Wait()
	require.NoError(t, err)

	// verify original bucket data
	b.ForceCommit()
	rtx := b.BatchTx()
	rtx.Lock()
	requireKeysExist(t, rtx, schema.Test, "key_%04d", 0, 100)
	// verify new bucket and its data exist
	requireKeysHaveValue(t, rtx, schema.Key, "newkey_%04d", 0, 50, []byte("newval"))
	rtx.Unlock()
}

func TestBackendDefragMultipleSequential(t *testing.T) {
	b := newDefragBackend(t)

	populateKeys(t, b, schema.Test, "key_%04d", []byte("value"), 200)

	tx := b.BatchTx()
	for round := 0; round < 3; round++ {
		// delete some keys before each defrag
		tx.Lock()
		for i := round * 20; i < (round+1)*20; i++ {
			tx.UnsafeDelete(schema.Test, []byte(fmt.Sprintf("key_%04d", i)))
		}
		tx.Unlock()
		b.ForceCommit()

		hashBefore, err := b.Hash(nil)
		require.NoError(t, err)

		err = b.Defrag()
		require.NoErrorf(t, err, "defrag round %d failed", round)

		hashAfter, err := b.Hash(nil)
		require.NoError(t, err)
		assert.Equalf(t, hashBefore, hashAfter, "hash mismatch after defrag round %d", round)

		// add new keys after each defrag
		tx.Lock()
		tx.UnsafeCreateBucket(schema.Test)
		for i := 0; i < 10; i++ {
			tx.UnsafePut(schema.Test, []byte(fmt.Sprintf("round%d_%04d", round, i)), []byte("new"))
		}
		tx.Unlock()
		b.ForceCommit()
	}

	// verify round keys from all 3 rounds exist
	rtx := b.BatchTx()
	rtx.Lock()
	for round := 0; round < 3; round++ {
		requireKeysExist(t, rtx, schema.Test, fmt.Sprintf("round%d_%%04d", round), 0, 10)
	}
	rtx.Unlock()
}

func TestBackendDefragEmptyDatabase(t *testing.T) {
	b := newDefragBackend(t)

	err := b.Defrag()
	require.NoError(t, err)

	// verify we can still use the database after defragging an empty one
	tx := b.BatchTx()
	tx.Lock()
	tx.UnsafeCreateBucket(schema.Test)
	tx.UnsafePut(schema.Test, []byte("after_defrag"), []byte("works"))
	tx.Unlock()
	b.ForceCommit()

	rtx := b.BatchTx()
	rtx.Lock()
	keys, vals := rtx.UnsafeRange(schema.Test, []byte("after_defrag"), nil, 0)
	require.Len(t, keys, 1)
	assert.Equal(t, []byte("works"), vals[0])
	rtx.Unlock()
}

func TestBackendDefragLargeJournalReplay(t *testing.T) {
	b := newDefragBackend(t)

	// pre-populate enough data to slow down Phase 1 copy, giving the
	// concurrent writer time to exceed defragLimit
	populateKeys(t, b, schema.Test, "pre_%05d", make([]byte, 1024), 5000)

	// write enough keys during defrag to exceed defragLimit (10000) in the replay,
	// exercising the batched commit path in replayJournal
	var wg sync.WaitGroup
	stop := make(chan struct{})
	totalWritten := make(chan int, 1)

	wg.Add(1)
	go func() {
		defer wg.Done()
		var count int
		for i := 0; ; i++ {
			select {
			case <-stop:
				totalWritten <- count
				return
			default:
			}
			wtx := b.BatchTx()
			wtx.Lock()
			for j := 0; j < 10; j++ {
				wtx.UnsafePut(schema.Test, []byte(fmt.Sprintf("journal_%06d", count)), []byte("jval"))
				count++
			}
			wtx.Unlock()
		}
	}()

	err := b.Defrag()
	close(stop)
	wg.Wait()
	written := <-totalWritten

	require.NoError(t, err)
	require.Greater(t, written, 0,
		"at least some writes must occur during defrag")
	t.Logf("journal ops written during defrag: %d", written)

	// verify pre-existing keys survived
	b.ForceCommit()
	rtx := b.BatchTx()
	rtx.Lock()
	requireKeysExist(t, rtx, schema.Test, "pre_%05d", 0, 5000)
	// verify at least some journal keys exist
	var found int
	for i := 0; i < written; i++ {
		keys, _ := rtx.UnsafeRange(schema.Test, []byte(fmt.Sprintf("journal_%06d", i)), nil, 0)
		if len(keys) > 0 {
			found++
		}
	}
	rtx.Unlock()
	assert.Greater(t, found, 0, "journal writes should be present after defrag")
}

func TestBackendDefragReadsDuringDefragSeeBufferedPuts(t *testing.T) {
	b := newDefragBackend(t)

	tx := b.BatchTx()
	tx.Lock()
	tx.UnsafeCreateBucket(schema.Test)
	tx.UnsafePut(schema.Test, []byte("pre_key"), []byte("pre_val"))
	tx.Unlock()
	b.ForceCommit()

	// During defrag, writes go to journal+buffer only (not bbolt).
	// Readers via ConcurrentReadTx should still see these puts
	// because readTx.buf is preserved (commits are skipped).
	wroteKey := make(chan struct{})
	readResult := make(chan bool, 1)
	stop := make(chan struct{})
	var wg sync.WaitGroup

	// writer: put a key during defrag and signal
	wg.Add(1)
	go func() {
		defer wg.Done()
		// wait a moment for defrag to start
		time.Sleep(10 * time.Millisecond)
		wtx := b.BatchTx()
		wtx.Lock()
		wtx.UnsafePut(schema.Test, []byte("during_defrag"), []byte("visible"))
		wtx.Unlock()
		close(wroteKey)
		<-stop
	}()

	// reader: after the write, check if the key is visible via ConcurrentReadTx
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-wroteKey
		time.Sleep(5 * time.Millisecond)
		rtx := b.ConcurrentReadTx()
		rtx.RLock()
		keys, _ := rtx.UnsafeRange(schema.Test, []byte("during_defrag"), nil, 0)
		rtx.RUnlock()
		readResult <- len(keys) > 0
		<-stop
	}()

	err := b.Defrag()
	close(stop)
	wg.Wait()
	require.NoError(t, err)

	visible := <-readResult
	assert.True(t, visible, "puts during defrag should be visible to ConcurrentReadTx via buffer")

	// verify key is also present after defrag
	b.ForceCommit()
	rtx := b.BatchTx()
	rtx.Lock()
	keys, vals := rtx.UnsafeRange(schema.Test, []byte("during_defrag"), nil, 0)
	rtx.Unlock()
	require.Len(t, keys, 1)
	assert.Equal(t, []byte("visible"), vals[0])
}

func TestBackendDefragExceedBatchLimit(t *testing.T) {
	// Use a small batch limit so we can exceed it without writing too many keys.
	b, _ := betesting.NewTmpBackend(t, time.Hour, 100)
	defer betesting.Close(t, b)
	backend.SetNonBlockingDefragForTest(b, true)

	populateKeys(t, b, schema.Test, "seed_%05d", make([]byte, 1024), 5000)

	// Write more than batchLimit keys during defrag to exercise the
	// Unlock path where pending >= batchLimit but commits are skipped.
	var wg sync.WaitGroup
	stop := make(chan struct{})
	totalWritten := make(chan int, 1)

	wg.Add(1)
	go func() {
		defer wg.Done()
		var count int
		for i := 0; ; i++ {
			select {
			case <-stop:
				totalWritten <- count
				return
			default:
			}
			wtx := b.BatchTx()
			wtx.Lock()
			for j := 0; j < 10; j++ {
				wtx.UnsafePut(schema.Test, []byte(fmt.Sprintf("batch_%06d", count)), []byte("v"))
				count++
			}
			wtx.Unlock()
		}
	}()

	err := b.Defrag()
	close(stop)
	wg.Wait()
	written := <-totalWritten

	require.NoError(t, err)
	t.Logf("wrote %d keys during defrag (batch limit = 100)", written)
	require.Greater(t, written, 100, "should have exceeded the batch limit during defrag")

	// verify all keys are present after defrag
	b.ForceCommit()
	rtx := b.BatchTx()
	rtx.Lock()
	var found int
	for i := 0; i < written; i++ {
		keys, _ := rtx.UnsafeRange(schema.Test, []byte(fmt.Sprintf("batch_%06d", i)), nil, 0)
		if len(keys) > 0 {
			found++
		}
	}
	rtx.Unlock()
	assert.Equal(t, written, found, "all keys written during defrag should be present")
}

func TestBackendDefragCommitDuringDefragPreservesData(t *testing.T) {
	b := newDefragBackend(t)

	tx := b.BatchTx()
	tx.Lock()
	tx.UnsafeCreateBucket(schema.Test)
	tx.UnsafePut(schema.Test, []byte("seed"), []byte("value"))
	tx.Unlock()
	b.ForceCommit()

	// Simulate what backend.run() does: call Commit() periodically.
	// During defrag, Commit() should be skipped so readTx.buf isn't cleared.
	wroteKeys := make(chan struct{})
	commitDone := make(chan struct{})
	stop := make(chan struct{})
	var wg sync.WaitGroup

	// writer: put keys during defrag
	wg.Add(1)
	go func() {
		defer wg.Done()
		time.Sleep(10 * time.Millisecond)
		for i := 0; i < 50; i++ {
			wtx := b.BatchTx()
			wtx.Lock()
			wtx.UnsafePut(schema.Test, []byte(fmt.Sprintf("commit_test_%04d", i)), []byte("val"))
			wtx.Unlock()
		}
		close(wroteKeys)
		<-stop
	}()

	// committer: call ForceCommit after writes, simulating periodic timer
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-wroteKeys
		b.ForceCommit()
		close(commitDone)
		<-stop
	}()

	// reader: after commit, verify data is still visible
	readResult := make(chan int, 1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-commitDone
		rtx := b.ConcurrentReadTx()
		rtx.RLock()
		var found int
		for i := 0; i < 50; i++ {
			keys, _ := rtx.UnsafeRange(schema.Test, []byte(fmt.Sprintf("commit_test_%04d", i)), nil, 0)
			if len(keys) > 0 {
				found++
			}
		}
		rtx.RUnlock()
		readResult <- found
		<-stop
	}()

	err := b.Defrag()
	close(stop)
	wg.Wait()
	require.NoError(t, err)

	found := <-readResult
	assert.Equal(t, 50, found, "all puts should remain visible after Commit() during defrag")
}

func TestBackendDefragConcurrentReadTxSurvivesSwitchover(t *testing.T) {
	b := newDefragBackend(t)

	populateKeys(t, b, schema.Test, "key_%04d", []byte("value"), 100)

	// Acquire a ConcurrentReadTx BEFORE defrag starts. This holds a
	// reference to the old DB's bbolt read tx via txWg. Phase 3 must
	// wait for this reader to finish before it can close the old DB.
	longReader := b.ConcurrentReadTx()
	longReader.RLock()

	// Verify the long reader can read data
	keys, vals := longReader.UnsafeRange(schema.Test, []byte("key_0050"), nil, 0)
	require.Len(t, keys, 1)
	assert.Equal(t, []byte("value"), vals[0])

	defragDone := make(chan error, 1)
	go func() {
		defragDone <- b.Defrag()
	}()

	// Hold the read tx open for a bit — Phase 3 should block on db.Close()
	// waiting for this reader to release.
	time.Sleep(50 * time.Millisecond)

	select {
	case err := <-defragDone:
		t.Fatalf("Defrag finished before longReader.RUnlock(): %v", err)
	default:
	}

	// The reader should still be able to read from the old snapshot
	keys, vals = longReader.UnsafeRange(schema.Test, []byte("key_0000"), nil, 0)
	require.Len(t, keys, 1)
	assert.Equal(t, []byte("value"), vals[0])

	// Release the long reader — this unblocks Phase 3
	longReader.RUnlock()

	err := <-defragDone
	require.NoError(t, err)

	// After defrag, verify new reads work on the new DB
	b.ForceCommit()
	rtx := b.ConcurrentReadTx()
	rtx.RLock()
	for i := 0; i < 100; i++ {
		keys, _ := rtx.UnsafeRange(schema.Test, []byte(fmt.Sprintf("key_%04d", i)), nil, 0)
		require.Lenf(t, keys, 1, "key_%04d should exist after defrag", i)
	}
	rtx.RUnlock()
}

func TestBackendDefragNoDeadlock(t *testing.T) {
	b := newDefragBackend(t)

	populateKeys(t, b, schema.Test, "key_%04d", make([]byte, 512), 1000)

	done := make(chan struct{})
	go func() {
		defer close(done)
		var wg sync.WaitGroup
		stop := make(chan struct{})

		// Goroutine doing writes (acquires batchTx lock)
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; ; i++ {
				select {
				case <-stop:
					return
				default:
				}
				tx := b.BatchTx()
				tx.Lock()
				tx.UnsafePut(schema.Test, []byte(fmt.Sprintf("dl_%04d", i%100)), []byte("w"))
				tx.Unlock()
			}
		}()

		// Goroutine doing reads (acquires readTx + mu)
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				rtx := b.ConcurrentReadTx()
				rtx.RLock()
				rtx.UnsafeRange(schema.Test, []byte("key_0000"), nil, 0)
				rtx.RUnlock()
			}
		}()

		// Goroutine doing commits (acquires batchTx lock + mu)
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				b.ForceCommit()
			}
		}()

		// Run 5 sequential defrags (acquires batchTx + mu + readTx)
		for range 5 {
			require.NoError(t, b.Defrag())
		}

		close(stop)
		wg.Wait()
	}()

	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("deadlock detected: defrag with concurrent writes, reads, and commits did not complete within 30s")
	}
}

func TestBackendDefragMultipleConcurrentReadTxDuringSwitchover(t *testing.T) {
	b := newDefragBackend(t)

	populateKeys(t, b, schema.Test, "key_%04d", []byte("value"), 200)

	// Simulate many concurrent readers that continuously create and
	// release ConcurrentReadTx during defrag, including across the
	// Phase 3 switchover. Each reader creates short-lived snapshots
	// that must not panic or return errors when the DB is swapped.
	var wg sync.WaitGroup
	stop := make(chan struct{})
	readErrors := make(chan error, 1000)

	for r := range 5 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				rtx := b.ConcurrentReadTx()
				rtx.RLock()
				key := []byte(fmt.Sprintf("key_%04d", id*20))
				keys, vals := rtx.UnsafeRange(schema.Test, key, nil, 0)
				if len(keys) != 1 {
					readErrors <- fmt.Errorf("reader %d: expected 1 key, got %d", id, len(keys))
				} else if string(vals[0]) != "value" {
					readErrors <- fmt.Errorf("reader %d: expected 'value', got %q", id, vals[0])
				}
				rtx.RUnlock()
			}
		}(r)
	}

	err := b.Defrag()
	close(stop)
	wg.Wait()
	close(readErrors)

	require.NoError(t, err)

	var errs []error
	for e := range readErrors {
		errs = append(errs, e)
	}
	assert.Empty(t, errs, "no read errors should occur during defrag switchover")

	// Verify data integrity after defrag
	b.ForceCommit()
	rtx := b.BatchTx()
	rtx.Lock()
	requireKeysExist(t, rtx, schema.Test, "key_%04d", 0, 200)
	rtx.Unlock()
}

// TestBackendDefragWriteVisibilityDuringCopy verifies that writes made during
// the defrag copy phase are immediately visible via UnsafeRange on the batch
// transaction. This reproduces a crash (range failed to find revision pair)
// that occurred when writes were only sent to the journal and not to bbolt.
func TestBackendDefragWriteVisibilityDuringCopy(t *testing.T) {
	b := newDefragBackend(t)

	populateKeys(t, b, schema.Test, "init_%04d", []byte("val"), 100)

	// Start defrag — during Phase 1 the journal is active.
	defragDone := make(chan error, 1)
	go func() {
		defragDone <- b.Defrag()
	}()

	// Write keys and immediately read them back while defrag is running.
	// If the write only goes to the journal (not bbolt), UnsafeRange
	// returns 0 results — reproducing the fatal crash.
	for attempt := 0; attempt < 50; attempt++ {
		key := []byte(fmt.Sprintf("during_%04d", attempt))
		val := []byte(fmt.Sprintf("v%d", attempt))

		tx := b.BatchTx()
		tx.Lock()
		tx.UnsafePut(schema.Test, key, val)
		keys, vals := tx.UnsafeRange(schema.Test, key, nil, 0)
		tx.Unlock()

		require.Lenf(t, keys, 1, "key %s written during defrag must be readable", key)
		require.Equal(t, val, vals[0])
	}

	err := <-defragDone
	require.NoError(t, err)

	// After defrag completes, all writes must survive in the new database.
	b.ForceCommit()
	rtx := b.BatchTx()
	rtx.Lock()
	requireKeysExist(t, rtx, schema.Test, "during_%04d", 0, 50)
	requireKeysExist(t, rtx, schema.Test, "init_%04d", 0, 100)
	rtx.Unlock()
}

// TestBackendDefragBackpressure verifies that a small journal limit
// causes writers to be throttled (not deadlocked) during defrag, and
// that all data is consistent after defrag completes.
func TestBackendDefragBackpressure(t *testing.T) {
	b := newDefragBackend(t)

	// Set a very small journal limit to force backpressure during defrag.
	backend.SetDefragJournalMaxOpsForTest(b, 50)

	// Pre-populate enough data to make the copy phase take time.
	populateKeys(t, b, schema.Test, "pre_%05d", make([]byte, 256), 5000)

	// Concurrent writer that will exceed the journal limit many times over.
	stop := make(chan struct{})
	wg, written := startConcurrentWriter(b, schema.Test, "bp_%06d", []byte("val"), stop)

	// Defrag must complete without deadlocking.
	err := b.Defrag()
	close(stop)
	wg.Wait()

	require.NoError(t, err)
	totalWritten := int(written.Load())
	require.Greater(t, totalWritten, 50,
		"writer should have produced more ops than the journal limit")
	t.Logf("wrote %d ops with journal limit 50", totalWritten)

	// Verify all pre-existing and concurrent writes survived.
	b.ForceCommit()
	rtx := b.BatchTx()
	rtx.Lock()
	requireKeysExist(t, rtx, schema.Test, "pre_%05d", 0, 5000)
	requireKeysExist(t, rtx, schema.Test, "bp_%06d", 0, totalWritten)
	rtx.Unlock()
}

// TestBackendDefragConcurrentLockInsideApply exercises the data race
// between LockInsideApply reading t.defragJournal without the mutex
// and defrag setting/clearing it under the mutex. Run with -race to
// verify there is no unsynchronized pointer access.
func TestBackendDefragConcurrentLockInsideApply(t *testing.T) {
	b := newDefragBackend(t)

	populateKeys(t, b, schema.Test, "key_%04d", make([]byte, 256), 1000)

	// Concurrent goroutines calling LockInsideApply while defrag
	// sets and clears the defragJournal pointer.
	var wg sync.WaitGroup
	stop := make(chan struct{})

	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; ; i++ {
				select {
				case <-stop:
					return
				default:
				}
				tx := b.BatchTx()
				tx.LockInsideApply()
				tx.UnsafePut(schema.Test, []byte(fmt.Sprintf("w%d_%06d", id, i)), []byte("v"))
				tx.Unlock()
			}
		}(g)
	}

	err := b.Defrag()
	close(stop)
	wg.Wait()
	require.NoError(t, err)
}

// TestBackendDefragCopyFailurePreservesData proves that when the copy
// phase fails during non-blocking defrag, no data is lost. Writes
// during the copy phase go to both the live database and the journal;
// discarding the journal on failure is safe because the live database
// already has every write.
func TestBackendDefragCopyFailurePreservesData(t *testing.T) {
	b := newDefragBackend(t)

	populateKeys(t, b, schema.Test, "pre_%04d", []byte("value"), 100)

	// Inject a copy failure. Writes will still flow into the live
	// database and the journal during the (simulated) copy phase.
	stop := make(chan struct{})

	backend.SetNonBlockDefragCopyFailHookForTest(b, func() error {
		// Give the concurrent writer time to produce writes while
		// the journal is active.
		time.Sleep(50 * time.Millisecond)
		return fmt.Errorf("simulated copy failure")
	})

	wg, written := startConcurrentWriter(b, schema.Test, "during_%06d", []byte("val"), stop)

	err := b.Defrag()
	close(stop)
	wg.Wait()

	// Defrag should have failed.
	require.Error(t, err)
	require.Contains(t, err.Error(), "simulated copy failure")

	totalWritten := int(written.Load())
	require.Greater(t, totalWritten, 0,
		"at least some writes must have occurred during the failed defrag")
	t.Logf("wrote %d ops during failed defrag copy", totalWritten)

	// All pre-existing data must survive.
	b.ForceCommit()
	rtx := b.BatchTx()
	rtx.Lock()
	requireKeysExist(t, rtx, schema.Test, "pre_%04d", 0, 100)
	// All writes during the failed copy must also survive — they went
	// to the live database, not just the journal.
	requireKeysExist(t, rtx, schema.Test, "during_%06d", 0, totalWritten)
	rtx.Unlock()

	// The server should remain fully functional after the failed defrag.
	tx := b.BatchTx()
	tx.Lock()
	tx.UnsafePut(schema.Test, []byte("after_failure"), []byte("ok"))
	tx.Unlock()
	b.ForceCommit()

	rtx = b.BatchTx()
	rtx.Lock()
	keys, vals := rtx.UnsafeRange(schema.Test, []byte("after_failure"), nil, 0)
	rtx.Unlock()
	require.Len(t, keys, 1)
	require.Equal(t, []byte("ok"), vals[0])
}
