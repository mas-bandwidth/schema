#!/bin/sh
# THE ELIXIR CORPUS EBIN, HANDED FROM THE COMPILING ROWS TO THE ROWS THAT RUN
# IT (ci-fast.yml: lang-clean (elixir K/N) give, verify-leg (elixir,
# conformance|fuzz) take; make/elixir.mk ELIXIR_EBIN_FROM).
#
#   elixir-ebin-handoff.sh give <ebin> <dest> <options> <unit>... -- <input>...
#   elixir-ebin-handoff.sh take <from> <ebin> <options> <unit>... -- <input>...
#
# The strict compile of the 42-unit table corpus is about 200 s of CPU, more
# than one runner gives a job in two minutes, so the lang-clean rows compile it
# as shards side by side and the verify rows, which need the whole corpus, take
# their shards instead of compiling it again.
#
# GIVE writes <dest>: the shard's ebin, the units it compiled (units.txt), the
# SHA-256 of every source it compiled from (inputs.sha256: the generated units,
# the driver, the strict wrapper), the Erlang compiler options it ran under
# (options.txt), the toolchain that compiled it (toolchain.txt), and a
# manifest of every .beam's SHA-256 (.manifest).
#
# TAKE reads every shard under <from> and builds <ebin> from them, and refuses
# (exit 1, saying why) unless all of this holds:
#   - every shard's ebin is exactly the files of its manifest, byte for byte:
#     a .beam truncated, changed at the same size, missing or added, or a
#     manifest missing, empty or forged, refuses (F-217's class: a handed-over
#     artifact that is not the one compiled is never used);
#   - every source a shard names has the same SHA-256 here, in the tree this
#     job generated: the shards are this commit's compile, not another's;
#   - every shard ran under the options this job asks for, and was compiled
#     by the toolchain this job runs ($ELIXIR_EBIN_TOOLCHAIN, `elixir
#     --version`: a .beam from a newer OTP does not load in an older one);
#   - the shards' units are the corpus, each unit in exactly one shard, and
#     every source here (<input>...) was compiled by some shard.
# Nothing is compiled on a refusal and no <ebin> is left: a verify row with no
# whole, current ebin is red, never green on a part, and never past the cap
# compiling it. Under GitHub Actions the refusal is also a workflow error, so
# it is seen on the run page.
set -eu

die() {
	echo "elixir-ebin-handoff: $*" >&2
	if [ "${GITHUB_ACTIONS:-}" = true ]; then
		echo "::error title=elixir-ebin-handoff::$*"
	fi
	exit 1
}

