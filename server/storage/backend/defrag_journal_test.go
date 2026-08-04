package backend

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefragJournalCloseAndDrain(t *testing.T) {
	j := newDefragJournal(0)

	j.appendCreateBucket([]byte("bucket1"))
	j.appendPut([]byte("bucket1"), []byte("key1"), []byte("val1"), false)
	j.appendPut([]byte("bucket1"), []byte("key2"), []byte("val2"), true)
	j.appendDelete([]byte("bucket1"), []byte("key1"))
	j.appendDeleteBucket([]byte("bucket1"))

	ops := j.closeAndDrain()
	require.Len(t, ops, 5)

	assert.Equal(t, opCreateBucket, ops[0].opType)
	assert.Equal(t, []byte("bucket1"), ops[0].bucketName)

	assert.Equal(t, opPut, ops[1].opType)
	assert.Equal(t, []byte("key1"), ops[1].key)
	assert.Equal(t, []byte("val1"), ops[1].value)
	assert.False(t, ops[1].seq)

	assert.Equal(t, opPut, ops[2].opType)
	assert.Equal(t, []byte("key2"), ops[2].key)
	assert.True(t, ops[2].seq)

	assert.Equal(t, opDelete, ops[3].opType)
	assert.Equal(t, []byte("key1"), ops[3].key)

	assert.Equal(t, opDeleteBucket, ops[4].opType)
	assert.Equal(t, []byte("bucket1"), ops[4].bucketName)

	// closeAndDrain again should be empty
	ops = j.closeAndDrain()
	assert.Empty(t, ops)
}

func TestDefragJournalAppendAfterClosePanics(t *testing.T) {
	j := newDefragJournal(0)
	j.closeAndDrain()

	assert.Panics(t, func() { j.appendPut([]byte("b"), []byte("k"), []byte("v"), false) })
	assert.Panics(t, func() { j.appendDelete([]byte("b"), []byte("k")) })
	assert.Panics(t, func() { j.appendCreateBucket([]byte("b")) })
	assert.Panics(t, func() { j.appendDeleteBucket([]byte("b")) })
}

func TestDefragJournalBackpressureBlocksWhenFull(t *testing.T) {
	j := newDefragJournal(5)

	// Fill to capacity.
	for range 5 {
		j.appendPut([]byte("b"), []byte("k"), []byte("v"), false)
	}
	require.Len(t, j.closeAndDrain(), 5)
	// Re-create since we just closed it.
	j = newDefragJournal(5)

	// Fill again.
	for range 5 {
		j.appendPut([]byte("b"), []byte("k"), []byte("v"), false)
	}

	// waitForSpace should block because the journal is at capacity.
	unblocked := make(chan struct{})
	go func() {
		j.waitForSpace()
		close(unblocked)
	}()

	select {
	case <-unblocked:
		t.Fatal("waitForSpace returned while journal is full")
	case <-time.After(50 * time.Millisecond):
	}

	// closeAndDrain frees space and wakes the blocked goroutine.
	ops := j.closeAndDrain()
	assert.Len(t, ops, 5)

	select {
	case <-unblocked:
	case <-time.After(time.Second):
		t.Fatal("waitForSpace did not unblock after closeAndDrain")
	}
}

func TestDefragJournalBackpressureMultipleWriters(t *testing.T) {
	j := newDefragJournal(2)

	for range 2 {
		j.appendPut([]byte("b"), []byte("k"), []byte("v"), false)
	}

	// Block two writers.
	var wg sync.WaitGroup
	wg.Add(2)
	for range 2 {
		go func() {
			defer wg.Done()
			j.waitForSpace()
		}()
	}

	// Give goroutines time to block.
	time.Sleep(50 * time.Millisecond)

	// closeAndDrain should wake both.
	j.closeAndDrain()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("not all writers unblocked after drain")
	}
}

func TestDefragJournalBackpressureDisabledWhenZero(t *testing.T) {
	j := newDefragJournal(0)

	// Fill well beyond what would normally be a limit.
	for range 100 {
		j.appendPut([]byte("b"), []byte("k"), []byte("v"), false)
	}

	// waitForSpace should return immediately when maxOps is 0 (unlimited).
	done := make(chan struct{})
	go func() {
		j.waitForSpace()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("waitForSpace blocked with maxOps=0")
	}
}
