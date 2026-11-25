#!/bin/sh
# produce.sh: a producer's built tree, carried from the job that builds it
# (ci-fast.yml's `produce`) to the jobs that use it.
#
#   produce.sh identity                print what a tree built here is built from
#   produce.sh pack <name> <path>...   tar the paths into build/produce/<name>.tgz
#                                      and record the identity in <name>.id
#   produce.sh unpack <dir>            check every <dir>/*.tgz was built from this
#                                      checkout's source, then unpack it and stand
#                                      what it holds newer than the checkout
#
# A carried tree may stand in for a build only for the exact source it was
# built from, as tools/go-ci-corpus.sh's corpus may: the identity is the merged
# tree (every tracked file, the pins in SIBLINGS and the Makefile, the
# workflow's tags) and the Go toolchain. Jobs merge main on their own, so the
# commit differs between jobs while the tree does not; when main moved between
# the producer and a consumer the trees differ and unpack refuses, naming both.
#
# A tar keeps the build's file modes (an artifact drops the executable bit)
# and its mtimes. Unpacked, every entry the tar holds, and nothing else, is set
# to ONE time a second after the checkout's own files: make rebuilds a target
# only when a prerequisite is strictly newer, so the carried tree is up to date
# against the checkout and against itself, directories included. That stamp is
# only sound because the identity check ran first. Every failure is loud: a
# dirty tracked tree, a pack with a path its build did not make, an unpack with
# nothing to unpack, a tree with no identity or another one, each exits
# non-zero naming it.
set -eu -o pipefail
identity() {
  printf '%s %s-%s\n' "$(git rev-parse 'HEAD^{tree}')" "$(go env GOVERSION)" "$(go env GOARCH)"
}
clean() {
  git diff --quiet HEAD -- || { echo 'produce.sh: tracked source differs from HEAD, so HEAD^{tree} does not name what is built' >&2; exit 1; }
}
cmd=${1:-}
case "$cmd" in
  identity)
    [ "$#" -eq 1 ] || { echo 'usage: produce.sh identity' >&2; exit 2; }
    identity
    ;;
  pack)
    [ "$#" -ge 3 ] || { echo 'usage: produce.sh pack <name> <path>...' >&2; exit 2; }
    name=$2
    shift 2
    clean
    for p in "$@"; do
      [ -e "$p" ] || { echo "produce.sh: $name: $p was not built" >&2; exit 1; }
    done
    mkdir -p build/produce
    tar -czf "build/produce/$name.tgz" "$@"
    identity > "build/produce/$name.id"
    printf 'produce.sh: packed %s for %s: %s\n' "$name" "$(cat "build/produce/$name.id")" "$*"
    ;;
  unpack)
    [ "$#" -eq 2 ] || { echo 'usage: produce.sh unpack <dir>' >&2; exit 2; }
    dir=$2
    set -- "$dir"/*.tgz
    [ -f "$1" ] || { echo "produce.sh: $dir holds no tree to unpack" >&2; exit 1; }
    clean
    want=$(identity)
    for t in "$@"; do
      id=${t%.tgz}.id
      [ -f "$id" ] || { echo "produce.sh: $t has no identity ($id): what it was built from is unknown" >&2; exit 1; }
      got=$(cat "$id")
      [ "$got" = "$want" ] || { echo "produce.sh: $t was built from $got, this checkout is $want" >&2; exit 1; }
    done
    sleep 1
    at=$(date +%s)
    for t in "$@"; do
      tar -xzf "$t"
      tar -tzf "$t" | sed 's#/$##' | tr '\n' '\0' | xargs -0 touch -h -d "@$at"
      printf 'produce.sh: unpacked %s: %s entries\n' "$t" "$(tar -tzf "$t" | wc -l | tr -d ' ')"
    done
    ;;
  *)
    echo 'usage: produce.sh identity | pack <name> <path>... | unpack <dir>' >&2
    exit 2
    ;;
esac
