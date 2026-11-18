# Maintaining openshift/etcd

openshift/etcd is upstream etcd plus downstream commits (carries). This
document says how to move it to a new upstream release. If a step is unclear or
wrong while you rebase, fix this document in the rebase PR.

**Minor release base tag:** none yet. Each branch records its own in its copy
of this document; read it from the branch you rebase
(`git show openshift/<branch>:REBASE.openshift.md`). A branch whose copy says
"none yet", or has no such line (such as `release-5.0`), is still on the 3.6
line, where the commands below use their first-switch rules. Each minor
release rebase PR sets this line on its branch (for example to `v3.7.3`); the
commands below call it `<base-tag>`. Patch release rebases do not change it.

## Preparing the local clone

```
git clone git@github.com:<user id>/etcd
cd etcd
git remote add upstream https://github.com/etcd-io/etcd.git
git remote add openshift https://github.com/openshift/etcd.git
git config merge.conflictStyle diff3   # show the base version in conflicts
```

Both flows start by fetching both remotes, so that `openshift/<branch>` and the
upstream tags are current. Start from a clean tree:
`git status --short --untracked-files=no` prints nothing. Changes to tracked
files would go into the rebase commits.

## Two kinds of rebase

- **Patch release**, for example v3.7.3 to v3.7.4: merge the new tag into the
  branch. The new tag contains the previous one, so the merge only brings in
  the upstream commits since the last rebase. See
  [Patch releases](#patch-releases-merging-the-new-tag).
- **Minor release**, for example 3.6 to 3.7: start from the new tag and
  cherry-pick our carries onto it. A minor release covers more than a year of
  upstream development, so merging the new tag would bring in thousands of
  commits, many of them already backported to the previous release as
  different commits, and they would conflict. See
  [Minor releases](#minor-releases-cherry-picking-the-carries).

Neither needs a force push. Do not rewrite the branch again, as the 3.5 to 3.6
move ([#343](https://github.com/openshift/etcd/pull/343)) did. In both, the
[commit message prefixes](#commit-message-prefixes) decide which downstream
commits are kept.

## Commit message prefixes

Every downstream non-merge commit starts with one of these prefixes, the same
as in
[openshift/kubernetes](https://github.com/openshift/kubernetes/blob/master/REBASE.openshift.md).
The prefix tells the next rebase whether to keep the commit. Automated ART
image updates ("Updating ose-etcd-container image ...") have no prefix. They
only touch OpenShift files and go into the squash in minor release step 5.

- `UPSTREAM: <carry>:`
  - A persistent change that has no etcd-io/etcd pull request to cherry-pick:
    - changes that only make sense in OpenShift, for example
      `discover-etcd-initial-cluster`;
    - dependencies we need ahead of the upstream release, either a newer
      version or a patched fork, for example the `google.golang.org/protobuf`
      bump (OCPBUGS-114428) or the `openshift/bbolt` fork (see the two
      sections below).
  - If the change is a cherry-pick of an etcd-io/etcd pull request, use
    `UPSTREAM: <PR#>:` instead. A related upstream change that is not the
    source of the commit goes in the body. When a carry could go upstream,
    open that pull request.
  - Kept in a patch release merge. Cherry-picked again in a minor release
    rebase while it is still needed.
- `UPSTREAM: <PR#>:`, for example `UPSTREAM: 12345:`
  - A cherry-pick of the etcd-io/etcd pull request with that number, merged or
    still open.
  - Keep it, or cherry-pick it again, only while the new tag does not include
    the pull request: `git log --oneline -E --grep='#<PR#>([^0-9]|$)' <tag>`.
    Check that the match is the merge or backport, not a revert of it, and
    that the tag has no later revert of it. A backport to the upstream release
    branch has its own pull request number; the original pull request's page
    links to it.
  - If the pull request is closed without merging, keep carrying the change
    with the same prefix until an equivalent change lands upstream. At the
    next minor release, note in the body that the pull request was closed and
    say when to drop the carry.
- `UPSTREAM: <drop>:`
  - A commit that only makes sense on the branch it was made for: merge
    conflict resolution, `go mod tidy`, generated files, and builder and base
    image updates. Not cherry-picked as its own commit; each rebase makes its
    own drops.
  - Drops that only touch OpenShift files, such as automated image updates,
    keep their changes through the `Add OpenShift specific files` squash
    (minor release step 5).
- `UPSTREAM: revert: <original prefix>: <original subject>`
  - Reverts a carry, for example because upstream now includes it, an
    upstream change we want bundles it with other changes, or it causes a
    regression. Keep git's `This reverts commit <sha>.` line and add the
    reason.
  - At the next minor release, leave out both the revert and the carry it
    reverts. If the revert was itself reverted later, the carry is active
    again: keep it.

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
([#427](https://github.com/openshift/etcd/pull/427)); review caught it. When
the upstream tag uses the same version or a newer one, take the upstream side;
the carry is no longer needed.

### Carries that wait on a dependency

A carry that waits on a dependency, like the `openshift/bbolt` fork, uses
`UPSTREAM: <carry>:`. openshift/kubernetes labels its dependency forks the
same way, but does not say when to drop them. In openshift/etcd, link the
upstream issue or pull request in the commit message body, and say when to
drop the carry, for example:

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
- `DOWNSTREAM: <carry>:` (also written `DOWNSTREAM <carry>:`) becomes
  `UPSTREAM: <carry>:`, or `UPSTREAM: <PR#>:` if it is a cherry-pick of an
  etcd-io/etcd pull request. `UPSTREAM: <carry>:` for such a cherry-pick also
  becomes `UPSTREAM: <PR#>:`. To check whether a carry was also proposed
  upstream, search for it:
  `gh pr list -R etcd-io/etcd --state all --search '<words from the subject> in:title'`.
- Commits that only touch OpenShift files, whatever their prefix, go into the
  `Add OpenShift specific files` squash (minor release step 5).
- Drops from merging upstream, such as conflict resolution and `go mod tidy`,
  are left out, whatever their prefix. They only fit the old base; the new
  rebase makes its own.
- A drop that also fixes a carry, for example by adding an import the carry
  needs, is split: add each fix to the carry it fixes when you cherry-pick
  that carry, and leave the rest of the drop out.
- A reverted carry is left out together with its revert, whatever prefix the
  revert has (see the `UPSTREAM: revert:` rule above).

## Patch releases: merging the new tag

These steps follow openshift/kubernetes's "Updating with `git merge`".
`openshift-hack/rebase.sh` automates them but has known bugs (it pushes the
branch without the merge, and builds the wrong builder image name) and will be
fixed; until then, follow the steps below.

1. Create a new OCPBUGS JIRA ticket for the OpenShift version, with the
   upstream changelog in the description. You can clone a previous rebase
   ticket such as [OCPBUGS-947](https://issues.redhat.com/browse/OCPBUGS-947).
2. Fetch, and create the rebase branch from the branch you rebase (`main`, or
   a release branch such as `release-5.1`):
   ```
   git fetch openshift && git fetch upstream --tags
   git checkout -b rebase-main-v3.7.4 openshift/main
   ```
3. Merge the new tag. If there are conflicts, commit the merge with the
   conflict markers still in the files, then resolve them in a separate
   commit, as openshift/kubernetes does, so that reviewers can see each
   conflict and its resolution:
   ```
   git -c rerere.enabled=false merge --no-edit v3.7.4
   # only if there were conflicts:
   git diff --name-only --diff-filter=U > ../conflicts.txt   # every conflicted path
   git add -u && git commit --no-edit
   # resolve every path in ../conflicts.txt, then:
   git add -u && git commit -m "UPSTREAM: <drop>: manually resolve conflicts"
   ```
   `-c rerere.enabled=false` keeps a resolution that git remembers from an
   earlier merge out of the marker commit. Use `git add -u`, not
   `git add --all`: the merge must not pick up local files. Resolve every
   listed path, including modify/delete conflicts, which have no markers. For `go.mod` and `go.sum`, see
   [Dependency versions ahead of upstream](#dependency-versions-ahead-of-upstream).
   When you are done, this must succeed (it fails if any conflict marker line
   is left, including diff3's `|||||||` line and a lone `=======`):
   ```
   ! git grep -n -E '^(={7}$|(<{7}|[|]{7}|>{7})( |$))'
   ```
4. Run `go mod tidy` in every module the merge changed (on 3.7 and later,
   `make fix-mod-tidy` does all of them). Use the Go version in `go.mod`; CI
   builds with the `build_root_image` in `.ci-operator.yaml`. Commit the
   result, if any, as `UPSTREAM: <drop>: go mod tidy`.
5. Whether or not there were conflicts, [check the carries](#checking-carries-after-a-merge)
   and [the bbolt version](#when-a-rebase-changes-the-bbolt-version).
6. Build and test (see [Building and testing](#building-and-testing)), push
   the branch to your fork, and open a PR against the branch you rebased, for
   example:
   ```
   git push origin rebase-main-v3.7.4
   gh pr create -R openshift/etcd --base main --head <user id>:rebase-main-v3.7.4 \
     --title "OCPBUGS-<number>: Rebase v3.7.4"
   ```
   Link the upstream release notes in the PR description.

### Checking carries after a merge

On the rebase branch, list the carries and the reverts:

```
git log --no-merges --ancestry-path --format='%h %s' <base-tag>..HEAD ^<new-tag> \
  | grep -E '^[0-9a-f]+ UPSTREAM: (<carry>|[0-9]+):'
git log --no-merges --ancestry-path --format='%h %s' <base-tag>..HEAD ^<new-tag> \
  | grep -E '^[0-9a-f]+ (UPSTREAM: revert:|.*Revert ")'
```

- `<base-tag>` is the [minor release base tag](#maintaining-openshiftetcd)
  in this document on the branch you rebase (for example, `v3.7.3` for the
  whole 3.7 line), and `<new-tag>` is the tag you just merged.
  `--ancestry-path` leaves out the commits from before that rebase, which are
  still in the history, and `^<new-tag>` leaves out the upstream commits.
- The revert pattern also finds older reverts that kept git's default
  `Revert "..."` subject under another prefix. Strike each reverted carry from
  the carry list, unless its revert was itself reverted.
- On a branch that has not had a minor release rebase with this process yet
  (the 3.6 branches), drop `--ancestry-path` and `^<new-tag>` from both
  commands, use `<new-tag>` as `<base-tag>`, and use this carry pattern, which
  also matches the older prefixes:
  ```
  grep -E '^[0-9a-f]+ (UPSTREAM: (<carry>|[0-9]+)|DOWNSTREAM:? <carry>):'
  ```
  Also read the commits that have no prefix: some older carries have none.

Go through every carry in the list, not only the ones that had conflicts, and
make sure that its change is still in the result. A carry can become
unnecessary, or stale, with no conflict at all. For example, upstream etcd can
move to a new bbolt release, with or without the `MADV_RANDOM` fix, while our
`replace` line stays on the old fork. See
[When a rebase changes the bbolt version](#when-a-rebase-changes-the-bbolt-version).

### Removing a carry in a patch release

When the new upstream tag includes a change that we carry, compare the carry's
own hunks (`git show <sha>`) with what is left of it in the merged tree:

```
git diff <new-tag> HEAD -- <files the carry changed>
```

Other carries can change the same files, so look for this carry's lines, not
for an empty diff.

- If none of its lines remain, upstream's version is already in place. Do not
  revert the carry: a revert would remove upstream's code. For an `UPSTREAM: <PR#>:`
  carry, nothing else is needed. For an `UPSTREAM: <carry>:`, record the
  removal with an empty revert, so that the revert list finds it:
  ```
  git commit --allow-empty -m "UPSTREAM: revert: <carry>: <subject>" \
    -m "This reverts commit <sha>." -m "<new-tag> includes this change."
  ```
- If some of its lines remain, for example the bbolt `replace`, remove only
  those. A full revert also removes any part of the carry that upstream now
  has, so use `git revert` only after checking each hunk it removes. Commit
  the removal with the `UPSTREAM: revert:` prefix, and name the upstream
  release or commit that replaces it, for example:
  ```
  UPSTREAM: revert: <carry>: OCPBUGS-103516: Replace bbolt with patched fork to remove MADV_RANDOM

  This reverts commit <sha>.

  bbolt vA.B.C includes the MADV_RANDOM fix (etcd-io/bbolt#939).
  ```
  If `git revert` does not apply cleanly, remove the change by hand in a
  commit with the same kind of message.
- If you already took the upstream version while resolving a merge conflict,
  record it as in the first bullet: nothing for an `UPSTREAM: <PR#>:` carry,
  an empty revert for an `UPSTREAM: <carry>:`.

If the upstream version differs from ours, for example with a different flag
name or behavior, check that
[cluster-etcd-operator](https://github.com/openshift/cluster-etcd-operator)
does not use the downstream version before you remove it.

## Minor releases: cherry-picking the carries

This follows the openshift/kubernetes process. Follow the copy of this
document on `$target` (step 2): the OpenShift files check makes the rebase
branch carry that copy, and step 7 sets the base tag line in it. The steps
below rebase `main`.
To rebase a release branch, use the branch that CI builds (`release-<X.Y>`, for
example `release-5.1`) instead of `main` in steps 2 to 9, and put the branch
in the rebase branch name (`rebase-release-5.1-v3.7.3`). A release branch
that is still synced from `main` gets the rebase from `main`; rebase it
separately only after it branches. Step 2's list for that branch decides what
is kept. If `main` was already rebased onto the same tag, use the adapted
version of each kept commit from that rebase instead of the original, listed
by:

```
git log --no-merges --reverse --ancestry-path --format='%h %s' <tag>..<main rebase PR head>
```

Never cherry-pick that PR's merge commit or its `<drop>` commits. Rebuild a
grouped commit, such as the OpenShift files squash, from the release branch's
own commits, because `main`'s version can hold changes the release branch
does not have.

1. Create a new OCPBUGS JIRA ticket, as for a patch release.
2. Fetch, record the tip you are rebasing, and list the commits to consider:
   ```
   git fetch openshift && git fetch upstream --tags
   target=$(git rev-parse openshift/main)
   git log --no-merges --reverse --ancestry-path --format='%h %s' \
     <base-tag>..$target ^<latest-tag>
   ```
   Write the `$target` SHA in the PR description; steps 4, 5 and 8 use it.
   `<base-tag>` is the [minor release base tag](#maintaining-openshiftetcd)
   in `$target`'s copy of this document (`git show $target:REBASE.openshift.md`),
   and `<latest-tag>` is the latest upstream tag merged into the branch
   (`git describe --tags --abbrev=0 --match 'v[0-9]*' $target`). `^<latest-tag>`
   leaves out the upstream commits that patch release merges brought in. For
   the first minor release rebase with this process (3.6 to 3.7), the list is
   `git log --no-merges --reverse --format='%h %s' <latest-tag>..$target`
   (for example `v3.6.16..$target`). Decide for each commit whether to cherry-pick
   it, squash it into another commit, or leave it out, using the
   [prefixes](#commit-message-prefixes) and [Older commits](#older-commits).
   Record the decisions in the rebase PR description.
3. Create the rebase branch from the new upstream tag:
   ```
   git checkout -b rebase-main-v3.7.3 v3.7.3
   ```
4. Merge the OpenShift branch with the `ours` merge strategy:
   ```
   git merge -s ours --no-edit $target
   ```
   This records the OpenShift branch as merged without taking any of its
   content, so the branch still matches the upstream tag. It keeps the history
   connected and lets the rebase PR merge normally, without a force push. Do
   not confuse it with the `-X ours` option, which resolves conflicts.
5. Cherry-pick the commits, applying the [prefixes](#commit-message-prefixes)
   and [Older commits](#older-commits) to each one:
   - Cherry-pick `UPSTREAM: <carry>:` commits that are still needed. Leave
     out a carry that waited on upstream or a dependency once the new tag
     includes the change.
   - Cherry-pick `UPSTREAM: <PR#>:` commits only if the new tag does not
     include that pull request. If it was closed without merging, keep the
     commit and add the note described in
     [Commit message prefixes](#commit-message-prefixes).
   - Leave out `UPSTREAM: <drop>:` commits, and each reverted carry together
     with its revert, whatever prefix the revert has. If a revert was itself
     reverted, keep the carry. To find the reverts, add
     `| grep -E '^[0-9a-f]+ (UPSTREAM: revert:|.*Revert ")'` to step 2's
     command.
   - Squash the commits that only touch OpenShift files into one commit named
     `UPSTREAM: <carry>: Add OpenShift specific files`, as openshift/kubernetes
     does. The OpenShift files are the paths in the check below.
     - Split any commit that changes both OpenShift files and etcd files
       first: its OpenShift files go into this commit, and each etcd change
       goes into the carry it belongs to. An etcd change that fits no carry
       becomes its own `UPSTREAM: <carry>:` commit. Leave out a change that a
       later commit undoes; `git diff <latest-tag> $target -- <file>` shows
       what is still live. If you cannot tell a carry from a leftover of a
       conflict resolution, find where the line came from with
       `git log -S'<line>' $target -- <file>`, and list it in the PR
       description for review.
     - Replay the previous rebase's `Add OpenShift specific files` commit, if
       there is one, then every later commit that touches OpenShift files,
       whatever its prefix, in order. For each, apply only its OpenShift
       files (this also covers the OpenShift part of a split commit):
       ```
       git diff --binary <sha>^ <sha> -- <the paths in the check below> | git apply --index -3
       ```
       Quote `'Dockerfile.*'`. Commit them as one commit, and keep the
       replayed commits' subjects in its body.
     - If a file that upstream also has, such as `OWNERS`, conflicts, keep the
       version from the commit you are replaying:
       `git checkout <sha> -- OWNERS`.
     - You can keep the picks as separate commits while the PR is in review,
       and squash them before it merges. Squash only the picks, never the
       `-s ours` merge. Force-pushing your own PR branch for this is fine.
     - Then check that the OpenShift files match `$target`. This must
       succeed:
       ```
       git diff --exit-code $target HEAD -- openshift-tools openshift-hack build.sh \
         OWNERS .ci-operator.yaml 'Dockerfile.*' REBASE.openshift.md AGENTS.md \
         CLAUDE.md ARCHITECTURE.md docs/openshift-downstream.md
       ```
       Make any change the new release needs to these files in a separate
       commit after this check.
   - Squash a carry with the later commits that fix it. Keep other carries
     separate, even when they belong to the same feature.
   - Fix each commit message as you go, so that every commit follows the
     prefixes in this document. Keep the original subject after the new
     prefix, so that the commit can be matched across rebases.
   - A dependency carry, such as bbolt or protobuf, usually conflicts in
     `go.mod` and `go.sum`. Do not merge them by hand: take the new tag's
     files, redo the carry's change with `go mod edit` in each module it
     changed, and run `go mod tidy` there. Leave any unrelated change that
     tidy makes out of the carry; step 6 commits it.
   - If the bbolt version that the new tag requires still lacks the
     `MADV_RANDOM` fix, cherry-pick the bbolt carry, then always repoint its
     `replace` at the fork branch for that bbolt version (for v3.7.3,
     `release-5.1-bbolt-v1.5.0`), using step 2 of
     [When a rebase changes the bbolt version](#when-a-rebase-changes-the-bbolt-version).
     Redo the `replace` as above for the new fork branch; the old line points
     at the previous fork. Fold the repoint into the carry, and write its
     message from
     [Carries that wait on a dependency](#carries-that-wait-on-a-dependency).
     If that bbolt
     version has the fix, leave out the carry and its repoints (see
     [If the new tag includes the fix](#when-a-rebase-changes-the-bbolt-version)).
   - Check that no carried change was lost. Every file this prints must be
     explained in the PR description (its carry was left out on purpose,
     upstream removed or replaced the file, or it is an obsolete downstream
     file, such as an empty placeholder):
     ```
     comm -23 <(git diff --name-only <latest-tag> $target | sort) \
              <(git diff --name-only <new-tag> HEAD | sort)
     ```
     `<latest-tag>` is the one from step 2 (for 3.6 to 3.7, `v3.6.16`), and
     `<new-tag>` is the tag you rebase onto. The check works per file, so it
     does not see a hunk lost inside a file. For each carry whose pick you
     resolved by hand, split or adapted, compare its hunks (`git show <sha>`)
     with `git diff <new-tag> HEAD -- <its files>`. For dependency carries,
     check the version in every module the carry changed.
   - Check that every commit on top of the new tag has a prefix that the
     carry lists can see (run it again after squashing); this prints nothing,
     and grep then exits 1:
     ```
     git log --no-merges --ancestry-path --format='%h %s' <new-tag>..HEAD \
       | grep -v -E '^[0-9a-f]+ UPSTREAM: (revert: )?(<carry>|<drop>|[0-9]+):'
     ```
   - Build (`GO_BUILD_FLAGS='-v' ./build.sh`) and compile the tests of each
     package a carry changed (`go test -run '^$' <packages>` in that
     package's module). Fix a carry's code or tests in that carry. Adapt
     OpenShift files to the new release, for example
     `discover-etcd-initial-cluster` code for a changed API, in
     `UPSTREAM: <carry>:` commits after the OpenShift files check.
6. Update the builder and base images (in `.ci-operator.yaml` and the
   `Dockerfile.*` files) if the new tag needs it, for example a newer Go than
   the `build_root_image` has. Run `go mod tidy` in every module (on 3.7 and
   later, `make fix-mod-tidy`), and regenerate the protobuf files with
   `./scripts/genproto.sh` (check them with `make verify-genproto` in a
   regular clone: in a `git worktree` it commits to your branch). Commit each
   as `UPSTREAM: <drop>:`.
7. Set the [minor release base tag](#maintaining-openshiftetcd) at the top of
   this document, on the branch you rebase, to the new tag, in its own
   commit, for example
   `UPSTREAM: <carry>: Set the minor release base tag to v3.7.3`. Build and
   test (see [Building and testing](#building-and-testing)), push the branch
   to your fork, and open a PR against `main`.
8. Before the PR merges, check whether `main` has moved since `$target`:
   ```
   git fetch openshift
   next=$(git rev-parse openshift/main)
   git log --no-merges --reverse --format='%h %s' $target..$next \
     ^$(git describe --tags --abbrev=0 --match 'v[0-9]*' $next)
   ```
   If `$next` is `$target`, you are done with this step. Otherwise, handle
   each listed commit as in step 5; for a listed revert of a carry you
   already picked, remove that pick. Then run
   `git merge -s ours --no-edit $next` and set `target=$next`, even if nothing
   was listed (for example, when `main` only merged a patch release): otherwise
   merging the PR brings `main`'s new commits into the new tree. Never sync
   with a plain `git merge openshift/main`: it brings the old branch's content
   into the new tree. Rerun the OpenShift files check against your squash
   commit (or your last OpenShift files pick, if you have not squashed yet)
   instead of `HEAD`, because the commits after it change some of those files
   on purpose.
   To keep this rare, ask the approvers in `OWNERS` for a short merge freeze
   on `main` in the last days before the rebase PR merges.
9. After the rebase PR merges, check that it kept the history, then find the
   open PRs that need a rebase. A PR with commits made on the old branch
   brings in commits that the `--ancestry-path` lists never show, even if
   its author merged the new `main` into it:
   ```
   git fetch openshift
   git merge-base --is-ancestor <new-tag> openshift/main &&
     git merge-base --is-ancestor <target from the PR description> openshift/main ||
     echo "history lost"
   ```
   If it prints `history lost`, stop and tell the approvers in `OWNERS`: the
   rebase PR did not keep the history. Otherwise, list the PRs:
   ```
   for pr in $(gh pr list -R openshift/etcd --base main --state open --limit 500 \
                 --json number --jq '.[].number'); do
     git fetch -q openshift "pull/$pr/head" || { echo "#$pr: fetch failed"; continue; }
     for c in $(git rev-list --no-merges openshift/main..FETCH_HEAD \
                  ^$(git describe --tags --abbrev=0 --match 'v[0-9]*' FETCH_HEAD)); do
       git merge-base --is-ancestor <new-tag> "$c" || { echo "#$pr needs a rebase"; break; }
     done
   done
   ```
   Each listed PR must be rebased onto the new `main` (not have `main` merged
   into it) before it merges. `git rebase openshift/main` on the PR branch
   replays only the PR's own commits; expect conflicts, since the tree is now
   on the new release. Tell the approvers in `OWNERS` when the rebase PR
   merges, so authors of open PRs know to rebase.

## Building and testing

- Build with the image build's invocation,
  `GOFLAGS='-mod=readonly' GO_BUILD_FLAGS='-v' ./build.sh` (`build.sh` fails if
  `GO_BUILD_FLAGS` is not set). Check that `bin/` has
  `etcd`, `etcdctl`, `etcdutl` and `discover-etcd-initial-cluster`; `make`
  builds only the first three.
- Test with `make test` and `go test ./openshift-tools/...`. `make test` runs
  the unit, integration, release and e2e passes and takes a long time;
  `make test-unit` runs only the unit tests.

## Downstream bbolt fork (openshift/bbolt)

The `go.mod` `replace` for `go.etcd.io/bbolt` points at `openshift/bbolt`, a
fork that removes the `madvise(MADV_RANDOM)` call to fix a page cache
regression on RHEL 10 kernels (OCPBUGS-103516,
https://github.com/etcd-io/bbolt/issues/939).

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
names its branch. The replace silently overrides `require`, so a merge can
succeed while the build still uses the old fork. This can happen in a patch
release too, for example etcd 3.7.5 moving to bbolt v1.5.1. If the versions
differ, check whether the new bbolt tag includes the `MADV_RANDOM` fix.

**If the new tag does not include the fix:**

1. Use the fork branch for the new tag. If there is none, cut one (see
   [Fork branches](#fork-branches)), for example `release-5.1-bbolt-v1.5.1`
   from `v1.5.1`.
2. Point the replace at its commit and run `go mod tidy` in each of the four
   module directories. Use `go mod edit` with the raw commit hash, and
   `go mod tidy` resolves the pseudo-version:
   ```bash
   for dir in . server etcdutl tests; do
     (cd $dir && go mod edit -replace go.etcd.io/bbolt=github.com/openshift/bbolt@<commit-hash> && go mod tidy) \
       || { echo "failed in $dir"; break; }
   done
   ```
   Then check that all four modules point at the commit you chose; this
   prints four lines, each ending in the first 12 characters of its hash:
   ```
   grep -H 'go.etcd.io/bbolt =>' go.mod server/go.mod etcdutl/go.mod tests/go.mod
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

## Payload and cluster testing

When CI and the PR presubmits are green, run the nightly payload jobs for the
OpenShift release you merge into. Replace 5.x with the release in the
`build_root_image` tag of the branch's `.ci-operator.yaml` (for example,
`openshift-5.1` means 5.1). The jobs test all of OpenShift, including upgrades:

```
/payload 5.x nightly informing
/payload 5.x nightly blocking
```

Check CPU, memory and disk usage for an upgrade job in PromeCIus, so the
rebase does not increase resource usage.

To debug on a specific platform (bare metal, vSphere) or variant (SNO), launch
a cluster with your PR from ClusterBot: `launch openshift/etcd#<PR>`.

OpenShift does not run etcd performance tests after a rebase. The upstream
community and
[SIG Scalability in Kubernetes](https://github.com/etcd-io/etcd/issues/14138#issuecomment-1247665949)
do, and the OpenShift scalability team tests upcoming y-stream releases.
