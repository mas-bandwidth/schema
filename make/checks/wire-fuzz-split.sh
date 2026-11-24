#!/bin/sh
# make/checks/wire-fuzz-split.sh — A LEG'S ARMS COVER THE CORPUS, DERIVED FROM
# THE ROSTER (docs/SPEC-TABLES.md §4.2, A LEG'S ARMS ARE HELD TO THE ROSTER).
#
#   sh make/checks/wire-fuzz-split.sh complete   <recipe> <plain log> <arm log>...
#   sh make/checks/wire-fuzz-split.sh complement <recipe> <plain log> <builder log>
#
# Every log is one `harness wire-fuzz` run of one leg over the committed corpus,
# and its summary line says how the harness split that corpus: `S seeds over R
# roots`, then `A seeds absent` (roots the leg has no codec for) and `O seeds
# outside the arm` (roots the arm has nothing to say about: a fixed or message
# root under --builder or --retain). The harness derives the outside count from
# the manifest, so it moves with the corpus and is never registered here: a
# number written into a recipe goes stale the day a seed lands (it did, twice:
# 219 registered, 315 measured).
#
# What this holds instead is the relation between the arms, read off the same
# run:
#
#   complete    the plain arm answers every seed (no absent), and each arm
#               after it answers every seed of its own roster (no absent), so
#               its seeds plus the ones outside it are the plain arm's seeds.
#   complement  the plain and builder arms split the corpus between them: the
#               builder arm leaves nothing absent, the seeds the plain arm
#               leaves absent are exactly as many as the builder arm answers,
#               and the seeds outside the builder arm are exactly the ones the
#               plain arm answers.
#
# A root that drops out of an arm, or out of both, moves one side of a relation
# and the recipe goes red naming the arm and both numbers. A log with no
# summary line is red: a run that said nothing proved nothing.
set -eu

usage() {
	echo "usage: wire-fuzz-split.sh complete <recipe> <plain log> <arm log>... | complement <recipe> <plain log> <builder log>" >&2
	exit 2
}
[ $# -ge 4 ] || usage
mode=$1 recipe=$2 plain=$3
shift 3

# split <log>: sets seeds, absent and outside from the log's summary line
split() {
	if [ ! -f "$1" ]; then
		echo "$recipe: $1 is missing: the arm's run left no log" >&2
		exit 1
	fi
	line=$(grep -E '^wire-fuzz[a-z-]*( retain)?: seed [0-9]+, [0-9]+ seeds over [0-9]+ roots' "$1" | tail -1 || true)
	if [ -z "$line" ]; then
		echo "$recipe: $1 carries no wire-fuzz summary line: the run proved nothing" >&2
		exit 1
	fi
	seeds=$(printf '%s\n' "$line" | sed -E 's/^[^,]*, ([0-9]+) seeds over .*/\1/')
	absent=$(printf '%s\n' "$line" | sed -nE 's/.*, ([0-9]+) (ledgered )?seeds absent.*/\1/p')
	outside=$(printf '%s\n' "$line" | sed -nE 's/.*, ([0-9]+) seeds outside the arm.*/\1/p')
	absent=${absent:-0}
	outside=${outside:-0}
}

split "$plain"
plain_seeds=$seeds plain_absent=$absent
if [ "$outside" -ne 0 ]; then
	echo "$recipe: the plain arm ($plain) counts $outside seeds outside it: the plain arm's roster is the whole corpus" >&2
	exit 1
fi

case "$mode" in
complete)
	if [ "$plain_absent" -ne 0 ]; then
		echo "$recipe: the plain arm ($plain) leaves $plain_absent seeds absent: the leg answers every root of the corpus" >&2
		exit 1
	fi
	for arm in "$@"; do
		split "$arm"
		if [ "$absent" -ne 0 ]; then
			echo "$recipe: $arm leaves $absent seeds absent: the leg answers every root of the arm" >&2
			exit 1
		fi
		if [ $((seeds + outside)) -ne "$plain_seeds" ]; then
			echo "$recipe: $arm answers $seeds seeds and counts $outside outside it, $((seeds + outside)) in all, where the plain arm answers $plain_seeds" >&2
			exit 1
		fi
		echo "$recipe: $arm answers $seeds seeds and $outside are outside it, the plain arm's $plain_seeds (derived from the roster)"
	done
	;;
complement)
	[ $# -eq 1 ] || usage
	split "$1"
	if [ "$absent" -ne 0 ]; then
		echo "$recipe: $1 leaves $absent seeds absent: the builder arm answers every root of its roster" >&2
		exit 1
	fi
	if [ "$plain_absent" -ne "$seeds" ]; then
		echo "$recipe: the plain arm ($plain) leaves $plain_absent seeds absent where the builder arm ($1) answers $seeds: the two arms do not split the corpus between them" >&2
		exit 1
	fi
	if [ "$outside" -ne "$plain_seeds" ]; then
		echo "$recipe: the builder arm ($1) counts $outside seeds outside it where the plain arm ($plain) answers $plain_seeds" >&2
		exit 1
	fi
	echo "$recipe: the plain arm answers $plain_seeds seeds and the builder arm the other $seeds (derived from the roster)"
	;;
*)
	usage
	;;
esac
