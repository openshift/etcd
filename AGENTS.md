# AGENTS.md - AI Agent Guide for etcd

Quick reference for AI agents working on the OpenShift etcd fork.

## Overview

**etcd**: Distributed key-value store using Raft consensus, ~10K writes/sec, MVCC storage. Single source of truth for Kubernetes/OpenShift cluster state.

**OpenShift Fork**: Branch `openshift-X.Y` (not `main`). Commit prefixes: `UPSTREAM: <carry>:`, `DOWNSTREAM:`. See [REBASE.openshift.md](./REBASE.openshift.md).

## Architecture

### Core Components
- **Raft Consensus** (`server/etcdserver/raft.go`, `server.go`) - All state changes via async proposals: `s.w.Register(id)` → `s.r.Propose(ctx, data)` → wait
- **MVCC Storage** (`server/storage/mvcc/`, `backend/`) - Revision (global), Version (per-key), Compaction, Defrag
- **Watches** (`server/storage/mvcc/watchable_store.go`) - Real-time key change notifications
- **Leases** (`server/lease/lessor.go`) - Time-bound key ownership
- **gRPC API** (`api/etcdserverpb/rpc.proto`) - Edit `.proto` → `make genproto` → Never edit `*.pb.go`

### Key Directories
```
server/etcdserver/          # Core: server.go, raft.go, apply*.go
server/storage/mvcc/        # MVCC, compaction
server/storage/backend/     # BoltDB, defrag
server/storage/wal/         # Write-Ahead Log
server/etcdserver/api/v3compactor/  # Auto-compaction
client/v3/                  # Go client
etcdctl/                    # CLI
etcdutl/                    # Utilities (defrag, snapshot)
```

## Operations

