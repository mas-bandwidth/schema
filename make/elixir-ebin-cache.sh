#!/bin/sh
# A content-addressed store for one compiled Elixir ebin, shared by the jobs of
# one CI host (make/elixir.mk, ELIXIR_EBIN_CACHE; the verify-leg Elixir rows).
#
#   elixir-ebin-cache.sh <cache-dir> <out-dir> <input file>... -- <compile command>...
#
# THE KEY is the SHA-256 of every input file's bytes and relative path, the
# toolchain banner ($ELIXIR_EBIN_TOOLCHAIN, `elixir --version`: the Elixir and
# erts versions) and $ERL_COMPILER_OPTIONS. An entry is written only after the
# compile command (the strict wrapper) exited 0, so a hit stands for a strict
# compile of the same bytes by the same compiler under the same options that
# passed; a change to any generated source, to the driver, to the strict
# wrapper or to the toolchain is a different key and compiles. Nothing here
# reads a branch name, a commit or a clock.
#
# THE ENTRY carries a manifest, written when it is stored: the SHA-256 and the
# path of every file the compile wrote. A hit copies the entry into <out-dir>
# and then checks the copy against the manifest in full: a file whose bytes
# differ (truncated, overwritten, the same size with one byte changed), a file
# the manifest names that is not there, a file there that the manifest does not
# name, or an entry with no readable manifest, and the entry is DISCARDED, the
# discard is said on stderr (and as a workflow warning under GitHub Actions),
# and the compile runs as on a miss. A hit is never a count, a name or an age
# (F-217: a zero-byte .beam under the right name was handed back as a hit).
# An entry is built under a temporary name and renamed into place whole, so a
# job killed mid-store leaves no half entry under a key.
#
# One lock per key: of several jobs that want the same ebin at once, one
# compiles and the others wait and copy, instead of all of them compiling the
# same corpus on the same cores.
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
	echo "elixir-ebin-cache v2"
	echo "toolchain: ${ELIXIR_EBIN_TOOLCHAIN:-}"
	echo "erl compiler options: ${ERL_COMPILER_OPTIONS:-}"
	printf '%s\n' "$sums"
} | sha256sum | cut -d' ' -f1)
entry="$cache/$key"

# manifest <dir>: every file under <dir> but the manifest, one "<sha256>  ./<path>"
# line each, sorted by path. Paths are the compiler's module file names.
manifest() {
	(cd "$1" && find . -type f ! -name .manifest | LC_ALL=C sort | while IFS= read -r f; do sha256sum "$f"; done)
}

# discard <why>: the entry is not the ebin it was stored as; say so and drop it.
discard() {
	echo "elixir-ebin-cache: DISCARDED entry $key: $1; compiling" >&2
	if [ "${GITHUB_ACTIONS:-}" = true ]; then
		echo "::warning title=elixir-ebin-cache::DISCARDED entry $key: $1; compiling"
	fi
	rm -rf "$entry" "$out"
	mkdir -p "$out"
}

exec 9>"$entry.lock"
flock 9

if [ -d "$entry" ]; then
	why=
	if [ ! -s "$entry/.manifest" ]; then
		why="it has no manifest"
	else
		rm -rf "$out"
		mkdir -p "$out"
		if ! cp -R "$entry/." "$out/" 2>/dev/null; then
			why="it could not be copied"
		else
			rm -f "$out/.manifest"
			got=$(manifest "$out")
			want=$(cat "$entry/.manifest")
			if [ "$got" != "$want" ]; then
				# name each difference: a changed, a missing and an extra file
				why=$(printf '%s\n' "$got" | awk -v want="$want" '
					BEGIN {
						n = split(want, lines, "\n")
						for (i = 1; i <= n; i++) if (split(lines[i], f, "  ") == 2) w[f[2]] = f[1]
					}
					split($0, f, "  ") == 2 { g[f[2]] = f[1] }
					END {
						for (p in w) if (!(p in g)) s = s ", " p " is missing"; else if (w[p] != g[p]) s = s ", " p " has other bytes"
						for (p in g) if (!(p in w)) s = s ", " p " is not in it"
						print (s == "" ? "its manifest does not read back" : "against its manifest" substr(s, 2))
					}')
			fi
		fi
	fi
	if [ -z "$why" ]; then
		touch "$entry"
		echo "elixir-ebin-cache: hit $key ($(printf '%s\n' "$want" | wc -l | tr -d ' ') files, each the bytes of the entry's manifest): the strict compile of these sources, by this toolchain, under these options, passed before"
		exit 0
	fi
	discard "$why"
fi

echo "elixir-ebin-cache: miss $key; compiling"
"$@"
status=$?
if [ "$status" -ne 0 ]; then
	exit "$status"
fi

# Store, then prune. A failure here never fails the compile that passed.
tmp=$(mktemp -d "$cache/.tmp.XXXXXX" 2>/dev/null) || exit 0
if cp -R "$out/." "$tmp/" 2>/dev/null && manifest "$tmp" > "$tmp.manifest" && mv "$tmp.manifest" "$tmp/.manifest" && [ "$(manifest "$out")" = "$(cat "$tmp/.manifest")" ]; then
	rm -rf "$entry"
	mv "$tmp" "$entry" 2>/dev/null || rm -rf "$tmp"
else
	rm -rf "$tmp" "$tmp.manifest"
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
