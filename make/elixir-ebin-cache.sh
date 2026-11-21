#!/bin/sh
# A content-addressed store for one compiled Elixir ebin, shared by the jobs of
# one CI host (make/elixir.mk, ELIXIR_EBIN_CACHE).
#
#   elixir-ebin-cache.sh <cache-dir> <out-dir> <input file>... -- <compile command>...
#
# The key is the SHA-256 of every input file's bytes and relative path, the
# toolchain banner ($ELIXIR_EBIN_TOOLCHAIN, `elixir --version`: the Elixir and
# erts versions) and $ERL_COMPILER_OPTIONS. An entry is written only after the
# compile command (the strict wrapper) exited 0, so a hit stands for a strict
# compile of the same bytes by the same compiler under the same options that
# passed; a change to any generated source, to the driver, to the strict
# wrapper or to the toolchain is a different key and compiles. Nothing here
# reads a branch name, a commit or a clock.
#
# One lock per key: of several jobs that want the same ebin at once, one
# compiles and the others wait and copy, instead of all of them compiling the
# same corpus on the same cores. An entry is moved into place whole (rename),
# so a job cut mid-copy leaves no half entry; a hit whose file count is not the
# recorded one is treated as a miss.
#
# Without flock or sha256sum (macOS), or when the cache directory cannot be
# made, the compile command runs as if this script were not there.
set -u

cache=$1
out=$2
shift 2
inputs=
while [ "$#" -gt 0 ] && [ "$1" != "--" ]; do
	inputs="$inputs $1"
	shift
done
if [ "$#" -eq 0 ]; then
	echo "elixir-ebin-cache: no -- before the compile command" >&2
	exit 2
fi
shift

keep=${ELIXIR_EBIN_CACHE_KEEP:-8}

if ! command -v flock >/dev/null 2>&1 || ! command -v sha256sum >/dev/null 2>&1; then
	echo "elixir-ebin-cache: flock or sha256sum is missing; compiling without the cache"
	exec "$@"
fi
# $inputs is a list of generated paths (no spaces) and may carry globs.
# shellcheck disable=SC2086
if ! sums=$(sha256sum $inputs 2>/dev/null); then
	echo "elixir-ebin-cache: an input is missing; compiling without the cache"
	exec "$@"
fi
if ! mkdir -p "$cache" 2>/dev/null; then
	echo "elixir-ebin-cache: cannot make $cache; compiling without the cache"
	exec "$@"
fi

key=$({
	echo "elixir-ebin-cache v1"
	echo "toolchain: ${ELIXIR_EBIN_TOOLCHAIN:-}"
	echo "erl compiler options: ${ERL_COMPILER_OPTIONS:-}"
	printf '%s\n' "$sums"
} | sha256sum | cut -d' ' -f1)
entry="$cache/$key"

count() {
	find "$1" -type f ! -name .complete | wc -l | tr -d ' '
}

exec 9>"$entry.lock"
flock 9

if [ -f "$entry/.complete" ]; then
	rm -rf "$out"
	mkdir -p "$out"
	if cp -R "$entry/." "$out/" 2>/dev/null && rm -f "$out/.complete" && [ "$(count "$out")" = "$(cat "$entry/.complete")" ]; then
		touch "$entry"
		echo "elixir-ebin-cache: hit $key ($(count "$out") files): the strict compile of these sources, by this toolchain, under these options, passed before"
		exit 0
	fi
	echo "elixir-ebin-cache: entry $key is not whole; compiling"
	rm -rf "$entry" "$out"
	mkdir -p "$out"
fi

echo "elixir-ebin-cache: miss $key; compiling"
"$@"
status=$?
if [ "$status" -ne 0 ]; then
	exit "$status"
fi

# Store, then prune. A failure here never fails the compile that passed.
tmp=$(mktemp -d "$cache/.tmp.XXXXXX" 2>/dev/null) || exit 0
if cp -R "$out/." "$tmp/" 2>/dev/null && count "$tmp" > "$tmp/.complete"; then
	rm -rf "$entry"
	mv "$tmp" "$entry" 2>/dev/null || rm -rf "$tmp"
else
	rm -rf "$tmp"
fi

# Keep the $keep most recently used entries; an entry another job holds the
# lock of is in use and is left. Lock files stay (a waiter may hold one) until
# a week old with no entry beside them.
n=0
for d in $(ls -1dt "$cache"/*/ 2>/dev/null); do
	d=${d%/}
	n=$((n + 1))
	if [ "$n" -le "$keep" ] || [ "$d" = "$entry" ]; then
		continue
	fi
	(
		flock -n 8 || exit 0
		rm -rf "$d"
	) 8>"$d.lock"
done
find "$cache" -maxdepth 1 -name '.tmp.*' -mmin +120 -exec rm -rf {} + 2>/dev/null
for l in $(find "$cache" -maxdepth 1 -name '*.lock' -mtime +7 2>/dev/null); do
	[ -d "${l%.lock}" ] || rm -f "$l"
done
exit 0
