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
cd etcd
git remote add upstream https://github.com/etcd-io/etcd.git
git remote add openshift https://github.com/openshift/etcd.git
```

Both flows start by fetching both remotes, so that `openshift/<branch>` and the
upstream tags are current.

## Two kinds of rebase

openshift/etcd follows upstream etcd releases in two ways, depending on the kind
of release:

- **Patch release**, for example v3.7.3 to v3.7.4: merge the new tag into the
  branch. The new tag contains the previous one, so the merge only brings in
  the upstream commits since the last rebase. See
  [Patch releases](#patch-releases-merging-the-new-tag).
- **Minor release**, for example 3.6 to 3.7: start from the new tag and
  cherry-pick our downstream changes (carries) onto it. A minor release covers
  more than a year of upstream development, so merging the new tag would bring
  in thousands of commits, many of them already backported to the previous
  release as different commits, and they would conflict. See
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
plus notes on dependencies.

- `UPSTREAM: <carry>:`
  - A persistent change that must survive every rebase and has no
    etcd-io/etcd pull request to cherry-pick. This covers:
    - changes that only make sense in OpenShift, for example
      `discover-etcd-initial-cluster`;
    - dependencies we need ahead of the upstream release, either a newer
      version or a patched fork, for example the `google.golang.org/protobuf`
      bump (OCPBUGS-114428) or the `openshift/bbolt` fork. See
      [Dependency versions ahead of upstream](#dependency-versions-ahead-of-upstream)
      and [Carries that wait on a dependency](#carries-that-wait-on-a-dependency).
  - If the change is a cherry-pick of an etcd-io/etcd pull request, use
    `UPSTREAM: <PR#>:` instead. A related upstream change that is not the
    source of the commit goes in the body. When a carry could go upstream,
    open that pull request.
  - Kept in a patch release merge. Cherry-picked again in a minor release
    rebase while it is still needed.
- `UPSTREAM: <PR#>:`, for example `UPSTREAM: 12345:`
  - A cherry-pick of the etcd-io/etcd pull request with that number, merged or
    still open.
  - Keep it, or cherry-pick it again, only while the new upstream tag does not
    include that pull request. To check, run
    `git log --oneline -E --grep='#<PR#>([^0-9]|$)' <tag>`. A backport to the
    upstream release branch has its own pull request number; the original pull
    request's page links to it.
  - If the pull request is closed without merging, keep carrying the change
    with the same prefix until an equivalent change lands upstream. At the
    next minor release, note in the body that the pull request was closed
    and say when to drop the carry.
