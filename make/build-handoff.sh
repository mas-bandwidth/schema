#!/bin/sh
# A BUILD HANDED FROM THE ROWS THAT COMPILE IT, IN SHARDS, TO THE ROWS THAT RUN
# IT WHOLE (ci-fast.yml: the lang-clean (c|elixir K/N) rows give, the
# verify-leg-handoff rows take). Two builds use it: the Elixir table corpus
# ebin (make/elixir.mk ELIXIR_EBIN_FROM) and the C conformance driver's
# objects (make/c.mk C_CONFORMANCE_FROM).
#
#   build-handoff.sh give <built> <dest> <options> <unit>... -- <input>...
#   build-handoff.sh take <from> <built> <options> <unit>... -- <input>...
#
# Each is past what one runner gives a job in two minutes when one job
# compiles all of it (the Elixir corpus about 200 s of CPU, the C driver's
# objects about 400 s at -O2), so the lang-clean rows compile it as shards
# side by side and the verify rows, which need all of it, take the shards
# instead of compiling it again.
#
# GIVE writes <dest>: every built file under <built> (in built/; a name that
# starts with a dot is a make stamp, never handed over), the units it
# compiled (units.txt), the SHA-256 of every input it compiled from
# (inputs.sha256: the sources and every header or wrapper they read), the
# compiler options it ran under (options.txt), the toolchain that compiled it
# (toolchain.txt, from $BUILD_HANDOFF_TOOLCHAIN, the compiler's --version), and
# a manifest of every built file's SHA-256 (.manifest).
#
# TAKE reads every shard under <from> and builds <built> from them, and
# refuses (exit 1, saying why) unless all of this holds:
#   - every shard's built files are exactly the files of its manifest, byte
#     for byte: a file truncated, changed at the same size, missing or added,
#     or a manifest missing, empty or forged, refuses (F-217's class: a
#     handed-over artifact that is not the one compiled is never used);
#   - every input a shard names has the same SHA-256 here, in the tree this
#     job generated: the shards are this commit's compile, not another's;
#   - every shard ran under the options this job asks for, and was compiled
#     by the toolchain this job runs (an Erlang banner is read for its OTP,
#     erts and Elixir versions only: its [smp:..] [ds:..] fields are the
#     host's core count, and the shards come from other hosts);
#   - the shards' units are the build, each unit in exactly one shard, and
#     every input here (<input>...) was compiled by some shard.
# Nothing is compiled on a refusal and no <built> is left: a verify row with
# no whole, current build is red, never green on a part, and never past the
# cap compiling it. Under GitHub Actions the refusal is also a workflow error,
# so it is seen on the run page.
set -eu

die() {
	echo "build-handoff: $*" >&2
	if [ "${GITHUB_ACTIONS:-}" = true ]; then
		echo "::error title=build-handoff::$*"
	fi
	exit 1
}

# built <dir>: the build's own files under <dir>, "./<file>", sorted. A name
# that starts with a dot (a make stamp) is make's record, never the build:
# not handed over, not in a manifest, never taken.
builtfiles() {
	(cd "$1" && find . -type f ! -name '.*' | LC_ALL=C sort)
}

# manifest <dir>: one "<sha256>  ./<file>" line per built file under <dir>.
manifest() {
	builtfiles "$1" | while IFS= read -r f; do (cd "$1" && sha256sum "$f"); done
}

# diffs <want> <got>: names each file that differs between two manifests.
diffs() {
	printf '%s\n' "$2" | awk -v want="$1" '
		BEGIN {
			n = split(want, lines, "\n")
			for (i = 1; i <= n; i++) if (split(lines[i], f, "  ") == 2) w[f[2]] = f[1]
		}
		split($0, f, "  ") == 2 { g[f[2]] = f[1] }
		END {
			for (p in w) if (!(p in g)) s = s ", " p " is missing"; else if (w[p] != g[p]) s = s ", " p " has other bytes"
			for (p in g) if (!(p in w)) s = s ", " p " is not in it"
			print (s == "" ? "the manifest does not read back" : substr(s, 3))
		}'
}

