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

## Updating with `rebase.sh`

To finally rebase, a script that will merge and rebase along the happy path without automatic conflict resolution and at the end will create a PR for you.

Here are the steps:
1. Create a new OCPBUGS JIRA ticket with the respective OpenShift version to rebase. Please include the change logs in the ticket description. You can clone a previous rebase we did in [OCPBUGS-947](https://issues.redhat.com/browse/OCPBUGS-947) and adjust.
2. It's best to start off with a fresh fork of [openshift/etcd](https://github.com/openshift/etcd/). Stay on the master branch.
3. This script requires `jq`, `git`, `podman` and `bash`. `gh` is optional.
4. In the root dir of that fork run:
```
openshift-hack/rebase.sh --etcd-tag=v3.5.4 --openshift-release=openshift-4.12 --jira-id=666
```

where `etcd-tag` is the [etcd-io/etcd](https://github.com/etcd-io/etcd/) release tag, the `openshift-release`
is the OpenShift release branch in [openshift/etcd](https://github.com/openshift/etcd/) and the `jira-id` is the
number of the OCPBUGS ticket created in step (1).

5. In case of conflicts, it will ask you to step into another shell to resolve those. The script will continue by committing the resolution with `UPSTREAM: <drop>`.
6. At the end, there will be a "rebase-$VERSION" branch pushed to your fork.
7. If you have `gh` installed and are logged in, it will attempt to create a PR for you by opening a web browser.

## Building and testing

- Build the code with `make`
- Test the code with `make test`

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