- `UPSTREAM: <drop>:`
  - A commit that only makes sense on the branch it was made for: merge
    conflict resolution, `go mod tidy`, generated files, and builder and base
    image updates.
  - Not cherry-picked as their own commits. Each rebase makes its own drop
    commits.
  - Commits that only touch OpenShift files, such as automated image updates
    and image-only drops, keep their changes: at a minor release, they are
    squashed into the `Add OpenShift specific files` commit (see
    [Minor releases](#minor-releases-cherry-picking-the-carries)).
- `UPSTREAM: revert: <original prefix>: <original subject>`
  - Reverts a carry, for example because upstream now includes it, an
    upstream change we want bundles it with other changes, or it causes a
    regression. Keep git's `This reverts commit <sha>.` line and add the
    reason.
  - Never cherry-picked again. At the next minor release, leave out both
    the revert and the carry it reverts. If the revert was itself reverted
    later, the carry is active again: keep it.

When a Jira ticket exists, put its ID after the prefix, for example
`UPSTREAM: <carry>: OCPBUGS-12345: Short summary`.

### Dependency versions ahead of upstream

openshift/kubernetes lists dependency updates under `<drop>`, but that covers
changes each rebase redoes anyway: vendoring and its own OpenShift libraries.
A dependency moved to a newer version than the upstream release uses, for
example for a CVE fix, is an `UPSTREAM: <carry>:` there too (for example
`UPSTREAM: <carry>: bump otel to v1.44.0 to fix CVE-2026-41178`), and in
openshift/etcd. If it were a `<drop>`, the next rebase would quietly lose it.

These carries conflict in `go.mod` and `go.sum` on almost every patch release
merge. Keep the higher of the two versions in every module that the carry
changed, then run `go mod tidy` in those modules. Taking the upstream side by
default undid the protobuf carry in a draft of the v3.6.16 rebase
([#427](https://github.com/openshift/etcd/pull/427)); review caught it.
When the upstream tag uses the same version or a newer one, take the upstream
side and the carry is no longer needed.

### Carries that wait on a dependency

`UPSTREAM: <PR#>:` refers to etcd-io/etcd pull requests. A carry that waits on
a dependency, like the `openshift/bbolt` fork, uses `UPSTREAM: <carry>:`.
openshift/kubernetes labels its dependency forks the same way, but does not
say when to drop them. In openshift/etcd, link the upstream issue or pull
request in the commit message body, and say when to drop the carry, for
example:

```
UPSTREAM: <carry>: OCPBUGS-103516: Replace bbolt with patched fork to remove MADV_RANDOM

Point go.etcd.io/bbolt at openshift/bbolt release-X.Y-bbolt-vA.B.C, which is
bbolt vA.B.C with the MADV_RANDOM call removed.

Upstream issue: https://github.com/etcd-io/bbolt/issues/939

Drop this carry once upstream etcd uses a bbolt release that includes the fix.
```

### Older commits

Commits made before these rules may use other prefixes, or none. Do not
rewrite them. At the next minor release rebase, relabel each one as it is
cherry-picked:

- Decide by each commit's diff (`git show --stat <sha>`), not by its subject
  or prefix. Older commits can be mislabeled: a commit named as a conflict
  resolution or a merge can hold a carry.
- `DOWNSTREAM: <carry>:` becomes `UPSTREAM: <carry>:`, or goes into the
  OpenShift files commit (see
  [Minor releases](#minor-releases-cherry-picking-the-carries)).
- `UPSTREAM: <carry>:` for a cherry-pick of an etcd-io/etcd pull request
  becomes `UPSTREAM: <PR#>:`.
- Commits that only touch OpenShift files, whatever their prefix (including
  `DOWNSTREAM: <drop>:` and automated image updates), are squashed into the
  `Add OpenShift specific files` commit (see step 5 of
  [Minor releases](#minor-releases-cherry-picking-the-carries)).
- Drops from merging upstream, such as conflict resolution and `go mod tidy`,
  are left out, whatever their prefix. They only fit the old base; the new
  rebase resolves its own conflicts and runs its own `go mod tidy`.
- A drop that also fixes a carry, for example by adding an import the carry
  needs, is split: add each fix to the carry it fixes when you cherry-pick
  that carry, and leave the rest of the drop out.
- A reverted carry is left out together with its revert, whatever prefix the
  revert has. New reverts use `UPSTREAM: revert:`; the revert list in
  [Checking carries after a merge](#checking-carries-after-a-merge) finds
  them.

## Patch releases: merging the new tag

`openshift-hack/rebase.sh` is outdated and will be removed: it pushes the
branch without the merge and commits conflict markers. Follow the steps below.

1. Create a new OCPBUGS JIRA ticket for the OpenShift version, with the
   upstream changelog in the description. You can clone a previous rebase
   ticket such as [OCPBUGS-947](https://issues.redhat.com/browse/OCPBUGS-947).
2. Fetch, and create the rebase branch from the branch you rebase (`main`, or
   a release branch such as `release-5.1`):
   ```
   git fetch openshift && git fetch upstream --tags
   git checkout -b rebase-v3.7.4 openshift/main
   ```
3. Merge the new tag:
   ```
   git merge v3.7.4
   ```
   Resolve the conflicts (for `go.mod` and `go.sum`, see
   [Dependency versions ahead of upstream](#dependency-versions-ahead-of-upstream)),
   then commit the merge with git's default message. Before you commit, both of
   these must print nothing:
   ```
   git diff --name-only --diff-filter=U
   git grep -n -E '^(<<<<<<<|>>>>>>>)( |$)'
   ```
4. Run `go mod tidy` in every module the merge changed (on 3.7 and later,
   `make fix-mod-tidy` does all of them). Use the Go version in `go.mod`; CI
   builds with the `build_root_image` in `.ci-operator.yaml`. Commit the
   result as `UPSTREAM: <drop>: go mod tidy`.
5. Whether or not there were conflicts, [check the carries](#checking-carries-after-a-merge)
   and [the bbolt version](#when-a-rebase-changes-the-bbolt-version).
6. Build and test (see [Building and testing](#building-and-testing)), push
   the branch to your fork, and open a PR against the branch you rebased, for
   example:
   ```
   git push origin rebase-v3.7.4
   gh pr create -R openshift/etcd --base main --head <user id>:rebase-v3.7.4 \
     --title "OCPBUGS-<number>: Rebase v3.7.4"
   ```
   Link the upstream release notes in the PR description.

### Checking carries after a merge

To list the carries in the merge result, run this on the rebase branch:

```
git log --no-merges --ancestry-path --format='%h %s' <base-tag>..HEAD ^<new-tag> \
  | grep -E '^[0-9a-f]+ UPSTREAM: (<carry>|[0-9]+):'
```

where `<base-tag>` is the upstream tag that the last minor release rebase
started from (for example, `v3.7.3` for the whole 3.7 line), and `<new-tag>` is
the tag you just merged. `--ancestry-path` leaves out the commits from before
that rebase, which are still in the history, and `^<new-tag>` leaves out the
upstream commits.

To list the reverts, so that you can strike the carries they revert:

```
git log --no-merges --ancestry-path --format='%h %s' <base-tag>..HEAD ^<new-tag> \
  | grep -E '^[0-9a-f]+ (UPSTREAM: revert:|.*Revert ")'
```

The second pattern also finds older reverts that kept git's default
`Revert "..."` subject under another prefix.

On a branch that has not had a minor release rebase with this process yet (the
3.6 branches), change both commands: drop `--ancestry-path` and `^<new-tag>`,
use `<new-tag>` as `<base-tag>`, and use this pattern for the carries, which
also matches the older prefixes:

```
grep -E '^[0-9a-f]+ (UPSTREAM: (<carry>|[0-9]+)|DOWNSTREAM:? <carry>):'
```

Also read the commits that have no prefix: some older carries have none.

Go through every carry in the list, not only the ones that had conflicts. Make
sure that its change is still in the result. A carry can become unnecessary
even when the merge has no conflict. For example, if upstream etcd moves to a
bbolt release that includes the `MADV_RANDOM` fix, the `require` line in
`go.mod` changes but the `replace` line does not. The merge succeeds, and the
build keeps using the `openshift/bbolt` fork.

The opposite also happens: if upstream etcd moves to a newer bbolt that still
lacks the fix, the merge succeeds and the build stays on the old fork. See
[When a rebase changes the bbolt version](#when-a-rebase-changes-the-bbolt-version).

### Removing a carry in a patch release

When the new upstream tag includes a change that we carry, first check whether
the downstream version is still in the merged tree:

```
git diff <new-tag> HEAD -- <files the carry changed>
```

- If it prints nothing, upstream's version is already in place. Do not revert
  the carry: a revert would remove upstream's code. For an `UPSTREAM: <PR#>:`
  carry, nothing else is needed. For an `UPSTREAM: <carry>:`, record the
  removal with an empty revert, so that the revert list finds it:
  ```
  git commit --allow-empty -m "UPSTREAM: revert: <carry>: <subject>" \
    -m "This reverts commit <sha>." -m "<new-tag> includes this change."
  ```
- If it prints the downstream difference, for example the bbolt `replace`,
  revert the carry in the rebase PR. Prefix the revert with
  `UPSTREAM: revert:` followed by the carry's subject (see
  [Commit message prefixes](#commit-message-prefixes)), and name the upstream
  release or commit that replaces it, for example:
  ```
  UPSTREAM: revert: <carry>: OCPBUGS-103516: Replace bbolt with patched fork to remove MADV_RANDOM

  This reverts commit <sha>.

  bbolt vA.B.C includes the MADV_RANDOM fix (etcd-io/bbolt#939).
  ```

If `git revert` does not apply cleanly, remove the change by hand in a commit
with the same kind of message. If you already took the upstream version while
resolving a merge conflict, record it with an empty revert, as above.

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

The steps below rebase `main`. To rebase a release branch, use the branch that
CI builds (`release-<X.Y>`, for example `release-5.1`) instead of `main` in
steps 2, 4, 5, 7 and 8. Step 2's list for that branch decides what is kept. If
`main` was already rebased onto the same tag, use the adapted version of each
kept commit from that rebase instead of the original, listed by:

```
git log --no-merges --reverse --ancestry-path --format='%h %s' <tag>..<main rebase PR head>
```

Never cherry-pick that PR's merge commit or its `<drop>` commits.

1. Create a new OCPBUGS JIRA ticket, as for a patch release.
2. Fetch, and list the commits to consider:
   ```
   git fetch openshift && git fetch upstream --tags
   git log --no-merges --reverse --ancestry-path --format='%h %s' \
     <base-tag>..openshift/main ^<latest-tag>
   ```
   where `<base-tag>` is the upstream tag that the last minor release rebase
   started from, and `<latest-tag>` is the latest upstream tag merged into the
   branch (`git describe --tags --abbrev=0 openshift/main`). `^<latest-tag>`
   leaves out the upstream commits that patch release merges brought in. For
   the first minor release rebase with this process (3.6 to 3.7), drop
   `--ancestry-path` and `^<latest-tag>`, and use the upstream tag that `main`
   is on, for example `v3.6.16`. Decide for each commit whether to cherry-pick
   it, squash it into another commit, or leave it out, using the
   [prefixes](#commit-message-prefixes) and [Older commits](#older-commits).
   Record the decisions in the rebase PR description.
3. Create the rebase branch from the new upstream tag:
   ```
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
   - Cherry-pick `UPSTREAM: <carry>:` commits that are still needed. Leave
     out a carry that waited on upstream or a dependency once the new tag
     includes the change.
   - Cherry-pick `UPSTREAM: <PR#>:` commits only if the new tag does not
     include that pull request. If the pull request was closed without
     merging, keep it and add the note described in
     [Commit message prefixes](#commit-message-prefixes).
   - Leave out `UPSTREAM: <drop>:` commits. Also leave out any carry that a
     later commit reverted, together with the revert, whatever prefix the
     revert has. New reverts use `UPSTREAM: revert:`; the revert list in
     [Checking carries after a merge](#checking-carries-after-a-merge) finds
     them.
   - Squash the commits that only touch OpenShift files into one commit named
     `UPSTREAM: <carry>: Add OpenShift specific files`, as openshift/kubernetes
     does. The OpenShift files are the paths in the check below.
     - Split any commit that changes both OpenShift files and etcd files
       first: its OpenShift files go into this commit, and each etcd change
       goes into the carry it belongs to (see [Older commits](#older-commits)).
     - Cherry-pick the previous rebase's `Add OpenShift specific files`
       commit, if there is one, then every later commit that only touches
       OpenShift files, whatever its prefix. Squash them into one commit, and
       keep the squashed commits' subjects in its body.
     - If a file that upstream also has, such as `OWNERS`, conflicts, keep the
       version from the commit you are picking:
       `git checkout <sha> -- OWNERS`.
     - You can keep the picks as separate commits while the PR is in review,
       and squash them before it merges. Squash only the picks, never the
       `-s ours` merge. Force-pushing your own PR branch for this is fine.
     - Then check that the OpenShift files match `openshift/main`. This
       prints nothing:
       ```
       git diff openshift/main HEAD -- openshift-tools openshift-hack build.sh \
         OWNERS .ci-operator.yaml 'Dockerfile.*' REBASE.openshift.md AGENTS.md \
         CLAUDE.md ARCHITECTURE.md docs/openshift-downstream.md
       ```
       Make any change the new release needs to these files in a separate
       commit after this check.
   - Squash carries that only make sense together, such as a change and the
     follow-up commits that fix it.
   - Fix each commit message as you go, so that every commit follows the
     prefixes in this document (see [Older commits](#older-commits)).
   - Cherry-pick the bbolt carry, then always repoint its `replace` at the
     fork branch for the bbolt version that the new tag requires (for v3.7.3,
     `release-5.1-bbolt-v1.5.0`), using step 2 of
     [When a rebase changes the bbolt version](#when-a-rebase-changes-the-bbolt-version).
     The cherry-pick keeps the old fork's `replace` line. Fold the repoint
     into the carry, and update the branch name in its message.
   - Check that no carried change was lost. Every file this prints must be
     explained in the PR description (its carry was left out on purpose, or
     upstream removed or replaced the file):
     ```
     comm -23 <(git diff --name-only <old-base-tag> openshift/main | sort) \
              <(git diff --name-only <new-tag> HEAD | sort)
     ```
     For 3.6 to 3.7, `<old-base-tag>` is `v3.6.16` and `<new-tag>` is
     `v3.7.3`.
6. Update the builder and base images (in `.ci-operator.yaml` and the
   `Dockerfile.*` files), run `go mod tidy` in every module (on 3.7 and later,
   `make fix-mod-tidy`), and regenerate generated files. Commit each as
   `UPSTREAM: <drop>:`.
7. Build and test (see [Building and testing](#building-and-testing)), push the
   branch to your fork, and open a PR against `main`.
8. Before the PR merges, fetch `openshift/main` again. If it has new commits,
   handle each as in step 5, then record the new tip with another
   `git merge -s ours openshift/main` and rerun the OpenShift files check. To
   keep this rare, ask the team for a short merge freeze on `main` in the last
   days before the rebase PR merges.
9. After the rebase PR merges, every open PR against `main` must be rebased
   onto the new branch before it merges. A PR still based on the old branch
   brings in commits that the `--ancestry-path` lists never show. To find
   them:
   ```
   for pr in $(gh pr list -R openshift/etcd --base main --state open --json number --jq '.[].number'); do
     git fetch -q openshift "pull/$pr/head" &&
       git tag --merged FETCH_HEAD '<new minor>.*' | grep -q . || echo "#$pr needs a rebase"
   done
   ```
   where `<new minor>` is, for example, `v3.7`. Tell the team in the channel
   when the rebase PR merges.

## Building and testing

- Build with `./build.sh`, which the image build uses. Check that `bin/` has
  `etcd`, `etcdctl`, `etcdutl` and `discover-etcd-initial-cluster`; `make`
  builds only the first three.
- Test with `make test` and `go test ./openshift-tools/...`.

## Downstream bbolt fork (openshift/bbolt)

openshift/etcd carries a `go.mod` replace directive that points
`go.etcd.io/bbolt` at a patched fork, `openshift/bbolt`. The fork removes the
`madvise(MADV_RANDOM)` call to fix a page cache regression on RHEL 10 kernels
(OCPBUGS-103516, https://github.com/etcd-io/bbolt/issues/939).

### Fork branches

Each fork branch is one upstream bbolt tag with the patch on top. Branches are
named `release-<OpenShift release>-bbolt-<bbolt tag>`, for example
`release-5.1-bbolt-v1.5.0`. The older `release-5.0` and `release-5.1` branches
predate this naming; `release-5.0-bbolt-v1.4.3` and `release-5.1-bbolt-v1.5.0`
point at the same commits. Keep both names.

- The replace pins a fork commit, not a branch, so every released etcd build
  depends on that exact commit. Never rebase, force-push or delete a fork
  branch.
- When etcd needs a bbolt tag that has no fork branch yet, cut a new branch
  from the tag and cherry-pick the patch onto it.
- At each new OpenShift release, create that release's branch name even if
  bbolt has not changed. Point it at the same commit as the previous release's
  branch, for example, in a clone of openshift/bbolt:
  ```
  git fetch origin
  git push origin origin/release-5.1-bbolt-v1.5.0:refs/heads/release-5.2-bbolt-v1.5.0
  ```
  Do not cherry-pick the patch again: a new commit would change etcd's pin for
  no reason.

### When a rebase changes the bbolt version

After any rebase, compare the bbolt version in `require` with the bbolt tag of
the fork branch that `replace` points at. The `replace` pseudo-version ends in
the fork commit's hash; `git ls-remote https://github.com/openshift/bbolt | grep <hash>`
names its branch. The replace silently overrides `require`: if upstream etcd
moved to a newer bbolt, the merge succeeds but the build stays on the old fork.
This can happen in a patch release too, for example etcd 3.7.5 moving to bbolt
v1.5.1.

If the versions differ, check whether the new bbolt tag includes the
`MADV_RANDOM` fix.

**If the new tag does not include the fix:**

1. Use the fork branch for the new tag. If there is none, cut one (see
   [Fork branches](#fork-branches)), for example `release-5.1-bbolt-v1.5.1`
   from `v1.5.1`.
2. Point the replace at its commit and run `go mod tidy` in each of the four
   module directories. Use `go mod edit` with the raw commit hash, and
   `go mod tidy` resolves the pseudo-version:
   ```bash
   for dir in . server etcdutl tests; do
     (cd $dir && go mod edit -replace go.etcd.io/bbolt=github.com/openshift/bbolt@<commit-hash> && go mod tidy)
   done
   ```
3. Commit it:
   - In a minor release rebase, this is part of cherry-picking the bbolt carry:
     one `UPSTREAM: <carry>:` commit with the message from
     [Carries that wait on a dependency](#carries-that-wait-on-a-dependency).
   - In a patch release rebase, add a new commit, for example
     `UPSTREAM: <carry>: OCPBUGS-103516: Point bbolt fork at release-5.1-bbolt-v1.5.1`.
     At the next minor release rebase, do not cherry-pick it separately: it
     folds into the bbolt carry.

**If the new tag includes the fix:**

The fork is no longer needed. In a patch release rebase, revert the bbolt carry
and any later repoint commits (see
[Removing a carry in a patch release](#removing-a-carry-in-a-patch-release)).
In a minor release rebase, leave them out. Then remove this whole "Downstream
bbolt fork" section, and the bbolt items in `AGENTS.md` and
`docs/openshift-downstream.md`.

## Payload testing

After all the above are green and your PR pre-submits are too, you can start with payload testing. This is to ensure the nightly jobs won't break on etcd after the merge, they also test all of OpenShift (including upgrades) well enough. 

You should run those two:

> /payload 5.x nightly informing
> 
> /payload 5.x nightly blocking

Replace 5.x with the respective OpenShift release you're merging against.

It pays off to inspect some Prometheus metrics (CPU, memory, disk usage) with an upgrade job through PromeCIus. This is to ensure we don't increase resource usage inadvertently. 

## Testing with ClusterBot

Sometimes it's easier to debug an issue using cluster bot. Here you can simply run the given OpenShift release using your rebase PR:

> launch openshift/etcd#155

This is particularly helpful when you want to test specific providers, for example Bare Metal or VSphere, or just other variants like SNO.

## Performance Testing

We currently do not do performance testing after an etcd rebase in OpenShift, however the upstream community and [SIG Scalability in k/k does](https://github.com/etcd-io/etcd/issues/14138#issuecomment-1247665949). 

The OpenShift scalability team also regularly runs performance tests with upcoming 4.y.0 releases.
