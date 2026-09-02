# AI agent guide for the openshift/etcd fork

Quick reference for AI agents and contributors working on `openshift/etcd`, the
OpenShift downstream fork of upstream [etcd](https://github.com/etcd-io/etcd).

For deeper reading:

- [ARCHITECTURE.md](./ARCHITECTURE.md) - how upstream etcd works internally
  (Raft, MVCC, storage, watches, leases, TLS).
- [docs/openshift-downstream.md](./docs/openshift-downstream.md) - how this fork
  differs from upstream and how it is deployed in OpenShift.
- [REBASE.openshift.md](./REBASE.openshift.md) - the rebase workflow used to keep
  the fork current.

## What this repo is

This is upstream etcd plus a set of carried OpenShift patches. The most important
thing to understand is the deployment model:

- Upstream etcd is a binary you start directly with flags and env vars.
- In OpenShift, etcd runs as a **static pod** managed by the
  [cluster-etcd-operator](https://github.com/openshift/cluster-etcd-operator)
  (CEO). This repo does not own that pod definition; it produces the `etcd`,
  `etcdctl`, and `etcdutl` binaries plus the `openshift-tools/` helpers (notably
  `discover-etcd-initial-cluster`) that CEO's pod invokes.

See [docs/openshift-downstream.md](./docs/openshift-downstream.md) for details.

## Critical rules

1. **Follow the commit-prefix convention.** Downstream commits are labeled so
   rebases can carry or drop them: `UPSTREAM: <carry>` (temporary carry, hope to
   upstream), `UPSTREAM: <drop>` (carry that will never go upstream),
   `DOWNSTREAM: <carry>` (OpenShift-specific). Preserve these prefixes.
2. **Rebase with the script, not by hand.** Use `openshift-hack/rebase.sh`; see
   [REBASE.openshift.md](./REBASE.openshift.md).
3. **Never edit generated code.** Edit the `.proto` and run `make genproto`;
   never hand-edit `*.pb.go`. Never edit `vendor/`.
4. **Do not break the API.** Never remove or rename API fields; deprecate instead.
5. **All persistent state goes through Raft.** Use `s.r.Propose()`; never mutate
   state directly or assume leadership.
6. **bbolt is replaced downstream.** `go.mod` has a `replace` directive pointing
   `go.etcd.io/bbolt` at `openshift/bbolt`. Do not remove it; see the bbolt
   section of [REBASE.openshift.md](./REBASE.openshift.md).
7. **All changes need tests.**

## Repository structure

```text
openshift-tools/        # Downstream helper binaries baked into the etcd image
  discover-etcd-initial-cluster/   # Computes ETCD_INITIAL_CLUSTER at pod start
openshift-hack/         # Downstream tooling, incl. rebase.sh
server/etcdserver/      # Core server: server.go, raft.go, apply*.go
server/storage/         # mvcc/, backend/ (bbolt), wal/
client/v3/              # Go client
api/etcdserverpb/       # gRPC API (edit rpc.proto, then `make genproto`)
etcdctl/                # CLI
etcdutl/                # Offline utilities (defrag, snapshot)
```

The repo is a multi-module workspace (`.`, `server`, `etcdutl`, `tests`, and
others) wired together with `replace` directives in `go.mod`.

## Downstream differences from upstream

The distinguishing content of this fork lives in the carried patches. The full,
current list is the git history filtered by the `DOWNSTREAM:` / `UPSTREAM:`
prefixes. The most significant ones:

- **`discover-etcd-initial-cluster`** - computes `ETCD_INITIAL_CLUSTER` live at
  pod start by querying the running cluster and inspecting the local data dir.
  This is how members bootstrap, scale up, and recover after a restore.
- **openshift/bbolt fork** - removes a `madvise(MADV_RANDOM)` call to fix a
  RHEL 10 page-cache regression (OCPBUGS-103516).
- **Learner flags / env vars** - downstream settings for adding members as
  learners before promotion.
- **force-new-cluster revision bumping / cluster ID in WAL** (ETCD-696) and
  **quorum-restore data dir archiving** (ETCD-656).

See [docs/openshift-downstream.md](./docs/openshift-downstream.md) for the
deep dive.

## Build and test

```bash
./build.sh          # builds bin/{etcd,etcdctl,etcdutl,discover-etcd-initial-cluster}
make build          # standard upstream build
make test           # unit tests
make test-integration
make test-e2e
make verify         # linters and generated-file checks
```

The `Dockerfile*` files copy the built binaries (including
`discover-etcd-initial-cluster`) into `/usr/bin/` of the etcd image that CEO
runs.

CI (comment on the PR):

```text
/payload 4.x nightly informing
/payload 4.x nightly blocking
```

See [REBASE.openshift.md](./REBASE.openshift.md) for ClusterBot and performance
testing.

## Agent rules

### Always

- Keep backwards compatibility; deprecate rather than remove.
- Route persistent state changes through Raft.
- Add tests with every change; run `go test -race` on concurrency changes.
- Use the zap logger (`lg.Info(...)`), not `fmt.Println`.

### Never

- Edit generated code (`*.pb.go`) or `vendor/`.
- Remove the bbolt `replace` directive or the downstream commit prefixes.
- Break or remove API fields.
- Block the Raft apply loop.

### Ask first

- Changes to Raft, the storage format, or on-disk layout (need a migration plan).
- New dependencies (license, security, maintenance).
- Anything that changes the contract between this repo and the
  cluster-etcd-operator (e.g. `discover-etcd-initial-cluster` flags or output).

## Key files

- `openshift-tools/pkg/discover-etcd-initial-cluster/initial-cluster.go` - bootstrap logic
- `openshift-hack/rebase.sh` - rebase automation
- `REBASE.openshift.md` - rebase and bbolt-fork procedures
- `build.sh` - builds all binaries, including the downstream helpers
- `Dockerfile.rhel` - assembles the etcd image
- `server/etcdserver/server.go` - core server
- `api/etcdserverpb/rpc.proto` - gRPC API source of truth
- `go.mod` - module wiring and the bbolt `replace` directive

## Resources

- [etcd.io docs](https://etcd.io/docs)
- [Raft paper](https://raft.github.io/raft.pdf)
- [cluster-etcd-operator](https://github.com/openshift/cluster-etcd-operator)
- [Upstream etcd](https://github.com/etcd-io/etcd)
