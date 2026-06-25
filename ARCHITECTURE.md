# etcd - Architecture Documentation

This document provides a comprehensive overview of etcd's architecture, design decisions, and operational model.

## Table of Contents

- [Overview](#overview)
- [System Architecture](#system-architecture)
- [Core Components](#core-components)
- [Raft Consensus](#raft-consensus)
- [Storage Architecture](#storage-architecture)
- [Client API](#client-api)
- [Watch Mechanism](#watch-mechanism)
- [Lease System](#lease-system)
- [Authentication and Authorization](#authentication-and-authorization)
- [Cluster Management](#cluster-management)
- [Performance Characteristics](#performance-characteristics)
- [Failure Modes and Recovery](#failure-modes-and-recovery)
- [Design Decisions](#design-decisions)
- [Deployment Topology](#deployment-topology)

## Overview

### What is etcd?

etcd is a distributed, reliable key-value store for the most critical data of distributed systems. It is the foundation for storing all cluster state in Kubernetes and OpenShift.

**Core Characteristics**:
- **Consistency**: Strong consistency via Raft consensus
- **Reliability**: Survives network partitions and node failures
- **Performance**: Handles ~10,000 writes/sec in production
- **Simplicity**: Clean gRPC API with straightforward semantics
- **Security**: TLS encryption and RBAC authorization

**Primary Use Case**: Kubernetes/OpenShift cluster state storage
- Every pod, service, configmap, deployment is stored in etcd
- Leader election and coordination primitives
- Configuration management
- Service discovery

### Key Features

1. **Distributed Consensus**: Raft algorithm ensures all nodes agree on state
2. **Multi-Version Storage**: MVCC enables historical queries and watch
3. **Watch API**: Real-time notifications when keys change
4. **Lease System**: Time-bound key ownership with automatic expiration
5. **Transaction Support**: Atomic multi-key operations with conditions
6. **Linearizable Reads**: Strongest consistency guarantee
7. **Member Management**: Dynamic cluster membership changes
8. **Snapshot & Restore**: Point-in-time backup and recovery

## System Architecture

### High-Level Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│                        etcd Cluster                                  │
│                                                                       │
│  ┌──────────────┐      ┌──────────────┐      ┌──────────────┐      │
│  │   Node 1     │      │   Node 2     │      │   Node 3     │      │
│  │  (Leader)    │◄────►│  (Follower)  │◄────►│  (Follower)  │      │
│  │              │      │              │      │              │      │
│  │  ┌────────┐  │      │  ┌────────┐  │      │  ┌────────┐  │      │
│  │  │ gRPC   │  │      │  │ gRPC   │  │      │  │ gRPC   │  │      │
│  │  │ Server │  │      │  │ Server │  │      │  │ Server │  │      │
│  │  └────┬───┘  │      │  └────┬───┘  │      │  └────┬───┘  │      │
│  │       │      │      │       │      │      │       │      │      │
│  │  ┌────▼───┐  │      │  ┌────▼───┐  │      │  ┌────▼───┐  │      │
│  │  │  Raft  │  │      │  │  Raft  │  │      │  │  Raft  │  │      │
│  │  │  Node  │  │      │  │  Node  │  │      │  │  Node  │  │      │
│  │  └────┬───┘  │      │  └────┬───┘  │      │  └────┬───┘  │      │
│  │       │      │      │       │      │      │       │      │      │
│  │  ┌────▼───┐  │      │  ┌────▼───┐  │      │  ┌────▼───┐  │      │
│  │  │  MVCC  │  │      │  │  MVCC  │  │      │  │  MVCC  │  │      │
│  │  │  Store │  │      │  │  Store │  │      │  │  Store │  │      │
│  │  └────┬───┘  │      │  └────┬───┘  │      │  └────┬───┘  │      │
│  │       │      │      │       │      │      │       │      │      │
│  │  ┌────▼───┐  │      │  ┌────▼───┐  │      │  ┌────▼───┐  │      │
│  │  │ BoltDB │  │      │  │ BoltDB │  │      │  │ BoltDB │  │      │
│  │  │Backend │  │      │  │Backend │  │      │  │Backend │  │      │
│  │  └────────┘  │      │  └────────┘  │      │  └────────┘  │      │
│  │       │      │      │       │      │      │       │      │      │
│  │  ┌────▼───┐  │      │  ┌────▼───┐  │      │  ┌────▼───┐  │      │
│  │  │  WAL   │  │      │  │  WAL   │  │      │  │  WAL   │  │      │
│  │  │  Log   │  │      │  │  Log   │  │      │  │  Log   │  │      │
│  │  └────────┘  │      │  └────────┘  │      │  └────────┘  │      │
│  │       │      │      │       │      │      │       │      │      │
│  │  ┌────▼───┐  │      │  ┌────▼───┐  │      │  ┌────▼───┐  │      │
│  │  │ Snap   │  │      │  │ Snap   │  │      │  │ Snap   │  │      │
│  │  │ Store  │  │      │  │ Store  │  │      │  │ Store  │  │      │
│  │  └────────┘  │      │  └────────┘  │      │  └────────┘  │      │
│  └──────────────┘      └──────────────┘      └──────────────┘      │
│         ▲                     ▲                     ▲               │
└─────────┼─────────────────────┼─────────────────────┼───────────────┘
          │                     │                     │
          │                     │                     │
    ┌─────┴─────────────────────┴─────────────────────┴─────┐
    │                Client Applications                      │
    │  (Kubernetes API Server, etcdctl, custom clients)       │
    └─────────────────────────────────────────────────────────┘
```

### Data Flow

**Write Operation (Linearizable)**:
```
1. Client → gRPC API (any node)
2. Node → Forward to Leader (if not leader)
3. Leader → Propose to Raft
4. Raft → Replicate to majority
5. Raft → Commit entry
6. Leader → Apply to MVCC store
7. MVCC → Write to BoltDB backend
8. Backend → Persist to disk
9. Leader → Return response to client
```

**Read Operation (Linearizable)**:
```
1. Client → gRPC API (any node)
2. Node → Check leadership (quorum read)
3. Node → Read from local MVCC store
4. MVCC → Query BoltDB backend
5. Node → Return response to client
```

**Read Operation (Serializable)**:
```
1. Client → gRPC API (any node)
2. Node → Read from local MVCC store (no quorum check)
3. MVCC → Query BoltDB backend
4. Node → Return response to client
```

## Core Components

### 1. EtcdServer

**Location**: `server/etcdserver/server.go`

**Responsibilities**:
- Coordinate all server operations
- Manage Raft node lifecycle
- Process client requests
- Apply committed Raft entries
- Manage cluster membership
- Handle snapshots and WAL

**Key Structures**:
```go
type EtcdServer struct {
  // Raft consensus
  r            raftNode
  raftStorage  *raft.MemoryStorage
  
  // Storage
  kv           mvcc.ConsistentWatchableKV
  be           backend.Backend
  
  // Cluster state
  cluster      api.Cluster
  id           types.ID
  
  // Configuration
  Cfg          config.ServerConfig
  
  // Lease management
  lessor       lease.Lessor
  
  // Apply layer
  applyV3      apply.ApplyV3
}
```

**Event Loop**:
The server runs a main event loop that:
1. Receives committed Raft entries
2. Applies entries to state machine (MVCC store)
3. Sends responses to waiting clients
4. Processes snapshots
5. Handles leadership changes

### 2. Raft Node

**Location**: `server/etcdserver/raft.go`

**Responsibilities**:
- Implement Raft consensus protocol
- Manage leader election
- Replicate log entries
- Handle network communication between nodes
- Manage Raft configuration changes

**Raft States**:
- **Leader**: Accepts writes, replicates to followers
- **Follower**: Replicates from leader, redirects writes
- **Candidate**: Transitional state during election
- **Learner**: Non-voting member (used for adding nodes)

**Communication**:
- Uses `rafthttp` package for peer-to-peer communication
- Maintains persistent connections between nodes
- Handles message serialization and network failures

### 3. MVCC Storage

**Location**: `server/storage/mvcc/`

**Architecture**:
```
┌─────────────────────────────────────────┐
│      ConsistentWatchableKV              │
│   (Combines consistency + watch)        │
└─────────────────┬───────────────────────┘
                  │
┌─────────────────▼───────────────────────┐
│         WatchableKV                     │
│   (Adds watch functionality)            │
└─────────────────┬───────────────────────┘
                  │
┌─────────────────▼───────────────────────┐
│         KV Store                        │
│   (Core MVCC operations)                │
│   - Put, Get, Delete, Txn               │
│   - Revision management                 │
└─────────────────┬───────────────────────┘
                  │
┌─────────────────▼───────────────────────┐
│       BoltDB Backend                    │
│   (Persistent storage)                  │
└─────────────────────────────────────────┘
```

**Key Concepts**:

**Revision**: Global monotonically increasing counter
- Increments on every write transaction
- Used for point-in-time queries
- Forms the basis for MVCC

**Key Structure**:
```
Key: /registry/pods/default/my-pod
  CreateRevision: 100
  ModRevision: 105
  Version: 3
  Value: <serialized pod data>
```

**Index Structure**:
```
BoltDB Buckets:
  key → keyIndex (revision history)
  keyIndex → <CreateRevision, ModRevision, Version, Generations>
  
  meta → consistentIndex (last applied Raft index)
  meta → scheduledCompactRevision
  
  rev_{revision} → key-value data
```

### 4. Backend Storage (BoltDB)

**Location**: `server/storage/backend/`

**BoltDB Characteristics**:
- Embedded key-value database
- B+tree data structure
- ACID transactions
- MVCC support
- Memory-mapped files for performance
- Single-writer, multiple-readers

**Buckets**:
- `key`: Stores key index with revision history
- `meta`: Stores metadata (consistent index, compaction, etc.)
- `lease`: Stores lease information
- `auth`: Stores authentication data
- `members`: Stores cluster membership
- `cluster`: Stores cluster configuration

**Backend Operations**:
```go
// Batch write (transaction)
tx := be.BatchTx()
tx.Lock()
defer tx.Unlock()
tx.UnsafePut(buckets.Key, key, value)
```

**Optimization**:
- Read transactions don't block writes
- Batch commits for better performance
- Periodic defragmentation to reclaim space

### 5. Write-Ahead Log (WAL)

**Location**: `server/storage/wal/`

**Purpose**: Ensure durability of Raft log entries before they're applied.

**Characteristics**:
- Append-only log structure
- Fsync after every write for durability
- Segmented files for easier management
- Used for crash recovery

**WAL Record Types**:
```go
type Record struct {
  Type RecordType  // Entry, State, Snapshot, CRC
  Data []byte
  Crc  uint32
}
```

**Recovery Process**:
1. Read WAL from last snapshot
2. Replay entries to rebuild Raft state
3. Apply committed entries to state machine
4. Resume normal operation

### 6. Snapshot Store

**Location**: `server/etcdserver/api/snap/`

**Purpose**: Periodic snapshots of entire state for faster recovery.

**Snapshot Process**:
```
1. Trigger snapshot (after N entries, typically 10,000)
2. Serialize current MVCC state
3. Write snapshot file
4. Update WAL with snapshot metadata
5. Truncate old WAL entries
```

**Benefits**:
- Faster recovery (don't replay entire WAL)
- Smaller WAL size
- Efficient cluster bootstrapping

**Snapshot Format**:
```
Snapshot File:
  - Metadata (index, term, cluster config)
  - BoltDB database dump
  - CRC checksum
```

## Raft Consensus

### Raft Overview

etcd uses the Raft consensus algorithm to maintain a consistent, replicated log across all nodes.

**Raft Properties**:
- **Leader-based**: One leader coordinates all writes
- **Strong consistency**: Linearizable reads and writes
- **Fault tolerance**: Survives f failures in 2f+1 cluster
- **Understandable**: Simpler than Paxos, easier to implement

### Leader Election

**Process**:
1. Follower times out waiting for heartbeat
2. Becomes candidate, increments term
3. Votes for itself, requests votes from others
4. Wins if receives majority votes
5. Becomes leader, sends heartbeats

**Election Timeout**: Randomized to avoid split votes
- Typical: 1000-5000ms
- Prevents multiple candidates simultaneously

**Safety**: Only candidates with up-to-date logs can win
- Candidate's log must contain all committed entries
- Ensures committed entries are never lost

### Log Replication

**Write Flow**:
```
1. Client sends write to leader
2. Leader appends entry to local log
3. Leader sends AppendEntries RPC to followers
4. Followers append entry, respond with success
5. Leader commits entry after majority acknowledges
6. Leader applies entry to state machine
7. Leader notifies followers of commit
8. Followers apply entry to state machine
```

**Log Structure**:
```
Index:  1    2    3    4    5    6
Term:   1    1    2    2    3    3
Entry: [A]  [B]  [C]  [D]  [E]  [F]
        ↑                   ↑
    Committed          Uncommitted
```

**Commit Rules**:
- Entry is committed when majority has it
- All entries before committed entry are also committed
- Committed entries are durable and will never be lost

### Log Compaction

**Problem**: Log grows unbounded over time.

**Solution**: Snapshot + truncate log.

**Process**:
1. Create snapshot of current state
2. Store snapshot index and term
3. Truncate log up to snapshot index
4. New nodes receive snapshot instead of full log

**Triggered by**: Raft entry count (default: 10,000 entries)

### Network Partitions

**Scenario**: Network partition splits cluster into two groups.

**Majority Partition** (has quorum):
- Elects new leader
- Continues accepting writes
- Operates normally

**Minority Partition** (no quorum):
- Cannot elect leader
- Rejects writes
- Accepts serializable reads (may be stale)

**Recovery**: When partition heals
- Minority rejoins cluster
- Syncs with current leader
- Conflicting uncommitted entries are discarded

### Membership Changes

**Safe Reconfiguration**: Raft's joint consensus prevents split-brain during membership changes.

**Process**:
1. Propose configuration change (add/remove member)
2. Enter joint consensus (both old and new configs)
3. Commit joint consensus
4. Transition to new configuration
5. Commit new configuration

**Learner Members**: Non-voting members used for safe addition
- Receive log replication
- Don't participate in voting
- Promoted to voting member when caught up

## Storage Architecture

### MVCC Implementation

**Multi-Version Concurrency Control** enables:
- Snapshot isolation for transactions
- Historical queries
- Watch from any revision
- Non-blocking reads

**Revision Semantics**:

**Main Revision**: Global counter for all changes
```
Transaction 1: Put key=A → Revision 10
Transaction 2: Put key=B, Put key=C → Revision 11 (both get same revision)
```

**Mod Revision**: When key was last modified
```
Put key=A value=1 → ModRevision=10
Put key=A value=2 → ModRevision=15
Put key=B value=x → ModRevision=15
```

**Version**: How many times key was modified
```
Put key=A value=1 → Version=1
Put key=A value=2 → Version=2
Put key=A value=3 → Version=3
```

**Key Index Structure**:
```go
type keyIndex struct {
  key         []byte
  modified    revision  // last modified revision
  generations []generation
}

type generation struct {
  ver     int64     // version counter
  created revision  // create revision
  revs    []revision // all modifications
}
```

**Example**:
```
Put foo=a → Rev 10
Put foo=b → Rev 15
Delete foo → Rev 20
Put foo=c → Rev 25

keyIndex for "foo":
  modified: (25,0)
  generations:
    [0]: created: (10,0), ver: 2, revs: [(10,0), (15,0)]
    [1]: created: (25,0), ver: 1, revs: [(25,0)]
```

### Compaction

**Purpose**: Reclaim space by removing old revisions.

**Types**:

**Periodic Compaction** (default):
- Automatically compacts based on time
- Keeps revisions for configured duration (e.g., 5 minutes)
- `--auto-compaction-mode=periodic --auto-compaction-retention=5m`

**Revision Compaction**:
- Keeps last N revisions
- `--auto-compaction-mode=revision --auto-compaction-retention=1000`

**Process**:
1. Mark revisions < target as deleted
2. Async goroutine removes deleted revisions
3. BoltDB frees space in B+tree
4. Space reusable immediately

**Effect on Operations**:
- Queries at compacted revision return `ErrCompacted`
- Watches from compacted revision fail
- Historical data is lost

### Defragmentation

**Problem**: Even after compaction, BoltDB file has fragmentation and wasted space.

**Solution**: Defragmentation creates new database file with only live data.

**Process**:
1. Create new BoltDB file
2. Copy all live data to new file
3. Atomically replace old file
4. Old file space reclaimed

**Trigger**:
```bash
etcdctl defrag                    # Online defrag (blocks writes)
etcdutl defrag --data-dir=/path   # Offline defrag
```

**Trade-offs**:
- Online: Convenient but blocks writes, doubles disk usage temporarily
- Offline: Requires downtime but more efficient

### Transaction Model

**Transaction Structure**:
```
If <conditions>
Then <operations>
Else <operations>
```

**Example**:
```go
txn := Txn().
  If(Compare(Value("key"), "=", "old")).
  Then(OpPut("key", "new"), OpPut("status", "updated")).
  Else(OpGet("key"))
```

**Semantics**:
- Evaluated atomically
- All comparisons in If() evaluated first
- Execute Then() if all comparisons succeed
- Execute Else() otherwise
- Return results of executed operations

**Compare Operations**:
- `Value`: Compare key value
- `Version`: Compare key version
- `CreateRevision`: Compare create revision
- `ModRevision`: Compare mod revision
- `Lease`: Compare lease ID

**Use Cases**:
- Compare-and-swap (CAS)
- Distributed locks
- Conditional updates
- Atomic multi-key operations

## Client API

### gRPC Services

**KV Service** (`rpc.proto`):
```protobuf
service KV {
  rpc Range(RangeRequest) returns (RangeResponse);       // Get
  rpc Put(PutRequest) returns (PutResponse);             // Put
  rpc DeleteRange(DeleteRangeRequest) returns (DeleteRangeResponse); // Delete
  rpc Txn(TxnRequest) returns (TxnResponse);             // Transaction
  rpc Compact(CompactionRequest) returns (CompactionResponse); // Compact
}
```

**Watch Service**:
```protobuf
service Watch {
  rpc Watch(stream WatchRequest) returns (stream WatchResponse);
}
```

**Lease Service**:
```protobuf
service Lease {
  rpc LeaseGrant(LeaseGrantRequest) returns (LeaseGrantResponse);
  rpc LeaseRevoke(LeaseRevokeRequest) returns (LeaseRevokeResponse);
  rpc LeaseKeepAlive(stream LeaseKeepAliveRequest) returns (stream LeaseKeepAliveResponse);
  rpc LeaseTimeToLive(LeaseTimeToLiveRequest) returns (LeaseTimeToLiveResponse);
  rpc LeaseLeases(LeaseLeasesRequest) returns (LeaseLeasesResponse);
}
```

**Cluster Service**:
```protobuf
service Cluster {
  rpc MemberAdd(MemberAddRequest) returns (MemberAddResponse);
  rpc MemberRemove(MemberRemoveRequest) returns (MemberRemoveResponse);
  rpc MemberUpdate(MemberUpdateRequest) returns (MemberUpdateResponse);
  rpc MemberList(MemberListRequest) returns (MemberListResponse);
  rpc MemberPromote(MemberPromoteRequest) returns (MemberPromoteResponse);
}
```

**Maintenance Service**:
```protobuf
service Maintenance {
  rpc Alarm(AlarmRequest) returns (AlarmResponse);
  rpc Status(StatusRequest) returns (StatusResponse);
  rpc Defragment(DefragmentRequest) returns (DefragmentResponse);
  rpc Hash(HashRequest) returns (HashResponse);
  rpc HashKV(HashKVRequest) returns (HashKVResponse);
  rpc Snapshot(SnapshotRequest) returns (stream SnapshotResponse);
  rpc MoveLeader(MoveLeaderRequest) returns (MoveLeaderResponse);
  rpc Downgrade(DowngradeRequest) returns (DowngradeResponse);
}
```

### Client Library (client/v3)

**Basic Operations**:
```go
// Create client
cli, err := clientv3.New(clientv3.Config{
  Endpoints:   []string{"localhost:2379"},
  DialTimeout: 5 * time.Second,
})
defer cli.Close()

// Put
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
_, err = cli.Put(ctx, "key", "value")
cancel()

// Get
ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
resp, err := cli.Get(ctx, "key")
cancel()

// Get with prefix
resp, err := cli.Get(ctx, "prefix", clientv3.WithPrefix())

// Delete
_, err = cli.Delete(ctx, "key")

// Transaction
txn := cli.Txn(ctx).
  If(clientv3.Compare(clientv3.Value("key"), "=", "old")).
  Then(clientv3.OpPut("key", "new")).
  Else(clientv3.OpGet("key"))
resp, err := txn.Commit()
```

**Watch**:
```go
watchChan := cli.Watch(context.Background(), "key")
for watchResp := range watchChan {
  for _, event := range watchResp.Events {
    fmt.Printf("Type: %s, Key: %s, Value: %s\n", 
      event.Type, event.Kv.Key, event.Kv.Value)
  }
}
```

**Lease**:
```go
// Grant lease
lease, err := cli.Grant(ctx, 10) // 10 seconds

// Put with lease
_, err = cli.Put(ctx, "key", "value", clientv3.WithLease(lease.ID))

// Keep alive
ch, err := cli.KeepAlive(context.Background(), lease.ID)
for ka := range ch {
  // Lease renewed
}

// Revoke lease (deletes associated keys)
_, err = cli.Revoke(ctx, lease.ID)
```

## Watch Mechanism

### Architecture

```
┌────────────────────────────────────────────────┐
│              Watch Clients                      │
└─────────────────┬──────────────────────────────┘
                  │
┌─────────────────▼──────────────────────────────┐
│         WatchableStore                         │
│  ┌──────────────────────────────────────────┐  │
│  │         Watcher Registry                 │  │
│  │  - watchers map[string]*watcherGroup     │  │
│  │  - victims (slow watchers)               │  │
│  └──────────────────────────────────────────┘  │
└─────────────────┬──────────────────────────────┘
                  │
┌─────────────────▼──────────────────────────────┐
│         Event Generator                        │
│  - Notifies watchers on Put/Delete             │
│  - Batches events for efficiency               │
└─────────────────┬──────────────────────────────┘
                  │
┌─────────────────▼──────────────────────────────┐
│         MVCC Store                             │
│  - Generates events during apply               │
└────────────────────────────────────────────────┘
```

### Watch Types

**Key Watch**: Watch single key
```go
watchChan := cli.Watch(ctx, "foo")
```

**Prefix Watch**: Watch all keys with prefix
```go
watchChan := cli.Watch(ctx, "foo", clientv3.WithPrefix())
```

**Range Watch**: Watch key range
```go
watchChan := cli.Watch(ctx, "foo", clientv3.WithRange("foz"))
```

**Historical Watch**: Watch from past revision
```go
watchChan := cli.Watch(ctx, "foo", clientv3.WithRev(100))
```

### Event Types

```go
type Event struct {
  Type EventType  // PUT or DELETE
  Kv   *KeyValue  // Current key-value
  PrevKv *KeyValue // Previous key-value (if WithPrevKV)
}
```

### Watch Guarantees

1. **Ordered**: Events delivered in revision order
2. **Reliable**: No events are lost or duplicated
3. **Resumable**: Can resume from any revision
4. **Atomic**: Transactional puts generate single event

### Slow Consumer Handling

**Problem**: Slow consumer can't keep up with event rate.

**Solution**: Event buffering with overflow detection.

**Behavior**:
- Events buffered in channel (default 1024)
- If buffer fills, watcher marked as "victim"
- Victim watchers receive all queued events in one batch
- Client must process or risk watch cancellation

## Lease System

### Architecture

```
┌────────────────────────────────────────────┐
│           Lessor                            │
│  ┌──────────────────────────────────────┐  │
│  │  Lease Map                           │  │
│  │  leaseID → Lease{TTL, keys}          │  │
│  └──────────────────────────────────────┘  │
│  ┌──────────────────────────────────────┐  │
│  │  Expiry Queue                        │  │
│  │  heap of leases by expiry time       │  │
│  └──────────────────────────────────────┘  │
└───────────────┬────────────────────────────┘
                │
                │ Expired leases
                ▼
┌───────────────────────────────────────────┐
│       Lease Revoker                       │
│  - Proposes lease revocation via Raft    │
│  - Deletes associated keys                │
└───────────────────────────────────────────┘
```

### Lease Lifecycle

**1. Grant Lease**:
```go
lease, err := cli.Grant(ctx, 30) // 30 seconds TTL
```
- Assigns unique lease ID
- Sets initial TTL
- Raft-replicated for consistency

**2. Attach Keys to Lease**:
```go
cli.Put(ctx, "key", "value", clientv3.WithLease(lease.ID))
```
- Key ownership tied to lease
- Multiple keys can share one lease
- Key deleted when lease expires

**3. Keep Alive (Renew)**:
```go
ch, err := cli.KeepAlive(ctx, lease.ID)
for ka := range ch {
  // Lease renewed
}
```
- Client sends periodic heartbeats
- Resets lease expiry time
- Continues until context canceled

**4. Lease Expiration**:
- Lessor detects expired lease
- Proposes revocation via Raft
- Keys associated with lease deleted
- Lease removed from map

**5. Explicit Revocation**:
```go
cli.Revoke(ctx, lease.ID)
```
- Immediately revokes lease
- Deletes all associated keys
- Raft-replicated

### Use Cases

**Distributed Locks**:
```go
// Acquire lock
lease, _ := cli.Grant(ctx, 30)
txn := cli.Txn(ctx).
  If(clientv3.Compare(clientv3.CreateRevision("lock"), "=", 0)).
  Then(clientv3.OpPut("lock", "holder", clientv3.WithLease(lease.ID)))
resp, _ := txn.Commit()

if resp.Succeeded {
  // Lock acquired
  defer cli.Revoke(context.Background(), lease.ID)
  
  // Keep alive in background
  ch, _ := cli.KeepAlive(context.Background(), lease.ID)
  go func() {
    for range ch {}
  }()
  
  // Critical section
}
```

**Session Management**:
- Client creates lease at start
- Attaches session data to lease
- Keeps lease alive periodically
- Session auto-deleted if client crashes

**Service Discovery**:
- Service registers endpoint with lease
- Keeps lease alive while running
- Endpoint removed on service crash

## Authentication and Authorization

### Authentication

**Supported Methods**:
- **Simple Password**: Username/password authentication
- **TLS Client Certificates**: Mutual TLS authentication

**User Management**:
```bash
etcdctl user add myuser           # Add user
etcdctl user grant-role myuser admin  # Grant role
etcdctl auth enable                # Enable auth
```

**Client Authentication**:
```go
cli, err := clientv3.New(clientv3.Config{
  Endpoints: []string{"localhost:2379"},
  Username:  "myuser",
  Password:  "mypassword",
})
```

### Authorization (RBAC)

**Role-Based Access Control**:

**Roles**: Named collection of permissions
```bash
etcdctl role add myrole
etcdctl role grant-permission myrole read /foo
etcdctl role grant-permission myrole readwrite /bar
```

**Users**: Assigned one or more roles
```bash
etcdctl user add alice
etcdctl user grant-role alice myrole
```

**Permissions**:
- `read`: Get, watch
- `write`: Put, delete
- `readwrite`: Both read and write

**Key Ranges**: Permissions apply to key ranges
```bash
# Permission on single key
etcdctl role grant-permission myrole read /exact-key

# Permission on key prefix
etcdctl role grant-permission myrole read /prefix/ --prefix=true

# Permission on key range
etcdctl role grant-permission myrole read /start /end
```

**Root User**: Special user with all permissions
- Created during `auth enable`
- Cannot be deleted
- Used for administrative tasks

## Cluster Management

### Cluster Bootstrapping

**Static Bootstrap**: All members known at start
```bash
# Member 1
etcd --name=member1 \
  --initial-cluster=member1=http://host1:2380,member2=http://host2:2380,member3=http://host3:2380 \
  --initial-cluster-state=new

# Member 2  
etcd --name=member2 \
  --initial-cluster=member1=http://host1:2380,member2=http://host2:2380,member3=http://host3:2380 \
  --initial-cluster-state=new

# Member 3
etcd --name=member3 \
  --initial-cluster=member1=http://host1:2380,member2=http://host2:2380,member3=http://host3:2380 \
  --initial-cluster-state=new
```

**Discovery Bootstrap**: Members discover each other via discovery service
```bash
# Generate discovery URL
curl https://discovery.etcd.io/new?size=3

# Start members with discovery URL
etcd --name=member1 --discovery=https://discovery.etcd.io/xxxxx
```

### Adding Members

**1. Add Learner** (recommended):
```bash
etcdctl member add newmember --learner=true --peer-urls=http://newhost:2380
```

**2. Start New Member**:
```bash
etcd --name=newmember \
  --initial-cluster-state=existing \
  --initial-cluster=member1=http://host1:2380,...,newmember=http://newhost:2380
```

**3. Promote Learner to Voting Member**:
```bash
etcdctl member promote <member-id>
```

**Why Learners?**
- Prevents quorum loss during catch-up
- New member doesn't vote until fully synchronized
- Safe to add multiple learners

### Removing Members

```bash
# List members
etcdctl member list

# Remove member
etcdctl member remove <member-id>

# Stop member process
systemctl stop etcd
```

**Quorum Considerations**:
- 3-member cluster: Can remove 1 member safely (quorum: 2)
- 5-member cluster: Can remove 2 members safely (quorum: 3)
- Never remove majority of members simultaneously

### Disaster Recovery

**Scenario**: Lost quorum (majority of members failed).

**Recovery Steps**:

**1. Stop all members**:
```bash
systemctl stop etcd
```

**2. Restore from snapshot on one member**:
```bash
etcdutl snapshot restore snapshot.db \
  --name=member1 \
  --initial-cluster=member1=http://host1:2380 \
  --initial-advertise-peer-urls=http://host1:2380
```

**3. Start restored member**:
```bash
etcd --force-new-cluster
```

**4. Add new members** (follow normal add process).

## Performance Characteristics

### Throughput

**Typical Performance** (on SSD):
- Sequential writes: ~10,000 ops/sec
- Random writes: ~5,000-8,000 ops/sec
- Reads (local): ~100,000+ ops/sec
- Linearizable reads (quorum): ~10,000 ops/sec

**Factors**:
- Disk I/O (WAL fsync is bottleneck)
- Network latency (Raft replication)
- Key/value size
- Number of watchers
- CPU and memory

### Latency

**Write Latency** (p99):
- Local SSD: 10-50ms
- Network SSD: 50-100ms
- HDD: 100-500ms

**Read Latency**:
- Serializable (local): <1ms
- Linearizable (quorum): 10-50ms

**Components**:
- Network RTT: 1-10ms
- Raft replication: 5-20ms
- WAL fsync: 5-20ms (SSD), 50-200ms (HDD)
- BoltDB write: 1-5ms

### Scalability Limits

**Cluster Size**:
- Recommended: 3 or 5 members
- Maximum: 7 members (diminishing returns)
- Larger clusters: Higher latency, lower throughput

**Database Size**:
- Recommended: <8GB
- Warning at: 8GB
- Alarm at: 10GB (default quota)
- Maximum tested: 100GB+

**Watchers**:
- Typical: <10,000 watchers
- Tested: 100,000+ watchers
- Impact: Memory usage, event fanout latency

**Keys**:
- Millions of keys supported
- Watch performance degrades with many keys per prefix
- Compaction critical for large keyspaces

### Optimization Techniques

**1. Use SSDs**: Dramatic improvement in write latency.

**2. Dedicated Disk**: Don't share disk with other I/O-intensive apps.

**3. Tune OS**:
```bash
# Increase file descriptors
ulimit -n 65536

# Disable swap
swapoff -a

# I/O scheduler
echo noop > /sys/block/sda/queue/scheduler
```

**4. etcd Configuration**:
```bash
# Snapshot less frequently (reduce I/O)
--snapshot-count=50000

# Larger request size limit
--max-request-bytes=10485760

# Auto-compaction
--auto-compaction-mode=periodic
--auto-compaction-retention=5m
```

**5. Client Best Practices**:
- Use serializable reads when possible
- Batch operations in transactions
- Use prefix watches instead of many individual watches
- Close watchers when done
- Reuse client connections

## Failure Modes and Recovery

### Single Node Failure

**3-Member Cluster**:
- Quorum: 2 nodes
- Healthy nodes: 2
- Status: **Operational**
- Behavior: Cluster continues, leader election if leader failed

**5-Member Cluster**:
- Quorum: 3 nodes
- Healthy nodes: 4
- Status: **Operational**
- Behavior: Cluster continues normally

**Recovery**: Replace failed node with new member.

### Quorum Loss

**3-Member Cluster** (2 failures):
- Quorum: 2 nodes
- Healthy nodes: 1
- Status: **Unavailable**
- Behavior: Reads may work (serializable), writes fail

**5-Member Cluster** (3 failures):
- Quorum: 3 nodes
- Healthy nodes: 2
- Status: **Unavailable**

**Recovery**: Restore from snapshot or repair members.

### Network Partition

**Scenario**: 3-member cluster splits into [2] and [1].

**Majority Partition [2]**:
- Has quorum
- Elects leader
- Accepts writes
- Operational

**Minority Partition [1]**:
- No quorum
- Cannot elect leader
- Rejects writes
- Serves stale serializable reads

**Recovery**: When partition heals
- Minority rejoins
- Syncs with leader
- Resumes normal operation

### Disk Failure

**Symptoms**:
- Slow I/O
- WAL write errors
- Backend commit timeouts
- Member drops out of cluster

**Recovery**:
1. Stop member
2. Replace disk
3. Restore from snapshot OR
4. Remove and re-add member

### Database Corruption

**Detection**:
- Hash mismatch errors
- Backend corruption errors
- Cluster consistency check failures

**Recovery**:
1. Identify corrupt member
2. Stop corrupt member
3. Restore from snapshot
4. Restart member

**Prevention**:
- Use ECC memory
- Validate backups regularly
- Monitor cluster health

### Split-Brain Prevention

**Raft Guarantees**:
- Only one leader per term
- Leader requires majority votes
- Two partitions cannot both have quorum

**Example**: 3-node cluster splits [2] vs [1]
- Partition [2]: Can elect leader, form quorum
- Partition [1]: Cannot elect leader, no quorum
- **No split-brain possible**

## Design Decisions

### Why Raft Instead of Paxos?

**Reasons**:
- **Understandability**: Raft is easier to understand and implement
- **Strong leader**: Simplifies log management
- **Modularity**: Separate leader election, log replication, safety
- **Proof of correctness**: Formally verified safety properties

**Trade-offs**:
- Paxos may have slightly better performance in some scenarios
- Raft's strong leader can be a bottleneck

### Why BoltDB?

**Reasons**:
- **Embedded**: No separate database process
- **ACID**: Strong consistency guarantees
- **MVCC**: Perfect fit for etcd's needs
- **Memory-mapped**: Efficient reads
- **Simple**: Easy to understand and debug

**Trade-offs**:
- Single-writer (all writes through one goroutine)
- File size growth requires defragmentation
- Not optimized for very large datasets (>100GB)

### Why gRPC?

**Reasons**:
- **Performance**: Binary protocol, HTTP/2 multiplexing
- **Type Safety**: Protocol buffers with generated code
- **Streaming**: Bi-directional streaming for watch
- **Cross-language**: Clients in any language
- **Built-in**: Authentication, load balancing, timeouts

**Trade-offs**:
- More complex than REST
- Requires HTTP/2
- Less human-readable than JSON

### Why MVCC?

**Reasons**:
- **Watch**: Enables efficient watch from any revision
- **Transactions**: Snapshot isolation for txns
- **History**: Point-in-time queries
- **Kubernetes**: Matches Kubernetes resourceVersion semantics

**Trade-offs**:
- Storage overhead (multiple versions)
- Requires compaction to reclaim space
- More complex than simple key-value

## Deployment Topology

### Development (1 Node)

```
┌──────────────┐
│  etcd-1      │
│  (Single)    │
└──────────────┘
```

**Use**: Local development, testing
**Fault Tolerance**: None
**Performance**: Full read/write speed

### Production (3 Nodes)

```
┌──────────────┐      ┌──────────────┐      ┌──────────────┐
│  etcd-1      │◄────►│  etcd-2      │◄────►│  etcd-3      │
│  (Leader)    │      │  (Follower)  │      │  (Follower)  │
└──────────────┘      └──────────────┘      └──────────────┘
```

**Use**: Small production clusters
**Fault Tolerance**: 1 node failure
**Quorum**: 2 nodes

### High Availability (5 Nodes)

```
┌──────────────┐      ┌──────────────┐      ┌──────────────┐
│  etcd-1      │◄────►│  etcd-2      │◄────►│  etcd-3      │
│  (Leader)    │      │  (Follower)  │      │  (Follower)  │
└──────────────┘      └──────────────┘      └──────────────┘
       ▲                                             ▲
       │                                             │
       ▼                                             ▼
┌──────────────┐                            ┌──────────────┐
│  etcd-4      │◄──────────────────────────►│  etcd-5      │
│  (Follower)  │                            │  (Follower)  │
└──────────────┘                            └──────────────┘
```

**Use**: Large production clusters
**Fault Tolerance**: 2 node failures
**Quorum**: 3 nodes

### Multi-Region (5 Nodes)

```
Region 1              Region 2              Region 3
┌──────────┐         ┌──────────┐         ┌──────────┐
│  etcd-1  │◄───────►│  etcd-2  │◄───────►│  etcd-3  │
│(Follower)│         │ (Leader) │         │(Follower)│
└──────────┘         └──────────┘         └──────────┘
                            ▲
                            │
                            ▼
Region 1              ┌──────────┐         Region 3
┌──────────┐         │  etcd-4  │         ┌──────────┐
│  etcd-5  │◄───────►│(Follower)│◄───────►│  (etc)   │
│(Follower)│         └──────────┘         │          │
└──────────┘         Region 2             └──────────┘
```

**Use**: Global availability
**Considerations**:
- Higher latency (cross-region)
- Place majority in low-latency region
- Consider network costs

### Kubernetes/OpenShift

```
┌─────────────────────────────────────────────┐
│           Kubernetes/OpenShift Cluster      │
│                                             │
│  ┌────────────────────────────────────────┐ │
│  │  Control Plane Nodes                   │ │
│  │                                         │ │
│  │  ┌──────────┐  ┌──────────┐  ┌───────┐│ │
│  │  │  etcd-1  │  │  etcd-2  │  │ etcd-3││ │
│  │  │(Static   │  │(Static   │  │(Static││ │
│  │  │ Pod)     │  │ Pod)     │  │ Pod)  ││ │
│  │  └──────────┘  └──────────┘  └───────┘│ │
│  │       ▲              ▲              ▲  │ │
│  └───────┼──────────────┼──────────────┼──┘ │
│          │              │              │    │
│  ┌───────▼──────────────▼──────────────▼──┐ │
│  │  kube-apiserver instances              │ │
│  │  (read/write cluster state to etcd)    │ │
│  └────────────────────────────────────────┘ │
└─────────────────────────────────────────────┘
```

**Characteristics**:
- etcd runs as static pods
- Co-located with kube-apiserver
- Dedicated data directory (hostPath)
- Separate network for peer communication

---

**Document Version**: 1.0  
**Last Updated**: 2026-06-25  
**Maintained By**: OpenShift etcd Team
