#!/usr/bin/env bash
# release: tag one commit on main vMAJOR.MINOR.PATCH (make release).
#
# usage: tools/release/release.sh <version> <commit> <main-ref> [push]
#
# Refuses, by name, and before it writes anything:
#   - a version that does not parse as vMAJOR.MINOR.PATCH (no leading zeros);
#   - a commit that does not resolve;
#   - a commit that is not on <main-ref> (an ancestor of it, or it);
#   - a version whose tag already exists;
#   - a version not greater than the newest vMAJOR.MINOR.PATCH tag.
# Otherwise it makes that one annotated tag on that one commit, and, when
# <push> is 1, pushes that tag alone to origin. Nothing else is pushed.
set -euo pipefail

version=${1:-}
commit=${2:-HEAD}
main=${3:-origin/main}
push=${4:-}

refuse() {
  echo "release REFUSES: $*"
  exit 1
}

grammar='^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'
if ! [[ "$version" =~ $grammar ]]; then
  refuse "version '$version' is not vMAJOR.MINOR.PATCH; make release VERSION=vX.Y.Z"
fi
want=("${BASH_REMATCH[1]}" "${BASH_REMATCH[2]}" "${BASH_REMATCH[3]}")

sha=$(git rev-parse --verify --quiet "$commit^{commit}") || refuse "commit '$commit' does not resolve"
git rev-parse --verify --quiet "$main^{commit}" > /dev/null || refuse "main ref '$main' does not resolve"
if ! git merge-base --is-ancestor "$sha" "$main"; then
  refuse "commit $sha is not on $main; a release is cut from main only"
fi
if git rev-parse --verify --quiet "refs/tags/$version" > /dev/null; then
  refuse "tag $version already exists"
fi

# greater: is a.b.c > x.y.z, numerically
greater() {
  if [ "$1" -ne "$4" ]; then [ "$1" -gt "$4" ]; return; fi
  if [ "$2" -ne "$5" ]; then [ "$2" -gt "$5" ]; return; fi
  [ "$3" -gt "$6" ]
}

newest=""
newest_parts=(0 0 0)
for t in $(git tag -l 'v*'); do
  [[ "$t" =~ $grammar ]] || continue
  parts=("${BASH_REMATCH[1]}" "${BASH_REMATCH[2]}" "${BASH_REMATCH[3]}")
  if [ -z "$newest" ] || greater "${parts[@]}" "${newest_parts[@]}"; then
    newest=$t
    newest_parts=("${parts[@]}")
  fi
done
if [ -n "$newest" ] && ! greater "${want[@]}" "${newest_parts[@]}"; then
  refuse "version $version is not greater than the newest release tag $newest"
fi

git tag -a "$version" -m "serialize.rb $version" "$sha"
echo "release: tagged $version at $sha${newest:+ (previous: $newest)}"
if [ "$push" = 1 ]; then
  git push origin "refs/tags/$version"
  echo "release: pushed $version"
else
  echo "release: not pushed (PUSH=1 pushes the tag alone)"
fi