# manifest <dir>: one "<sha256>  ./<file>" line per file under <dir>, sorted.
manifest() {
	(cd "$1" && find . -type f | LC_ALL=C sort | while IFS= read -r f; do sha256sum "$f"; done)
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

# toolchain: the OTP release, the erts version and the Elixir line of
# $ELIXIR_EBIN_TOOLCHAIN; the banner's [smp:..] [ds:..] fields are the host's
# core count, not the compiler, and the shards come from other hosts.
toolchain() {
	printf '%s\n' "${ELIXIR_EBIN_TOOLCHAIN:-}" | sed -n -e 's/^\(Erlang\/OTP [0-9][0-9]* \[erts-[^]]*\]\).*/\1/p' -e '/^Elixir /p'
}

mode=${1-}
case "$mode" in
give)
	[ $# -ge 4 ] || die "usage: give <ebin> <dest> <options> <unit>... -- <input>..."
	ebin=$2 dest=$3 options=$4
	shift 4
	rm -rf "$dest"
	mkdir -p "$dest/ebin"
	: > "$dest/units.txt"
	while [ $# -gt 0 ] && [ "$1" != "--" ]; do
		printf '%s\n' "$1" >> "$dest/units.txt"
		shift
	done
	[ $# -gt 0 ] || die "give: no -- before the inputs"
	shift
	[ -s "$dest/units.txt" ] || die "give: no units"
	ls "$ebin"/*.beam > /dev/null 2>&1 || die "give: $ebin holds no .beam"
	cp "$ebin"/*.beam "$dest/ebin/"
	# the inputs may be globs over the generated tree
	# shellcheck disable=SC2068
	sha256sum $@ > "$dest/inputs.sha256" || die "give: an input is missing"
	printf '%s\n' "$options" > "$dest/options.txt"
	toolchain > "$dest/toolchain.txt"
	manifest "$dest/ebin" > "$dest/.manifest"
	echo "elixir-ebin-handoff: gave $(wc -l < "$dest/units.txt" | tr -d ' ') units, $(wc -l < "$dest/.manifest" | tr -d ' ') files, compiled under options '$options'"
	;;
take)
	[ $# -ge 4 ] || die "usage: take <from> <ebin> <options> <unit>... -- <input>..."
	from=$2 ebin=$3 options=$4
	shift 4
	units=
	while [ $# -gt 0 ] && [ "$1" != "--" ]; do
		units="$units $1"
		shift
	done
	[ $# -gt 0 ] || die "take: no -- before the inputs"
	shift
	# the inputs may be globs over the generated tree
	# shellcheck disable=SC2068
	inputs=$(ls -1 $@ 2>/dev/null | LC_ALL=C sort -u)
	[ -n "$inputs" ] || die "take: no input is here"
	[ -d "$from" ] || die "take: $from is not a directory: no lang-clean (elixir) shard was handed over"
	shards=$(find "$from" -name units.txt | LC_ALL=C sort)
	[ -n "$shards" ] || die "take: $from holds no shard"
	rm -rf "$ebin"
	part="$ebin.part"
	rm -rf "$part"
	mkdir -p "$part"
	seen=$(mktemp)
	named=$(mktemp)
	here=$(mktemp)
	held=$(mktemp)
	trap 'rm -rf "$seen" "$named" "$here" "$held" "$part"' EXIT
	for u in $shards; do
		s=$(dirname "$u")
		[ -s "$s/.manifest" ] || die "take: shard $s has no manifest"
		got=$(manifest "$s/ebin")
		want=$(cat "$s/.manifest")
		[ "$got" = "$want" ] || die "take: shard $s is not the ebin it was given as: $(diffs "$want" "$got")"
		[ -f "$s/options.txt" ] && [ "$(cat "$s/options.txt")" = "$options" ] ||
			die "take: shard $s was compiled under options '$(cat "$s/options.txt" 2>/dev/null)', this job asks for '$options'"
		[ -f "$s/toolchain.txt" ] && [ "$(cat "$s/toolchain.txt")" = "$(toolchain)" ] ||
			die "take: shard $s was compiled by '$(tr '\n' ' ' < "$s/toolchain.txt" 2>/dev/null)', this job runs '$(toolchain | tr '\n' ' ')'"
		[ -s "$s/inputs.sha256" ] || die "take: shard $s names no inputs"
		sha256sum -c --quiet "$s/inputs.sha256" > /dev/null 2>&1 ||
			die "take: shard $s was compiled from other sources than this tree's: $(sha256sum -c --quiet "$s/inputs.sha256" 2>&1 | head -3 | tr '\n' ' ')"
		cat "$u" >> "$seen"
		sed 's/^[0-9a-f]*  //' "$s/inputs.sha256" >> "$named"
		for b in "$s"/ebin/*.beam; do
			n=$(basename "$b")
			echo "$n" >> "$held"
			[ -f "$part/$n" ] || cp "$b" "$part/$n"
		done
	done
	# Every shard compiles the conformance driver beside its units, so the
	# driver's modules are in every shard; their bytes differ by the source
	# path the compiler records (each runner has its own checkout), and the
	# driver source each compiled is the one sha256sum -c just matched to this
	# tree, so the first shard's copy is taken. A module in more than one shard
	# but not in all of them is a unit two shards both compiled: refused.
	nshards=$(printf '%s\n' "$shards" | wc -l | tr -d ' ')
	split=$(LC_ALL=C sort "$held" | uniq -c | awk -v n="$nshards" '$1 > 1 && $1 != n { printf " %s", $2 }')
	[ -z "$split" ] || die "take: modules in more than one shard but not in all:$split"
	# every source here was compiled by some shard, and no shard compiled a
	# source that is not here (sha256sum -c above refuses those)
	printf '%s\n' "$inputs" > "$here"
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
		die "take: the shards are not the corpus: missing [${missing# }], not in the corpus [${extra# }]"
	fi
	mv "$part" "$ebin"
	echo "elixir-ebin-handoff: took $nshards shards, $(printf '%s\n' "$have" | wc -l | tr -d ' ') units, $(ls "$ebin" | wc -l | tr -d ' ') files, each the bytes of its shard's manifest, compiled from this tree's sources under options '$options'"
	;;
*)
	die "usage: elixir-ebin-handoff.sh give|take ..."
	;;
esac
