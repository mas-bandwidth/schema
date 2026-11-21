#!/bin/sh
# THE DETERMINISTIC SPLIT the sharded per-commit rows share (ci-fast.yml
# lang-clean: the Elixir corpus compile, make/elixir.mk ELIXIR_TABLE_SHARD; the
# C format gate, make/checks/c-clean-format.sh C_FORMAT_SHARD).
#
#   shard.sh K/N < list
#
# prints the lines of the list whose 1-based position p has (p - 1) mod N equal
# to K - 1, in the order they came. Shards 1/N .. N/N of the same list are
# disjoint and together are the whole list, every line exactly once; that is
# held by internal/ci TestShardSplitIsAPartition, so a row that ran shard K/N
# can stand for its share of the unsharded gate and nothing else. The caller
# sorts the list first, so a shard is a function of the list's contents.
#
# Anything but two whole numbers with 1 <= K <= N exits 2 and prints nothing:
# a row that asked for a shard that does not exist must not pass on no work.
set -eu
spec=${1-}
k=${spec%%/*}
n=${spec#*/}
case "$spec" in
*/*) ;;
*) echo "shard.sh: '$spec' is not K/N" >&2; exit 2 ;;
esac
case "$k$n" in
'' | *[!0-9]*) echo "shard.sh: '$spec' is not K/N with whole numbers" >&2; exit 2 ;;
esac
case "$k" in '' | 0*) echo "shard.sh: '$spec': K is not a number from 1" >&2; exit 2 ;; esac
case "$n" in '' | 0*) echo "shard.sh: '$spec': N is not a number from 1" >&2; exit 2 ;; esac
if [ "$k" -gt "$n" ]; then
	echo "shard.sh: '$spec': K is past N" >&2
	exit 2
fi
awk -v k="$k" -v n="$n" '(NR - 1) % n == k - 1'
