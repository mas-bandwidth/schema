#!/bin/sh
# make/python-toolchain-check.sh — the Python leg's pins are what they say
# (make/python-toolchain.mk, python-toolchain-check; docs/PORT-PYTHON.md,
# THE PIN).
#
# usage: sh make/python-toolchain-check.sh [root]
#
# Reads PYTHON, PYTHON_FLOOR, MYPY and the expected versions from the
# environment make exports, and refuses, by pin name:
#
#   - an interpreter whose own sys.version_info is not its pinned release;
#   - an analyzer whose own --version is not the locked mypy;
#   - any file of the leg under <root> (default: the working directory) that
#     runs an interpreter it would find on PATH: a bare python3 command, an
#     env shebang, or a Go exec.Command or exec.LookPath of a python.
#
# Every interpreter runs isolated (-I): no PYTHON* environment variable, no
# user site directory, no script directory on sys.path.
set -u
root=${1:-.}
fail=0

refuse() {
	echo "python-toolchain-check REFUSES: $*"
	fail=1
}

version_of() {
	"$1" -I -c 'import sys; print("%d.%d.%d" % sys.version_info[:3])' 2>/dev/null
}

check_interpreter() {
	pin=$1 path=$2 want=$3
	got=$(version_of "$path")
	if [ "$got" != "$want" ]; then
		refuse "the python leg's pinned $pin is CPython ${got:-<no answer>}, the pin is $want"
		echo "  $pin = $path"
		echo "  make dist-python builds the pinned interpreters into dist/"
	fi
}

check_interpreter PYTHON "${PYTHON:?PYTHON is not set; run this through make python-toolchain-check}" "${PYTHON_CURRENT_VERSION:?}"
check_interpreter PYTHON_FLOOR "${PYTHON_FLOOR:?}" "${PYTHON_FLOOR_VERSION:?}"

mypy_want=${PYTHON_MYPY_VERSION:?}
mypy_got=$("${MYPY:?}" --version 2>/dev/null)
case "$mypy_got" in
"mypy $mypy_want "* | "mypy $mypy_want") ;;
*)
	refuse "the python leg's pinned MYPY reports \"${mypy_got:-<no answer>}\", the lock is mypy $mypy_want"
	echo "  MYPY = $MYPY"
	echo "  make dist-python installs it from make/python-mypy.lock"
	;;
esac

# THE PATH GATE. The leg's files: its make fragments and scripts, its Go
# packages, its conformance driver, and the staged serialize.py's code and
# Makefile. A line that names the pinned path
# (dist/python-3.11.18/bin/python3.11) is no PATH lookup; the patterns below
# match a command word, not a path component.
files=$(cd "$root" && {
	find make internal/codegen test/conformance compiler \
		\( -path 'make/python*' -o -path 'internal/codegen/python*' -o -path 'test/conformance/python*' -o -path 'compiler/*python*' \) \
		-type f 2>/dev/null
	find third_party/serialize.py \( -name Makefile -o -name '*.py' -o -name '*.sh' -o -name '*.mk' \) -type f 2>/dev/null
} | sort)
if [ -z "$files" ]; then
	refuse "found none of the leg's files under $root; the gate would have checked nothing"
fi
for f in $files; do
	[ "$f" = make/python-toolchain-check.sh ] && continue
	hits=$(cd "$root" && grep -nE \
		-e '(^|[^A-Za-z0-9_./$-])python3([^A-Za-z0-9_.-]|$)' \
		-e '/usr/bin/env[[:space:]]+python' \
		-e '(Command|LookPath)\("python' \
		"$f" | grep -vE '^[0-9]+:[[:space:]]*(#|//)')
	if [ -n "$hits" ]; then
		refuse "$f runs an interpreter from PATH, not a pin:"
		echo "$hits" | sed 's/^/  /'
	fi
done

if [ $fail -ne 0 ]; then
	exit 1
fi
echo "python-toolchain-check: PYTHON is CPython $PYTHON_CURRENT_VERSION, PYTHON_FLOOR is CPython $PYTHON_FLOOR_VERSION, MYPY is mypy $mypy_want; $(echo "$files" | wc -l | tr -d ' ') leg files run no interpreter from PATH"
