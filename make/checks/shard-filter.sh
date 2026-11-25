#!/bin/sh
# THE LINT SHARD FILTER. A lint gate over generated files runs whole, or as
# shard K of N. The gate hands this filter its whole list of files or units,
# the UNIVERSE, and every shard computes the same assignment from it: the
# entries are taken heaviest first (a file's bytes, a directory's kilobytes,
# the cost a formatter or a compiler pays for it) and each goes to the
# lightest shard so far, ties to the lower shard and then by path. So the N
# shards hold every entry of the universe exactly once and carry about equal
# weight. A line whose key is not in the universe (a ratchet line for a file
# no longer generated) lands by its key's POSIX cksum, K-1 mod N, so exactly
# one shard holds it red: a gate filters its ratchet through this filter with
# the same universe, and a shard never loosens it.
#
# usage: shard-filter.sh <K/N | ""> <universe-file> [key] < lines > lines-of-the-shard
#        an empty spec passes every line: the whole gate. With key "first",
#        a line is keyed by its first field (a ratchet line `<path> <kinds>`),
#        else by the whole line (a path).
set -eu
spec=${1-}
universe=${2-}
key=${3-line}
case "$key" in line | first) ;; *) echo "shard-filter: key \"$key\" is not line or first" >&2; exit 2 ;; esac
if [ -z "$spec" ]; then
	cat
	exit 0
fi
case "$spec" in */*) ;; *) echo "shard-filter: shard \"$spec\" is not K/N" >&2; exit 2 ;; esac
k=${spec%%/*}
n=${spec#*/}
case "$k" in '' | *[!0-9]*) echo "shard-filter: shard \"$spec\" is not K/N" >&2; exit 2 ;; esac
case "$n" in '' | *[!0-9]*) echo "shard-filter: shard \"$spec\" is not K/N" >&2; exit 2 ;; esac
if [ "$n" -lt 1 ] || [ "$k" -lt 1 ] || [ "$k" -gt "$n" ]; then
	echo "shard-filter: shard $spec: want 1 <= K <= N" >&2
	exit 2
fi
[ -r "$universe" ] || { echo "shard-filter: the universe \"$universe\" cannot be read" >&2; exit 2; }

# <weight> <path> for every entry of the universe; an entry that is not on
# disk is a gate's bookkeeping error, refused by name
weights=$(while IFS= read -r p; do
	[ -n "$p" ] || continue
	if [ -d "$p" ]; then
		w=$(du -sk "$p") || exit 1
		w=${w%%[ 	]*}
	elif [ -f "$p" ]; then
		w=$(wc -c < "$p") || exit 1
		w=$(echo $w)
	else
		echo "shard-filter: $p is in the universe but not on disk" >&2
		exit 1
	fi
	printf '%s %s\n' "$w" "$p"
done < "$universe") || exit 1
# the assignment: heaviest first, each to the lightest shard (LPT); one line
# `<shard> <path>` per entry of the universe
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
printf '%s\n' "$weights" | LC_ALL=C sort -k1,1nr -k2,2 | awk -v n="$n" '
	NF {
		best = 1
		for (s = 2; s <= n; s++) if (load[s] < load[best]) best = s
		load[best] += $1
		print best, $2
	}' > "$tmp/assigned"
# Y: in the universe and this shard's; C: not in the universe, so by cksum
awk -v k="$k" -v key="$key" '
	FNR == NR { shard[$2] = $1; next }
	{
		q = $0
		if (key == "first") q = $1
		if (q in shard) { if (shard[q] == k) print "Y\t" $0 }
		else print "C\t" $0
	}' "$tmp/assigned" - | while IFS= read -r tagged; do
	p=${tagged#?	}
	case "$tagged" in
	Y*) printf '%s\n' "$p" ;;
	*)
		q=$p
		if [ "$key" = first ]; then
			q=${p%%[ 	]*}
		fi
		h=$(printf '%s' "$q" | cksum)
		h=${h%% *}
		if [ $((h % n)) -eq $((k - 1)) ]; then
			printf '%s\n' "$p"
		fi
		;;
	esac
done