### Compaction & Defrag
**Compaction**: Removes old revisions, marks space free (doesn't reclaim disk)
```bash
etcd --auto-compaction-mode=periodic --auto-compaction-retention=5m
```

**Defragmentation**: Rewrites DB to reclaim space, blocks writes, needs ~2x DB memory
```bash
etcdctl defrag                                    # Online: 30s-5min
etcdutl defrag --data-dir=/path                   # Offline: faster, requires stop
```
**Trigger**: When `(db_total_size - db_size_in_use) / db_total_size > 30%`

**Files**: `server/storage/mvcc/kvstore_compaction.go`, `server/storage/backend/backend.go`

### Backup & Restore

**Snapshot Save** (online, 32KB chunks, SHA256):
```bash
etcdctl snapshot save backup.db
etcdctl snapshot status backup.db --write-out=table
```
**Files**: `etcdctl/ctlv3/command/snapshot_command.go`, `client/v3/snapshot/v3_snapshot.go`, `server/etcdserver/api/v3rpc/maintenance.go`

**Snapshot Restore** (offline, requires stop):
```bash
etcdutl snapshot restore backup.db --data-dir=/var/lib/etcd-restore \
  --name member1 --initial-cluster member1=http://host1:2380,...
```
**Process**: Verify SHA256 → Copy DB → Trim membership → Create WAL/snapshot → Update index  
**Files**: `etcdutl/snapshot/v3_snapshot.go`, `server/etcdserver/bootstrap.go`

**WAL Replay** (automatic on startup, CRC32 validation, auto-repairs torn writes):  
**Files**: `server/storage/wal/wal.go` (`ReadAll()`), `server/storage/wal/repair.go`

**Disk Layout**:
```
/var/lib/etcd/member/
├── snap/{term}-{index}.snap, db, {index}.snap.db
└── wal/{seq}-{index}.wal
```

### TLS & Certificates

**Setup** (Client/Peer/Metrics TLS):
```bash
etcd --cert-file=/path/server.crt --key-file=/path/server.key --client-cert-auth \
     --peer-cert-file=/path/peer.crt --peer-key-file=/path/peer.key
```

**Features**: Client cert auth, CN/SAN validation, CRL support, dynamic reload (no restart), auto-TLS (dev only)

**Files**:
- Config: `server/embed/config.go`, `server/embed/etcd.go`
- Loading: `client/pkg/tlsutil/tlsutil.go`, `client/pkg/transport/listener.go`
- Client: `client/v3/config.go`, `server/etcdserver/api/v3rpc/grpc.go`
- Peer: `server/etcdserver/api/rafthttp/transport.go`
- Validation: `client/pkg/transport/listener_tls.go` (CRL, SAN)

**Enhancement Areas**: Proactive cert reload (inotify, SIGHUP), TLS metrics, OCSP stapling

### I/O Performance

**Critical Paths**:
- **WAL**: fsync when `raft.MustSync()` true (target: P99 < 10ms)
- **Backend**: Batched commits every 100ms/10K txns (target: P99 < 25ms)

**Tuning**:
```bash
etcd --wal-dir=/mnt/nvme/etcd-wal --data-dir=/mnt/ssd/etcd-data \
     --backend-batch-interval=100ms --backend-batch-limit=10000 \
     --snapshot-count=10000
```

**Requirements**: SSD (NVMe preferred), dedicated disk, benchmark with `fio --rw=write --ioengine=sync --fdatasync=1 --size=22m --bs=2300`

## Development

### Workflows
- **API Feature**: Edit `.proto` → `make genproto` → Implement in `server/etcdserver/api/v3rpc/` → Client in `client/v3/` → Tests
- **Bug Fix**: Failing test → Minimal fix → `make test-unit PKG=./server/...` → `go test -race -count=100`
- **Performance**: Baseline → Profile (`-cpuprofile`) → Optimize → Document metrics

### Testing
```bash
make test-unit                    # Fast, isolated
make test-integration             # Real server + clients
make test-e2e                     # Real processes
go test -race -count=100 ./...    # Race detection
make verify                       # Linters
```
**Checklist**: Unit + integration (API changes) + E2E (features) + `-race` passes

## Critical Rules

### ALWAYS
1. Backwards compatibility - Never remove/rename API fields
2. Use Raft for state - All persistent changes via Raft
3. Handle errors - Check all returns, use zap logging
4. Add tests - All changes require tests
5. Profile first - Measure before optimizing

### NEVER
1. Modify Raft directly - Use `s.r.Propose()`
2. Block Raft apply loop - Keep it fast
3. Edit generated code - Edit `.proto`, run `make genproto`
4. Break API - Deprecate, don't remove
5. Commit without tests
6. Use `fmt.Println` - Use `lg.Info()` (zap)
7. Assume leadership - Always propose via Raft
8. Disable tests - Fix or file issue
9. Unbounded allocations - Max 1.5MB request
10. Panic in library - Return errors

### Ask First
- Raft changes: Consult maintainers, read [Raft paper](https://raft.github.io/raft.pdf)
- Dependencies: License, security, maintenance
- Breaking changes: Can it be compatible?
- Performance: Have benchmarks
- Storage format: Migration plan

## Common Mistakes

1. **Raft Flow**: Async (Propose → Replicate → Commit → Apply)
2. **Revision vs Version**: Revision=global, Version=per-key
3. **Context**: Check `<-ctx.Done()` in loops
4. **Defrag**: Needs ~2x DB memory, blocks writes
5. **Compaction**: Handle `ErrCompacted` on old revisions
6. **Transactions**: Use `Txn()`, not Get+Put
7. **Resources**: `defer cli.Close()`
8. **Consistency**: Linearizable (slow) vs Serializable (fast, stale)

## Key Metrics

**Critical Alerts**:
```promql
etcd_disk_wal_fsync_duration_seconds{quantile="0.99"} > 0.01              # Disk slow
etcd_mvcc_db_total_size_in_bytes / etcd_server_quota_backend_bytes > 0.8  # Near quota
(etcd_mvcc_db_total_size_in_bytes - etcd_mvcc_db_total_size_in_use_in_bytes) 
  / etcd_mvcc_db_total_size_in_bytes > 0.3                                # High fragmentation
etcd_server_proposals_pending > 100                                       # Raft slow
rate(etcd_server_leader_changes_seen_total[5m]) > 3                       # Unstable leader
```

**Other Key Metrics**:
```
etcd_disk_backend_commit_duration_seconds     # Backend commit latency
etcd_server_proposals_committed_total         # Raft proposals committed
etcd_debugging_snap_save_total_duration_seconds  # Snapshot save time
```

**Access**: `curl http://localhost:2379/metrics` or `etcdctl endpoint status --write-out=table`

## Configuration Defaults

| Setting | Default | File |
|---------|---------|------|
| Snapshot count | 10,000 | `DefaultSnapshotCount`, `server/etcdserver/server.go` |
| Backend batch interval | 100ms | `defaultBatchInterval`, `server/storage/backend/backend.go` |
| Backend batch limit | 10,000 | `defaultBatchLimit`, `server/storage/backend/backend.go` |
| Database quota | 2GB (OpenShift: 8GB) | `DefaultQuotaBytes`, `server/storage/quota.go` |
| Max request size | 1.5MB | `DefaultMaxRequestBytes`, `server/embed/config.go` |

## OpenShift

**Commit Prefixes**: `UPSTREAM: <carry>:` (temporary), `UPSTREAM: <drop>:` (downstream-only), `DOWNSTREAM:` (OpenShift-specific)

**CI**: `/payload 4.17 nightly informing`, `/payload 4.17 nightly blocking`, `launch openshift/etcd#PR`

**Rebase**: `openshift-hack/rebase.sh --etcd-tag=v3.5.15 --openshift-release=openshift-4.17 --jira-id=12345`

## Resources

- [etcd.io/docs](https://etcd.io/docs) - Official docs
- [etcd Metrics](https://etcd.io/docs/v3.5/metrics/) - Full metrics
- [Raft Paper](https://raft.github.io/raft.pdf) - Consensus algorithm
- [REBASE.openshift.md](./REBASE.openshift.md) - Rebase procedures
- [OpenShift etcd Practices](https://docs.redhat.com/en/documentation/openshift_container_platform/4.20/html/etcd/etcd-practices)

**Tools**: `etcdctl` (CLI), `etcdutl` (defrag/snapshot), `benchmark` (perf testing)

---

**Version**: 4.0 (Final)  
**Last Updated**: 2026-06-26  
**Verified**: Configs/metrics verified against codebase and [official docs](https://etcd.io/docs/v3.5/metrics/)
