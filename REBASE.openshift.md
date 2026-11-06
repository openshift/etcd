# Maintaining openshift/etcd

OpenShift is based on upstream etcd. With every release of etcd that is
intended to be shipped as OCP, it is necessary to incorporate the upstream changes
while ensuring that our downstream customizations are maintained.

## Maintaining this document

An openshift/etcd rebase is a complex process involving many manual and
potentially error-prone steps. If, while performing a rebase, you find areas where
the documented procedure is unclear or missing detail, please update this document
and include the change in the rebase PR. This will ensure that the instructions are
as comprehensive and accurate as possible for the person performing the next
rebase.

## Getting started

Before incorporating upstream changes you may want to:

- Read this document
- Find the best tool for resolving merge conflicts
- Use diff3 conflict resolution strategy
  (https://blog.nilbus.com/take-the-pain-out-of-git-conflict-resolution-use-diff3/)
- Teach Git to remember how you’ve resolved a conflict so that the next time it can
  resolve it automatically (https://git-scm.com/book/en/v2/Git-Tools-Rerere)

## Preparing the local repo clone

Clone from a personal fork of etcd via a pushable (ssh) url:

```
git clone git@github.com:<user id>/etcd
```

## Two kinds of rebase

openshift/etcd follows upstream etcd releases in two ways, depending on the kind
of release:

- **Patch release**, for example v3.7.3 to v3.7.4: merge the new tag with
  `openshift-hack/rebase.sh`. The new tag contains the previous one, so the
  merge only brings in the upstream commits since the last rebase. See
  [Patch releases](#patch-releases-updating-with-rebasesh).
- **Minor release**, for example 3.6 to 3.7: start from the new tag and
  cherry-pick our downstream changes (carries) onto it. The new tag does not contain the previous
  minor release, so merging would bring in hundreds of upstream commits that
  conflict with their own backports. See
  [Minor releases](#minor-releases-cherry-picking-the-carries).

Both keep the history connected, and neither needs a force push. The move from
etcd 3.5 to 3.6 ([#343](https://github.com/openshift/etcd/pull/343)) rewrote
the branch once, after about three years of drift. Do not do that again.

In both, the [commit message prefixes](#commit-message-prefixes) decide which
downstream commits are kept.

## Commit message prefixes

Every non-merge commit in openshift/etcd that is not in upstream etcd starts
with a prefix. The prefix tells the person doing the next rebase whether to
keep the commit. openshift/etcd uses the same prefixes as
[openshift/kubernetes](https://github.com/openshift/kubernetes/blob/master/REBASE.openshift.md),
plus two etcd rules for dependencies.

- `UPSTREAM: <carry>:`
  - A persistent change that must survive every rebase. This covers changes
    that are specific to OpenShift, and changes that are waiting for upstream
    etcd or one of its dependencies (such as bbolt) to include them.
  - Kept in a patch release merge. Cherry-picked again in a minor release
    rebase.
  - Examples: the non-blocking defrag, the `openshift/bbolt` replace directive
    (OCPBUGS-103516), and the `google.golang.org/protobuf` v1.36.12 bump
    (OCPBUGS-114428).
- `UPSTREAM: <PR#>:`, for example `UPSTREAM: 12345:`
  - A cherry-pick of the etcd-io/etcd pull request with that number, merged or
    still open.
  - Keep it, or cherry-pick it again, only while the new upstream tag does not
    include that pull request. To check, see whether the pull request, or the
    backport of it to the upstream release branch, is in the tag.
- `UPSTREAM: <drop>:`
  - A commit that only makes sense on the branch it was made for: merge
    conflict resolution, `go mod tidy`, generated files, builder and base image
    updates, and reverting a carry (for example, because upstream now includes
    it, or because it is being replaced).
  - Never cherry-picked again. Each rebase makes its own drop commits.
  - Automated commits that have no prefix, such as the image updates that the
    OpenShift build tooling creates, are treated as `UPSTREAM: <drop>:`.

When a Jira ticket exists, put its ID after the prefix, for example
`UPSTREAM: <carry>: OCPBUGS-12345: Short summary`.

### Dependency versions ahead of upstream

openshift/kubernetes labels dependency updates `<drop>`, because each rebase
updates them again. In openshift/etcd, a commit that moves a dependency to a
newer version than the upstream release uses, for example to pick up a fix
early, is an `UPSTREAM: <carry>:`. If it were a `<drop>`, the next rebase would
quietly lose it.

These carries conflict in `go.mod` and `go.sum` on almost every patch release
merge. Keep the higher of the two versions in every module that the carry
changed, then run `go mod tidy` in those modules. When the upstream tag uses
the same version or a newer one, take the upstream side and the carry is no
longer needed.

### Carries that wait on a dependency

`UPSTREAM: <PR#>:` only works for etcd-io/etcd pull requests. A carry that
waits on a dependency, like the `openshift/bbolt` fork, uses
`UPSTREAM: <carry>:`. In the commit message body, link the upstream issue or
pull request, and say when to drop the carry, for example:

```
UPSTREAM: <carry>: OCPBUGS-103516: Replace bbolt with patched fork to remove MADV_RANDOM

Point go.etcd.io/bbolt at openshift/bbolt release-X.Y, which is bbolt
vA.B.C with the MADV_RANDOM call removed.

Upstream issue: https://github.com/etcd-io/bbolt/issues/939

Drop this carry once a bbolt release includes the fix.
```

### Older commits

Commits made before these rules may use other prefixes. Do not rewrite them.
At the next minor release rebase, relabel each one as it is cherry-picked:

- `DOWNSTREAM: <carry>:` becomes `UPSTREAM: <carry>:`, or goes into the
  OpenShift files commit (see
  [Minor releases](#minor-releases-cherry-picking-the-carries)).
- `UPSTREAM: <carry>:` for a cherry-pick of an etcd-io/etcd pull request
  becomes `UPSTREAM: <PR#>:`.
- `DOWNSTREAM: <drop>:` and commits with no prefix, such as automated image
  updates, are not cherry-picked. Their end state is already in the files that
  are carried.
- A reverted carry is left out together with its revert, whatever prefix the
  revert has.

## Patch releases: updating with `rebase.sh`

`rebase.sh` merges the OpenShift branch with the new upstream tag and creates a
PR for you. It does not resolve conflicts automatically.

Here are the steps:
1. Create a new OCPBUGS JIRA ticket with the respective OpenShift version to rebase. Please include the change logs in the ticket description. You can clone a previous rebase we did in [OCPBUGS-947](https://issues.redhat.com/browse/OCPBUGS-947) and adjust.
2. It's best to start off with a fresh fork of [openshift/etcd](https://github.com/openshift/etcd/). Stay on the `main` branch.
3. This script requires `jq`, `git`, `podman` and `bash`. `gh` is optional.
4. In the root dir of that fork run:
```
openshift-hack/rebase.sh --etcd-tag=v3.5.4 --openshift-release=openshift-4.12 --jira-id=666
```

where `etcd-tag` is the [etcd-io/etcd](https://github.com/etcd-io/etcd/) release tag, the `openshift-release`
is the OpenShift release branch in [openshift/etcd](https://github.com/openshift/etcd/) and the `jira-id` is the
number of the OCPBUGS ticket created in step (1).

5. In case of conflicts, it will ask you to step into another shell to resolve those. The script will continue by committing the resolution with `UPSTREAM: <drop>:`. Before you continue, check the carries (see [Checking carries after a merge](#checking-carries-after-a-merge)).
6. At the end, there will be a "rebase-$VERSION" branch pushed to your fork.
7. If you have `gh` installed and are logged in, it will attempt to create a PR for you by opening a web browser.

### Checking carries after a merge

To list the carries in the merge result, run this on the rebase branch:

```
git log --no-merges --ancestry-path --format='%h %s' <base-tag>..HEAD \
  | grep -E '^[0-9a-f]+ UPSTREAM: (<carry>|[0-9]+):'
```

where `<base-tag>` is the upstream tag that the last minor release rebase
started from (for example, `v3.7.3` for the whole 3.7 line). `--ancestry-path`
leaves out the commits from before that rebase, which are still in the history.
On a branch that has not had a minor release rebase with this process yet,
drop `--ancestry-path`, use the upstream tag of the previous patch release as
`<base-tag>`, and also match `DOWNSTREAM: <carry>:`.

Go through every carry in the list, not only the ones that had conflicts. Make
sure that its change is still in the result. A carry can become unnecessary
even when the merge has no conflict. For example, if upstream etcd moves to a
bbolt release that includes the `MADV_RANDOM` fix, the `require` line in
`go.mod` changes but the `replace` line does not. The merge succeeds, and the
build keeps using the `openshift/bbolt` fork.

### Removing a carry in a patch release

When the new upstream tag includes a change that we carry, revert the carry in
the rebase PR. Commit the revert as `UPSTREAM: <drop>:` and name the upstream
release or commit that replaces it, for example:

```
UPSTREAM: <drop>: OCPBUGS-12345: Revert "Replace bbolt with patched fork to remove MADV_RANDOM"

bbolt vA.B.C includes the MADV_RANDOM fix (etcd-io/bbolt#939), so the
fork carried in b74556a0c is no longer needed.
```

If `git revert` does not apply cleanly, remove the change by hand in a commit
with the same kind of message. If you already took the upstream version while
resolving a merge conflict, list the removed carry in the rebase PR description
instead.

If the upstream version differs from ours, for example with a different flag
name or behavior, check whether
[cluster-etcd-operator](https://github.com/openshift/cluster-etcd-operator)
uses the downstream version before you remove it.

At the next minor release rebase, neither the carry nor its revert is
cherry-picked.

## Minor releases: cherry-picking the carries

A minor release rebase starts a new branch from the upstream tag, connects it to
the OpenShift branch's history, and cherry-picks the carries that are still
needed. This follows the openshift/kubernetes process.

1. Create a new OCPBUGS JIRA ticket, as for a patch release.
2. List the commits to consider:
   ```
   git log --no-merges --reverse --ancestry-path --format='%h %s' <base-tag>..openshift/main
   ```
   where `<base-tag>` is the upstream tag that the last minor release rebase
   started from. For the first minor release rebase with this process (3.6 to
   3.7), drop `--ancestry-path` and use the upstream tag that `main` is on,
   for example `v3.6.16`. Decide for each commit whether to cherry-pick it,
   squash it into another commit, or leave it out, using the
   [prefixes](#commit-message-prefixes). Record the decisions in the rebase PR
   description.
3. Create the rebase branch from the new upstream tag:
   ```
   git fetch upstream --tags
   git checkout -b rebase-v3.7.3 v3.7.3
   ```
4. Merge the OpenShift branch with the `ours` merge strategy:
   ```
   git merge -s ours openshift/main
   ```
   This records the OpenShift branch as merged without taking any of its
   content, so the branch still matches the upstream tag. It keeps the history
   connected and lets the rebase PR merge normally, without a force push. Do
   not confuse it with the `-X ours` option, which resolves conflicts.
5. Cherry-pick the commits:
   - Cherry-pick `UPSTREAM: <carry>:` commits.
   - Cherry-pick `UPSTREAM: <PR#>:` commits only if the new tag does not
     include that pull request.
   - Leave out `UPSTREAM: <drop>:` commits. Also leave out any carry that a
     later commit reverted, together with the revert, whatever prefix the
     revert has.
   - Squash everything that only touches OpenShift files into one commit named
     `UPSTREAM: <carry>: Add OpenShift specific files`. This includes
     `openshift-tools/`, `openshift-hack/`, the Dockerfiles,
     `.ci-operator.yaml`, this document, and the agent docs.
   - Squash carries that only make sense together, such as a change and the
     follow-up commits that fix it.
   - Fix each commit message as you go, so that every commit follows the
     prefixes in this document (see [Older commits](#older-commits)).
   - If the new etcd release uses a new bbolt version, check whether the
     `openshift/bbolt` fork needs a new branch (see
     [Downstream bbolt fork](#downstream-bbolt-fork-openshiftbbolt)).
6. Update the builder and base images, run `go mod tidy`, and regenerate
   generated files. Commit each as `UPSTREAM: <drop>:`.
7. Build and test, push the branch to your fork, and open a PR against `main`.

## Building and testing

- Build the code with `make`
- Test the code with `make test`

## Downstream bbolt fork (openshift/bbolt)

openshift/etcd currently carries a `go.mod` replace directive pointing
`go.etcd.io/bbolt` to a patched fork at `openshift/bbolt`. This fork
removes the `madvise(MADV_RANDOM)` call to fix a page cache regression
on RHEL 10 kernels (OCPBUGS-103516).

Each openshift/etcd release branch pins to a corresponding
openshift/bbolt release branch (e.g. `release-5.0` → `release-5.0`).
These branches are based on the specific bbolt tag that openshift/etcd
uses for that release, with the patch applied on top. Do not rebase an
existing openshift/bbolt release branch to a newer bbolt tag — that
would break the corresponding openshift/etcd release.

When rebasing openshift/etcd to a new upstream etcd that bumps the
bbolt version for a new release, first check whether the new bbolt tag
already includes the MADV_RANDOM fix
(https://github.com/etcd-io/bbolt/issues/939).

**If the new bbolt tag does NOT include the fix:**

1. Create a new `release-X.Y` branch on `openshift/bbolt` from the new
   bbolt tag (e.g. `release-5.1` from `v1.4.4`)
2. Cherry-pick the MADV_RANDOM removal patch onto it
3. Update the replace directive and run `go mod tidy` in each of the
   four module directories. Use `go mod edit` with the raw commit hash
   and `go mod tidy` will resolve the correct pseudo-version
   (timestamp + hash):
   ```bash
   for dir in . server etcdutl tests; do
     (cd $dir && go mod edit -replace go.etcd.io/bbolt=github.com/openshift/bbolt@<commit-hash> && go mod tidy)
   done
   ```

The replace directive silently overrides whatever bbolt version is in
`go.mod` — a version bump without updating the fork will appear to
succeed but won't actually pick up the new bbolt code.

**If the new bbolt tag DOES include the fix:**

The fork is no longer needed. Drop the replace directive from all four
`go.mod` files and remove this section from the rebase document.

## Payload testing

After all the above are green and your PR pre-submits are too, you can start with payload testing. This is to ensure the nightly jobs won't break on etcd after the merge, they also test all of OpenShift (including upgrades) well enough. 

You should run those two:

> /payload 4.x nightly informing
> 
> /payload 4.x nightly blocking

Replace 4.x with the respective OpenShift release you're merging against.

It pays off to inspect some Prometheus metrics (CPU, memory, disk usage) with an upgrade job through PromeCIus. This is to ensure we don't increase resource usage inadvertently. 

## Testing with ClusterBot

Sometimes it's easier to debug an issue using cluster bot. Here you can simply run the given OpenShift release using your rebase PR:

> launch openshift/etcd#155

This is particularly helpful when you want to test specific providers, for example Bare Metal or VSphere, or just other variants like SNO.

## Performance Testing

We currently do not do performance testing after an etcd rebase in OpenShift, however the upstream community and [SIG Scalability in k/k does](https://github.com/etcd-io/etcd/issues/14138#issuecomment-1247665949). 

The OpenShift scalability team also regularly runs performance tests with upcoming 4.y.0 releases.
