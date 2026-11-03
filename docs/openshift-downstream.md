# OpenShift downstream differences

This document describes how `openshift/etcd` differs from upstream
`etcd-io/etcd`. `ARCHITECTURE.md` covers how upstream etcd works internally; this
document covers the OpenShift-specific pieces layered on top.

Most downstream changes are carried patches, tracked by commit-message
convention (`UPSTREAM: <carry>`, `UPSTREAM: <drop>`, `DOWNSTREAM: <carry>`) and
maintained across rebases. See [REBASE.openshift.md](../REBASE.openshift.md) for
the rebase workflow. The authoritative list of downstream changes is the git
history filtered by those prefixes; this document explains the ones that are the
hardest to understand from the code alone.

## Deployment model

The single biggest difference is how etcd is run. Upstream etcd is a binary an
operator starts directly (or via systemd), passing it flags and environment
variables such as `ETCD_INITIAL_CLUSTER`.

In OpenShift, etcd is **not** started directly. The
[cluster-etcd-operator](https://github.com/openshift/cluster-etcd-operator)
(CEO) renders and manages an etcd **static pod** on each control-plane node
(pod template:
[`bindata/etcd/pod.gotpl.yaml`](https://github.com/openshift/cluster-etcd-operator/blob/master/bindata/etcd/pod.gotpl.yaml)).

This repository does not contain that pod definition. What it contributes to the
OpenShift deployment is:

- the `etcd`, `etcdctl`, and `etcdutl` binaries, and
- the helper binaries under `openshift-tools/`, most importantly
  `discover-etcd-initial-cluster`.

`build.sh` builds all of these into `bin/`, and the `Dockerfile*` files copy them
into `/usr/bin/` of the etcd image that CEO runs. The rest of this document
focuses on `discover-etcd-initial-cluster`, since its role in bootstrapping is
the least obvious from the source.

## discover-etcd-initial-cluster

Source lives in this repo:

- `openshift-tools/discover-etcd-initial-cluster/main.go` (binary entrypoint)
- `openshift-tools/pkg/discover-etcd-initial-cluster/initial-cluster.go` (logic)
- `openshift-tools/pkg/discover-etcd-initial-cluster/walutil.go` (WAL parsing)
- `openshift-tools/pkg/discover-etcd-initial-cluster/initial-cluster_test.go`

The canonical operator-side documentation is in the CEO repo:
[`docs/discover-etcd-initial-cluster.md`](https://github.com/openshift/cluster-etcd-operator/blob/master/docs/discover-etcd-initial-cluster.md).

### Why it exists

A static pod manifest is rendered ahead of time and cannot know the live peer
membership of the etcd cluster, which changes as members are scaled up, replaced,
or recovered after a quorum loss. Upstream etcd expects whoever launches it to
supply `ETCD_INITIAL_CLUSTER`. In OpenShift that value is instead computed
**live, at pod start**, by contacting the running cluster and inspecting the
local data directory. `discover-etcd-initial-cluster` is the tool that does this.

### Where it runs in the pod

The three init containers (`setup`, `etcd-ensure-env-vars`,
`etcd-resources-copy`) run first. Then, inside the main `etcd` container's
`/bin/sh -c` command wrapper, `discover-etcd-initial-cluster` runs and its
**stdout** is captured into `ETCD_INITIAL_CLUSTER` **before** the wrapper finally
`exec`s the etcd binary:

```bash
# from cluster-etcd-operator bindata/etcd/pod.gotpl.yaml (abridged)
ETCD_INITIAL_CLUSTER=$(discover-etcd-initial-cluster \
  --cacert=/etc/kubernetes/static-pod-certs/configmaps/etcd-all-bundles/server-ca-bundle.crt \
  --cert=/etc/kubernetes/static-pod-certs/secrets/etcd-all-certs/etcd-peer-NODE_NAME.crt \
  --key=/etc/kubernetes/static-pod-certs/secrets/etcd-all-certs/etcd-peer-NODE_NAME.key \
  --endpoints=${ALL_ETCD_ENDPOINTS} \
  --data-dir=/var/lib/etcd \
  --target-peer-url-host=${NODE_NODE_ENVVAR_NAME_ETCD_URL_HOST} \
  --target-name=NODE_NAME)
export ETCD_INITIAL_CLUSTER
# ... later ...
exec etcd ...
```

### Flags

All flags are required (`Validate()`); `TargetPeerURLScheme` (`https`) and
`TargetPeerURLPort` (`2380`) are hardcoded defaults, not flags.

| Flag | Purpose |
| ---- | ------- |
| `--cacert` | CA bundle used to verify the etcd server |
| `--cert` | client cert used to authenticate to etcd |
| `--key` | client key |
| `--endpoints` | comma-separated etcd endpoints to contact |
| `--data-dir` | data dir to stat for an existing `member/` directory |
| `--target-peer-url-host` | this member's peer-URL host (IP or hostname), used to match against the member list |
| `--target-name` | name to assign to this peer if it needs to be created |

### How it works

1. Stat `<data-dir>/member/snap` to decide whether a local data dir already
   exists.
2. Build a TLS etcd client (fail-fast, 2s dial timeout). If the client cannot be
   created **but a data dir already exists**, assume this member was already
   initialized (single-node cluster or a transient network problem) and exit `0`
   with empty output so etcd starts normally. If the client fails and there is no
   data dir, return an error so the container restarts and retries.
3. Determine the local cluster ID via `findLocalClusterIdentifier()`: read
   `<data-dir>/revision.json` (fast path), falling back to parsing the WAL
   (`walutil.go`).
4. Loop for up to 135 attempts. Each iteration fetches the current membership
   (`NonLinearizeableMemberList`) and matches this node against it by peer URL
   (`checkTargetMember`), then decides what to print. Membership-list errors
   retry immediately; when the member is not found, the helper sleeps one
   second before retrying.

### Decision matrix

`getInitialCluster()` decides the outcome from three inputs: whether this member
is in the member list (and whether it has started, indicated by a non-empty
`Name`), whether a local data dir exists, and whether the live cluster ID differs
from the local one.

| Member in list | Local data dir | Extra condition | Outcome |
| -------------- | -------------- | --------------- | ------- |
| Yes, unstarted | No | | Print the computed `ETCD_INITIAL_CLUSTER`; etcd joins as a new member |
| Yes, unstarted | Yes | | Archive the data dir, exit non-zero so the container restarts |
| Yes, started | Yes | | Print empty string; etcd restarts with its existing state |
| Yes, started | No | | Exit non-zero: data dir destroyed, member must be removed from the cluster |
| No | Yes | cluster ID mismatch | Archive the data dir and signal etcd to start (member was removed, e.g. after a restore) |
| No | Yes | cluster ID matches | Exit non-zero: possible scaling problem, data dir must be removed manually |
| No | No | | Retry; times out with an error after 135 attempts |

### Output contract

- Exactly one line is written to **stdout**: the value for `ETCD_INITIAL_CLUSTER`,
  formatted as comma-separated `<name>=<peerURL>` pairs with this member appended
  when it is unstarted (`formatInitialCluster`).
- An **empty string is a valid result**: it means "start with the existing
  configuration" rather than bootstrapping a fresh member.
- All diagnostics and logging go to **stderr** so they do not pollute the
  captured environment variable.
- A non-zero exit restarts the container.
- When a data dir must be discarded, `archiveDataDir` renames
  `member` to `member-removed-archive-<timestamp>` instead of deleting it. This
  is what lets a member rejoin cleanly after a quorum restore.

## Other downstream patches

These are the other notable downstream differences. They are listed here as
pointers; expand them in follow-up changes as needed.

- **Learner flags / env vars** - downstream-specific flags and environment
  variables used when adding members as learners before promotion.
- **openshift/bbolt fork** - a `go.mod` `replace` redirects `go.etcd.io/bbolt`
  to a patched `openshift/bbolt` that removes a `madvise(MADV_RANDOM)` call to
  fix a RHEL 10 page-cache regression (OCPBUGS-103516). See
  [REBASE.openshift.md](../REBASE.openshift.md).
- **force-new-cluster revision bumping / cluster ID in WAL** (ETCD-696) - support
  for tracking the cluster ID in the WAL and revision file, used by
  `discover-etcd-initial-cluster` and quorum recovery.
- **Quorum-restore data dir archiving** (ETCD-656) - automation around moving the
  data dir aside during a quorum restore, which the archiving behavior above
  supports.

The complete, current list is always the git history filtered by the
`DOWNSTREAM: <carry>` / `UPSTREAM: <carry>` commit prefixes.
