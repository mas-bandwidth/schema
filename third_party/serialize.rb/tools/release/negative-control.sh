#!/usr/bin/env bash
# The release tool's refusals (make release-negative-control). Builds a
# scratch repository under build/ (no remote, no network) and requires each
# refusal by its name, then one good tag at the right commit.
set -euo pipefail

repo=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
tool="$repo/tools/release/release.sh"
nc="$repo/build/release-nc"
rm -rf "$nc"
mkdir -p "$nc"
cd "$nc"

# the scratch repository's own identity and settings, whatever the host's are
export GIT_CONFIG_COUNT=5
export GIT_CONFIG_KEY_0=user.name GIT_CONFIG_VALUE_0=release-control
export GIT_CONFIG_KEY_1=user.email GIT_CONFIG_VALUE_1=release-control@example.invalid
export GIT_CONFIG_KEY_2=commit.gpgsign GIT_CONFIG_VALUE_2=false
export GIT_CONFIG_KEY_3=tag.gpgsign GIT_CONFIG_VALUE_3=false
export GIT_CONFIG_KEY_4=init.defaultBranch GIT_CONFIG_VALUE_4=main

git init --quiet .
git commit --quiet --allow-empty -m a
git tag -a v1.0.0 -m v1.0.0
git tag -a v1.0.9 -m v1.0.9
git commit --quiet --allow-empty -m b
on_main=$(git rev-parse HEAD)
git checkout --quiet -b side
git commit --quiet --allow-empty -m c
off_main=$(git rev-parse HEAD)
git checkout --quiet main

# expect_refusal <what> <message> <release.sh args...>
expect_refusal() {
  local what=$1 msg=$2
  shift 2
  if bash "$tool" "$@" > out.log 2>&1; then
    cat out.log
    echo "NEGATIVE CONTROL FAILED: $what was released"
    exit 1
  fi
  if ! grep -q "release REFUSES: .*$msg" out.log; then
    cat out.log
    echo "NEGATIVE CONTROL FAILED: $what was refused, but not by \"$msg\""
    exit 1
  fi
  echo "release-negative-control: $what -> refused ($msg)"
}

expect_refusal "a version with no v" "is not vMAJOR.MINOR.PATCH" 1.1.0 "$on_main" main
expect_refusal "a two-part version" "is not vMAJOR.MINOR.PATCH" v1.1 "$on_main" main
expect_refusal "a leading zero" "is not vMAJOR.MINOR.PATCH" v1.01.0 "$on_main" main
expect_refusal "an empty version" "is not vMAJOR.MINOR.PATCH" "" "$on_main" main
expect_refusal "a commit off main" "is not on main" v1.1.0 "$off_main" main
expect_refusal "a commit that does not resolve" "does not resolve" v1.1.0 no-such-commit main
expect_refusal "an existing tag" "already exists" v1.0.9 "$on_main" main
expect_refusal "a version below the newest tag" "is not greater than the newest release tag v1.0.9" v1.0.8 "$on_main" main
if git rev-parse --verify --quiet refs/tags/v1.1.0 > /dev/null; then
  echo "NEGATIVE CONTROL FAILED: a refused release left the tag v1.1.0 behind"
  exit 1
fi

# v1.0.10 > v1.0.9 numerically, not as text
if ! bash "$tool" v1.0.10 "$on_main" main > out.log 2>&1; then
  cat out.log
  echo "NEGATIVE CONTROL FAILED: v1.0.10 on main after v1.0.9 was refused"
  exit 1
fi
got=$(git rev-parse 'v1.0.10^{commit}')
if [ "$got" != "$on_main" ]; then
  echo "NEGATIVE CONTROL FAILED: v1.0.10 tags $got, not $on_main"
  exit 1
fi
echo "release-negative-control: v1.0.10 on main after v1.0.9 -> tagged at the commit asked for"
echo "release-negative-control: every refusal names its reason, and a good release tags exactly one commit"
