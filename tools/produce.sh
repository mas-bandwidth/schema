#!/bin/sh
# produce.sh: a producer's built tree, carried from the job that builds it
# (ci-fast.yml's `produce`) to the jobs that use it.
#
#   produce.sh pack <name> <path>...   tar the paths into build/produce/<name>.tgz
#   produce.sh unpack <dir>            unpack every <dir>/*.tgz into the checkout
#                                      and stand what it holds newer than it
#
# A tar keeps the build's file modes (an artifact drops the executable bit)
# and its mtimes. Unpacked, every path is set to ONE time a second after the
# checkout's own files: make rebuilds a target only when a prerequisite is
# strictly newer, so the carried tree is up to date against the checkout and
# against itself, directories included. Every failure is loud: a pack with a
# path its build did not make, or an unpack with nothing to unpack, exits
# non-zero naming it.
set -eu
cmd=${1:-}
case "$cmd" in
  pack)
    [ "$#" -ge 3 ] || { echo 'usage: produce.sh pack <name> <path>...' >&2; exit 2; }
    name=$2
    shift 2
    for p in "$@"; do
      [ -e "$p" ] || { echo "produce.sh: $name: $p was not built" >&2; exit 1; }
    done
    mkdir -p build/produce
    tar -czf "build/produce/$name.tgz" "$@"
    printf 'produce.sh: packed %s: %s\n' "$name" "$*"
    ;;
  unpack)
    [ "$#" -eq 2 ] || { echo 'usage: produce.sh unpack <dir>' >&2; exit 2; }
    dir=$2
    set -- "$dir"/*.tgz
    [ -f "$1" ] || { echo "produce.sh: $dir holds no tree to unpack" >&2; exit 1; }
    sleep 1
    at=$(date +%s)
    for t in "$@"; do
      paths=$(tar -tzf "$t" | sed -e 's#/$##' -e 's#^\([^/]*/[^/]*\).*#\1#' | sort -u)
      tar -xzf "$t"
      # shellcheck disable=SC2086 # one word per top path
      find $paths -exec touch -h -d "@$at" {} +
      printf 'produce.sh: unpacked %s: %s\n' "$t" "$(echo $paths)"
    done
    ;;
  *)
    echo 'usage: produce.sh pack <name> <path>... | unpack <dir>' >&2
    exit 2
    ;;
esac
