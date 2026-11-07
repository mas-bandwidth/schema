#!/bin/sh
# make/squirrel-lint.sh SQ DIR... — the Squirrel port's lint of GENERATED code
# (make/schema-squirrel-generated-lint-gate.mk carries the why).
#
# Every *.nut file under the named directories is held to two checks, and every
# finding names its file:
#
#   COMPILE  the pinned interpreter compiles the file (`sq -c`, compile only,
#            no run) with exit 0 and NOTHING on stdout or stderr. Squirrel 3.2's
#            compiler has no warning class at all, so "no warnings" is "no
#            output": a compiler that ever learns to warn reds this gate by
#            name rather than being ignored.
#   FORMAT   the canonical text every emitter writes: LF line endings, no tab,
#            no trailing whitespace, one newline at the end of the file and no
#            blank line before it, and never two blank lines in a row. No
#            Squirrel formatter is pinnable (upstream ships none), so these are
#            the rules gofmt, dart format and clang-format hold for their ports
#            that apply to Squirrel text.
#
# An empty set of files is a refusal, never a pass.
set -eu

[ $# -ge 2 ] || { echo "usage: $0 SQ DIR..." >&2; exit 2; }
sq=$1
shift
[ -x "$sq" ] || command -v "$sq" >/dev/null 2>&1 || {
	echo "tool missing: sq ($sq); run make dist-squirrel (make/squirrel.mk)" >&2
	exit 1
}

list=$(find "$@" -type f -name '*.nut' | LC_ALL=C sort)
[ -n "$list" ] || { echo "squirrel lint: no .nut file under $* — this gate would pass over an empty set" >&2; exit 1; }

fail=0
files=0
for f in $list; do
	files=$((files + 1))
	if ! out=$("$sq" -c -o /dev/null "$f" 2>&1); then
		printf 'COMPILE %s: %s\n' "$f" "$(printf '%s' "$out" | tr '\n' ' ')"
		fail=1
	elif [ -n "$out" ]; then
		printf 'COMPILE %s: the compiler printed output: %s\n' "$f" "$(printf '%s' "$out" | tr '\n' ' ')"
		fail=1
	fi
	if [ -s "$f" ] && [ "$(tail -c 1 "$f" | od -An -tx1 | tr -d ' \n')" != 0a ]; then
		printf 'FORMAT %s: no newline at the end of the file\n' "$f"
		fail=1
	fi
done

# One awk pass over every file for the line rules. A blank line is a line with
# nothing on it; a line of spaces is trailing whitespace and is named as that.
tab=$(printf '\t')
cr=$(printf '\r')
# shellcheck disable=SC2086
if ! awk -v tab="$tab" -v cr="$cr" '
	function endfile() {
		if (name != "" && lastblank) { printf "FORMAT %s:%d: a blank line ends the file\n", name, lastline; bad = 1 }
	}
	FNR == 1 { endfile(); name = FILENAME; blanks = 0 }
	{
		lastline = FNR
		if (index($0, cr)) { printf "FORMAT %s:%d: carriage return\n", FILENAME, FNR; bad = 1 }
		if (index($0, tab)) { printf "FORMAT %s:%d: tab\n", FILENAME, FNR; bad = 1 }
		if ($0 ~ /[ ]$/) { printf "FORMAT %s:%d: trailing whitespace\n", FILENAME, FNR; bad = 1 }
		if ($0 == "") {
			blanks++
			if (blanks == 2) { printf "FORMAT %s:%d: two blank lines in a row\n", FILENAME, FNR; bad = 1 }
		} else blanks = 0
		lastblank = ($0 == "")
	}
	END { endfile(); exit bad }
' $list; then
	fail=1
fi

if [ "$fail" -ne 0 ]; then
	echo "squirrel lint: FAILED over $files generated .nut file(s) under $*"
	exit 1
fi
echo "squirrel lint: $files generated .nut file(s) under $* compile under $sq with no output and hold the canonical text"
