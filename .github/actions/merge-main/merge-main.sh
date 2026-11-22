#!/usr/bin/env bash
# THE LANE MERGE (.github/actions/merge-main, footgun 2026-11-22).
#
# A pull request whose base is a lane (rowan/fix-ledger-1122, a sprint stream,
# dev) is checked out by actions/checkout as refs/pull/N/merge: the PR merged
# into its LANE. Nothing in that tree is current main, so a lane that has
# drifted from main goes green on every PR into it and goes red only when the
# lane lands. This script makes the checkout the PR merged into its lane AND
# merged with current main, so every gate after it checks what will land.
#
# A push, a dispatch, and a pull request into main are left exactly as checked
# out. A lane that does not merge cleanly with main FAILS here, by name, with
# the conflicting paths: merge main into the lane first.
#
# Inputs: GITHUB_EVENT_NAME and GITHUB_BASE_REF (set by Actions);
# MERGE_MAIN_BRANCH (default main) and MERGE_MAIN_REMOTE (default origin) for
# the tests in internal/ci/merge_main_test.go.
set -euo pipefail

event=${GITHUB_EVENT_NAME:?merge-main: GITHUB_EVENT_NAME is not set}
base=${GITHUB_BASE_REF:-}
main=${MERGE_MAIN_BRANCH:-main}
remote=${MERGE_MAIN_REMOTE:-origin}

if [ "$event" != pull_request ]; then
  echo "merge-main: a $event event, not a pull request: the checkout is tested as it is"
  exit 0
fi
if [ -z "$base" ]; then
  echo "::error title=merge-main::a pull_request event with no GITHUB_BASE_REF: cannot tell whether this pull request targets $main"
  exit 1
fi
if [ "$base" = "$main" ]; then
  echo "merge-main: a pull request into $main: the checkout is already the merge with $main"
  exit 0
fi

# A default actions/checkout is one commit deep, which has no merge base with
# main. Deepen it blobless: commits and trees only, a few seconds on this
# repository; the merge fetches the blobs it needs.
fetch=(--quiet --no-tags --no-recurse-submodules)
if [ "$(git rev-parse --is-shallow-repository)" = true ]; then
  fetch+=(--unshallow --filter=blob:none)
fi
git fetch "${fetch[@]}" "$remote" "+refs/heads/$main:refs/remotes/$remote/$main"

tip=$(git rev-parse --short "$remote/$main")
behind=$(git rev-list --count "HEAD..$remote/$main")
echo "merge-main: this pull request targets the lane $base, which is $behind commit(s) behind $main; every gate below runs on the merge with $main at $tip"
if [ "$behind" = 0 ]; then
  exit 0
fi

log=$(mktemp)
if ! git -c user.name='schema CI' -c user.email='ci@schema.invalid' \
    merge --no-ff --no-edit -m "CI: the $base lane merged with $main at $tip" "$remote/$main" >"$log" 2>&1; then
  cat "$log"
  conflicts=$(git diff --name-only --diff-filter=U | tr '\n' ' ')
  echo "::error title=lane does not merge with main::the lane $base does not merge cleanly with $main at $tip ($behind commit(s) behind). Conflicting paths: ${conflicts:-(none named; see the merge output above)}. Merge $main into $base, then push: this pull request is checked against what will land, not against the lane alone."
  exit 1
fi
rm -f "$log"
echo "merge-main: merged $main at $tip into the checkout; HEAD is now $(git rev-parse --short HEAD)"
