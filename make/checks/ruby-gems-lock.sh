#!/bin/sh
# make/checks/ruby-gems-lock.sh LOCK — the Ruby analyzers' gem lock
# (make/ruby-gems.lock) is read whole before dist-ruby verifies and installs
# it (make/ruby-toolchain.mk, dist/ruby-%/GEMS).
#
# A lock whose last line has no newline is refused: the install's shell `read`
# loop returns false on that line and drops it, so that gem was neither
# fetched nor held to its sha256, and a copy already under dist/ruby-gems was
# installed unchecked by `gem install dist/ruby-gems/*.gem`. A line that is not
# `name==version sha256:<64 hex>`, and a lock that names no gem, are refused
# too. Prints the gem count, which the install holds its verified count to.
# Toolchain-free and milliseconds; its controls are the committed fixtures
# under testdata/ruby-gems-lock-controls (ruby-gems-lock-controls).
set -eu -o pipefail
lock=$1
fail() { echo "ruby gems lock $lock: $1" >&2; exit 1; }
[ -f "$lock" ] || fail "the lock does not exist"
[ -z "$(tail -c 1 "$lock")" ] || fail "the lock does not end in a newline, so its last line would not be read"
bad=$(sed -E -e '/^(#.*)?$/d' -e '/^[A-Za-z0-9_.-]+==[0-9A-Za-z_.-]+ sha256:[0-9a-f]{64}$/d' "$lock" | sed -n 1p)
[ -z "$bad" ] || fail "\"$bad\" is not name==version sha256:<64 hex>"
n=$(sed -E '/^(#.*)?$/d' "$lock" | wc -l | tr -d ' ')
[ "$n" -gt 0 ] || fail "the lock names no gem"
echo "ruby gems lock $lock: $n gem(s)"