# toolchain: $BUILD_HANDOFF_TOOLCHAIN without what names the host. An Erlang
# banner keeps its OTP release, erts version and Elixir line; any other
# banner (cc --version) keeps every non-empty line.
toolchain() {
	case "${BUILD_HANDOFF_TOOLCHAIN:-}" in
	*Erlang/OTP*)
		printf '%s\n' "$BUILD_HANDOFF_TOOLCHAIN" | sed -n -e 's/^\(Erlang\/OTP [0-9][0-9]* \[erts-[^]]*\]\).*/\1/p' -e '/^Elixir /p'
		;;
	*)
		printf '%s\n' "${BUILD_HANDOFF_TOOLCHAIN:-}" | sed '/^[[:space:]]*$/d'
		;;
	esac
}

# inputs <pattern>...: one path per line. A glob is expanded here, and one
# that matches nothing names nothing (an include directory with no header);
# any other name is kept as given, so a named input that is missing refuses.
inputs() {
	for p in "$@"; do
		case "$p" in
		*[*?[]*)
			for f in $p; do
				[ -e "$f" ] && printf '%s\n' "$f"
			done
			;;
		*) printf '%s\n' "$p" ;;
		esac
	done
	return 0
}

mode=${1-}
case "$mode" in
give)
	[ $# -ge 4 ] || die "usage: give <built> <dest> <options> <unit>... -- <input>..."
	built=$2 dest=$3 options=$4
	shift 4
	rm -rf "$dest"
	mkdir -p "$dest/built"
	: > "$dest/units.txt"
	while [ $# -gt 0 ] && [ "$1" != "--" ]; do
		printf '%s\n' "$1" >> "$dest/units.txt"
		shift
	done
	[ $# -gt 0 ] || die "give: no -- before the inputs"
	shift
	[ -s "$dest/units.txt" ] || die "give: no units"
	[ -d "$built" ] || die "give: $built holds nothing"
	list=$(builtfiles "$built")
	[ -n "$list" ] || die "give: $built holds nothing"
	printf '%s\n' "$list" | while IFS= read -r f; do
		mkdir -p "$(dirname "$dest/built/$f")"
		cp "$built/$f" "$dest/built/$f"
	done
	named=$(inputs "$@" | LC_ALL=C sort -u)
	[ -n "$named" ] || die "give: no inputs"
	# shellcheck disable=SC2086
	sha256sum $named > "$dest/inputs.sha256" || die "give: an input is missing"
	printf '%s\n' "$options" > "$dest/options.txt"
	toolchain > "$dest/toolchain.txt"
	manifest "$dest/built" > "$dest/.manifest"
	echo "build-handoff: gave $(wc -l < "$dest/units.txt" | tr -d ' ') units, $(wc -l < "$dest/.manifest" | tr -d ' ') files, compiled under options '$options'"
	;;
take)
	[ $# -ge 4 ] || die "usage: take <from> <built> <options> <unit>... -- <input>..."
	from=$2 built=$3 options=$4
	shift 4
	units=
	while [ $# -gt 0 ] && [ "$1" != "--" ]; do
		units="$units $1"
		shift
	done
	[ $# -gt 0 ] || die "take: no -- before the inputs"
	shift
	here_inputs=$(inputs "$@" | while IFS= read -r f; do [ -e "$f" ] && printf '%s\n' "$f"; done | LC_ALL=C sort -u)
	[ -n "$here_inputs" ] || die "take: no input is here"
	[ -d "$from" ] || die "take: $from is not a directory: no lang-clean shard was handed over"
	shards=$(find "$from" -name units.txt | LC_ALL=C sort)
	[ -n "$shards" ] || die "take: $from holds no shard"
	rm -rf "$built"
	part="$built.part"
	rm -rf "$part"
	mkdir -p "$part"
	seen=$(mktemp)
	named=$(mktemp)
	here=$(mktemp)
	held=$(mktemp)
	files=$(mktemp)
	trap 'rm -rf "$seen" "$named" "$here" "$held" "$files" "$part"' EXIT
	for u in $shards; do
		s=$(dirname "$u")
		[ -s "$s/.manifest" ] || die "take: shard $s has no manifest"
		got=$(manifest "$s/built")
		want=$(cat "$s/.manifest")
		[ "$got" = "$want" ] || die "take: shard $s is not the build it was given as: $(diffs "$want" "$got")"
		[ -f "$s/options.txt" ] && [ "$(cat "$s/options.txt")" = "$options" ] ||
			die "take: shard $s was compiled under options '$(cat "$s/options.txt" 2>/dev/null)', this job asks for '$options'"
		[ -f "$s/toolchain.txt" ] && [ "$(cat "$s/toolchain.txt")" = "$(toolchain)" ] ||
			die "take: shard $s was compiled by '$(tr '\n' ' ' < "$s/toolchain.txt" 2>/dev/null)', this job runs '$(toolchain | tr '\n' ' ')'"
		[ -s "$s/inputs.sha256" ] || die "take: shard $s names no inputs"
		sha256sum -c --quiet "$s/inputs.sha256" > /dev/null 2>&1 ||
			die "take: shard $s was compiled from other sources than this tree's: $(sha256sum -c --quiet "$s/inputs.sha256" 2>&1 | head -3 | tr '\n' ' ')"
		cat "$u" >> "$seen"
		sed 's/^[0-9a-f]*  //' "$s/inputs.sha256" >> "$named"
		builtfiles "$s/built" > "$files"
		while IFS= read -r f; do
			echo "$f" >> "$held"
			if [ ! -f "$part/$f" ]; then
				mkdir -p "$(dirname "$part/$f")"
				cp "$s/built/$f" "$part/$f"
			fi
		done < "$files"
	done
	# A file every shard built (the Elixir conformance driver, compiled beside
	# each shard's units) differs between shards by the source path the
	# compiler records (each runner has its own checkout); the inputs each
	# compiled are the ones sha256sum -c just matched to this tree, so the
	# first shard's copy is taken. A file in more than one shard but not in all
	# of them is a unit two shards both compiled: refused.
	nshards=$(printf '%s\n' "$shards" | wc -l | tr -d ' ')
	split=$(LC_ALL=C sort "$held" | uniq -c | awk -v n="$nshards" '$1 > 1 && $1 != n { printf " %s", $2 }')
	[ -z "$split" ] || die "take: files in more than one shard but not in all:$split"
	# every input here was compiled by some shard, and no shard compiled an
	# input that is not here (sha256sum -c above refuses those)
	printf '%s\n' "$here_inputs" > "$here"
	unread=$(LC_ALL=C sort -u "$named" | LC_ALL=C comm -13 - "$here" | tr '\n' ' ')
	[ -z "$unread" ] || die "take: no shard compiled $unread"
	# shellcheck disable=SC2086
	want=$(printf '%s\n' $units | LC_ALL=C sort)
	have=$(LC_ALL=C sort "$seen")
	twice=$(printf '%s\n' "$have" | uniq -d | tr '\n' ' ')
	[ -z "$twice" ] || die "take: units in more than one shard: $twice"
	if [ "$have" != "$want" ]; then
		missing=$(printf '%s\n' "$want" | awk -v have="$have" 'BEGIN { n = split(have, h, "\n"); for (i = 1; i <= n; i++) got[h[i]] = 1 } !($0 in got) { printf " %s", $0 }')
		extra=$(printf '%s\n' "$have" | awk -v want="$want" 'BEGIN { n = split(want, w, "\n"); for (i = 1; i <= n; i++) asked[w[i]] = 1 } !($0 in asked) { printf " %s", $0 }')
		die "take: the shards are not the build: missing [${missing# }], not in it [${extra# }]"
	fi
	mkdir -p "$(dirname "$built")"
	mv "$part" "$built"
	echo "build-handoff: took $nshards shards, $(printf '%s\n' "$have" | wc -l | tr -d ' ') units, $(builtfiles "$built" | wc -l | tr -d ' ') files, each the bytes of its shard's manifest, compiled from this tree's sources under options '$options'"
	;;
*)
	die "usage: build-handoff.sh give|take ..."
	;;
esac
