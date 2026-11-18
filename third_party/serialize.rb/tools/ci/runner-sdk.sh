#!/usr/bin/env bash
# Use the pinned interpreters already built on the runner's sdk when they are
# the pins (make/ruby-toolchain.mk), instead of restoring or building dist/.
#
# A per-commit job must not spend its time building CRuby, so before it
# touches a cache it looks for the pins where the runner keeps prebuilt
# toolchains: $HOME/sdk/ruby-<version>. An install is used only when its own
# RUBY_VERSION is the pinned release, the sdk's ruby-<version>.tar.gz (or a
# PIN file beside the install) has the pinned sha256, and, for the floor, its
# RuboCop is the locked RuboCop. Nothing is copied: dist/ruby-<version> becomes
# a small directory whose bin/, lib/, include/ and share/ link to the sdk tree,
# and its PIN (and GEMS marker) makes the Makefile default to the pins.
#
# usage: tools/ci/runner-sdk.sh [root]
#
# Exit 0 when both pins and the analyzer were found and dist/ now names them;
# exit 1 with a line saying why not, so the caller can restore the cache or
# build in the cache-warming job.
set -euo pipefail

root=${1:-.}
mk="$root/make/ruby-toolchain.mk"
lock="$root/make/ruby-gems.lock"

if [ ! -f "$mk" ] || [ ! -f "$lock" ]; then
  echo "runner-sdk: no $mk or $lock"
  exit 1
fi

pin() { sed -n "s/^$1[[:space:]]*:=[[:space:]]*//p" "$mk" | head -n1; }
sha256_of() { { sha256sum "$1" 2>/dev/null || shasum -a 256 "$1"; } | cut -d' ' -f1; }
version_of() { "$1" --disable-gems -e 'print RUBY_VERSION' 2>/dev/null || true; }

floor=$(pin RUBY_FLOOR_VERSION)
current=$(pin RUBY_CURRENT_VERSION)
rubocop=$(pin RUBY_RUBOCOP_VERSION)
if [ -z "$floor" ] || [ -z "$current" ] || [ -z "$rubocop" ]; then
  echo "runner-sdk: $mk does not name the pins"
  exit 1
fi

sdk=${SERIALIZE_RB_SDK:-${HOME:-/nonexistent}/sdk}
if [ ! -d "$sdk" ]; then
  echo "runner-sdk: no prebuilt pins under $sdk"
  exit 1
fi

install_dir() {
  local v=$1 d
  for d in "$sdk/ruby-$v" "$sdk/$v"; do
    if [ -x "$d/bin/ruby" ]; then
      printf '%s\n' "$d"
      return 0
    fi
  done
  return 1
}

# The install must be the pin by sha, not merely by the directory's name: the
# sdk keeps the ruby-lang.org tarball it built from, or a PIN beside the tree.
sha_evidence() {
  local v=$1 want t got f d
  want=$(pin "RUBY_SHA256_$v")
  [ -n "$want" ] || return 1
  for t in "$sdk/dl/ruby-$v.tar.gz" "$sdk/ruby-$v.tar.gz"; do
    if [ -f "$t" ]; then
      got=$(sha256_of "$t")
      if [ "$got" != "$want" ]; then
        echo "runner-sdk: $t has sha256 $got, the pin is $want; not using it"
        return 1
      fi
      return 0
    fi
  done
  d=$(install_dir "$v") || return 1
  for f in "$d/PIN" "$d/.pin"; do
    if [ -f "$f" ] && grep -q "$want" "$f"; then
      return 0
    fi
  done
  return 1
}

link_install() {
  local v=$1 d=$2 dst sub
  dst="$root/dist/ruby-$v"
  rm -rf "$dst"
  mkdir -p "$dst"
  for sub in bin lib include share; do
    if [ -e "$d/$sub" ]; then ln -s "$d/$sub" "$dst/$sub"; fi
  done
  printf '%s %s %s\n' "$v" "$(pin "RUBY_SHA256_$v")" "$d" > "$dst/PIN"
  if [ "$v" = "$floor" ]; then cp "$lock" "$dst/GEMS"; fi
}

floor_dir=$(install_dir "$floor") || floor_dir=
current_dir=$(install_dir "$current") || current_dir=
if [ -z "$floor_dir" ] || [ -z "$current_dir" ]; then
  echo "runner-sdk: no prebuilt CRuby $floor and $current under $sdk"
  exit 1
fi

if ! sha_evidence "$floor" || ! sha_evidence "$current"; then
  echo "runner-sdk: the prebuilt pins under $sdk are not the pinned sha256"
  exit 1
fi

if [ "$(version_of "$floor_dir/bin/ruby")" != "$floor" ]; then
  echo "runner-sdk: $floor_dir is not CRuby $floor"
  exit 1
fi
if [ "$(version_of "$current_dir/bin/ruby")" != "$current" ]; then
  echo "runner-sdk: $current_dir is not CRuby $current"
  exit 1
fi

rubocop_got=$(env -u RUBYOPT -u RUBYLIB -u GEM_HOME -u GEM_PATH "$floor_dir/bin/ruby" -e 'load Gem.bin_path("rubocop", "rubocop")' -- --version 2>/dev/null || true)
if [ "$rubocop_got" != "$rubocop" ]; then
  echo "runner-sdk: $floor_dir has RuboCop \"${rubocop_got:-none}\", the lock is RuboCop $rubocop"
  exit 1
fi

link_install "$floor" "$floor_dir"
link_install "$current" "$current_dir"
echo "runner-sdk: using the prebuilt pins under $sdk (CRuby $floor and $current, RuboCop $rubocop)"
