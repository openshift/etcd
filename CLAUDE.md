# Claude Code Instructions for openshift/etcd

## Repository Overview

This is the OpenShift fork of [etcd-io/etcd](https://github.com/etcd-io/etcd). It tracks upstream etcd releases with downstream patches applied on top for OpenShift integration.

### Remotes

- `origin` = `git@github.com:openshift/etcd` (the OpenShift fork)
- `upstream` = `https://github.com/etcd-io/etcd` (upstream etcd)
- User forks are added as needed for PRs

### Branch Naming

- `openshift-4.XX` branches track specific OpenShift releases
- Rebase branches follow the pattern `rebase-etcd-<version>-openshift-<release>`

## Upstream Rebase Procedure

When asked to rebase to a new upstream etcd version (e.g., "rebase v3.6.9"), follow these steps:

### 1. Identify the Current Upstream Base

Find which upstream tag the current branch is based on. Look for a commit like `version: bump up to 3.6.X` in the log, or check the most recent upstream tag that is an ancestor:

```
git log --oneline | grep "version: bump up to"
```

### 2. Identify Downstream Patches

List all commits on top of the current upstream base:

```
git log --oneline <current-upstream-tag>..HEAD --no-merges
```

Categorize each commit:

- **`DOWNSTREAM: <carry>:`** - Must be cherry-picked onto the new base. These are ongoing downstream patches.
- **`DOWNSTREAM: <drop>:`** - Skip these. They were fixups specific to the previous upstream version.
- **Merge commits** - Skip these. They will be replaced by the new rebase structure.
- **Other downstream commits** (ART image updates, Dockerfile changes, Konflux/cachi2 support, etc.) - Carry these forward unless they are clearly version-specific.

### 3. Create the Rebase Branch

```
git checkout -b rebase-etcd-<new-version>-openshift-<release> <new-upstream-tag>
```

### 4. Cherry-pick Carry Patches

Cherry-pick in chronological order (oldest first). Resolve conflicts as they arise, preferring the new upstream version for any upstream code and preserving downstream intent for downstream code:

```
git cherry-pick <commit-sha>
```

### 5. Merge the Target Branch to Resolve PR Conflicts

After cherry-picking all carry patches, the PR will likely have merge conflicts because both branches (the target and the rebase) contain the same downstream files applied on different upstream bases. To fix this, merge the target branch into the rebase branch, resolving all conflicts by keeping the rebased (ours) version:

```
git merge origin/openshift-4.XX --no-commit
git checkout --ours <conflicted-files>
git add <conflicted-files>
git commit
```

This merge commit ensures the PR can be merged cleanly on GitHub.

### 6. Create a `DOWNSTREAM: <drop>` Fixup Commit

After cherry-picking and merging, you almost certainly need a fixup commit to resolve build failures caused by upstream API changes. Common issues:

**Moved/renamed packages:** Upstream refactors move packages between versions. Check for import errors and find the new locations using `find` or `ls`. Known moves in recent versions:
- `server/datadir` → `server/storage/datadir`
- `server/wal` → `server/storage/wal`
- `server/wal/walpb` → `server/storage/wal/walpb`
- `pkg/grpc_testing` → `pkg/grpctesting` (also `GrpcRecorder` → `GRPCRecorder`)
- `server/etcdserver/api/v2http` was removed; use `etcdhttp` handlers instead

**Removed/renamed functions:** Check the upstream equivalent code (e.g., `tests/framework/integration/cluster.go` for patterns) when a function like `toErr` is missing — it may have been renamed (e.g., to `ContextError`).

**Missing imports:** Downstream carry patches may add code using stdlib packages (like `os`, `strconv`) that weren't needed in the original file. If upstream refactored imports, those may be lost.

**Dockerfile/CI builder updates:** All `Dockerfile.*` and `.ci-operator.yaml` files reference specific Go versions and OpenShift release versions in their `FROM` lines. These must be updated to match the target OpenShift release and the Go version required by the new upstream tag.

**Do NOT downgrade the Go version:** If the new upstream tag requires a newer Go version (e.g., 1.25), do not attempt to downgrade it. The `go` directive in `go.mod` will be bumped back by `go mod tidy` because upstream dependencies require it.

Build and iterate:
```
make build
go build -v ./openshift-tools/...
```

Commit all fixes as `DOWNSTREAM: <drop>: <version> fixup`.

### 7. Push and Create PR

Push the rebase branch to a personal fork and create a PR against the target `openshift-4.XX` branch.

## Downstream Commit Message Conventions

- `DOWNSTREAM: <carry>: <JIRA-ID>: description` - Patches carried across rebases
- `DOWNSTREAM: <drop>: description` - One-time fixups dropped on next rebase
- `DOWNSTREAM <carry>: description` - Also valid (note: no colon after DOWNSTREAM)

## Build and Test

- Build: `make build` (builds etcd, etcdctl, etcdutl)
- Build downstream tools: `go build -v ./openshift-tools/...`
- Test: `make test`
- The `make build` target also builds etcdctl and etcdutl, so a successful `make build` covers the main binaries
- The `yamlfmt` warning during build is cosmetic and can be ignored

## Key Downstream Files

These files are downstream-only and don't exist in upstream etcd:

- `Dockerfile.art`, `Dockerfile.rhel`, `Dockerfile.installer` - OpenShift container builds
- `Dockerfile.art-cachi2`, `Dockerfile.installer.art-cachi2` - Konflux/cachi2 builds
- `.ci-operator.yaml` - CI configuration
- `build.sh` - OpenShift build script
- `openshift-hack/` - Downstream rebase tooling
- `openshift-tools/` - Downstream tools (e.g., discover-etcd-initial-cluster)
- `client/v3/patch_cluster.go` - Downstream client patch (NonLinearizeableMemberList)
- `server/revbump/` - Downstream revision bumping for force-new-cluster
- `server/config/config.go` - Has downstream additions (OPENSHIFT_ETCD_HARDWARE_DELAY_TIMEOUT)
- `tests/integration/cluster.go` - Downstream test cluster helper (references upstream internal APIs that change between versions)
- `REBASE.openshift.md` - Additional rebase documentation

## Known Fragile Downstream Files

These files reference upstream internal APIs and are most likely to break during a rebase:

- `tests/integration/cluster.go` - Large downstream carry that mirrors `tests/framework/integration/cluster.go`. When imports break, check the upstream framework version for the current API.
- `client/v3/patch_cluster.go` - Uses internal client functions that may be renamed upstream.
- `openshift-tools/pkg/discover-etcd-initial-cluster/walutil.go` - Imports server-internal packages (wal, datadir, snap) that move during upstream refactors.
