#!/bin/sh
# THE FORMAT GATES' INPUT AND STATUS CONTROLS (make/c.mk
# tables-c-format-negative-control, make/cpp.mk
# tables-cpp-format-negative-control). A format gate that scans nothing, or
# that reads a failed formatter's bytes as a verdict, is green for no reason.
# Each case below hands the engine one such input and requires it RED, with
# the line that names the cause; the guard first requires the same engine
# GREEN on the canonical tree every case is built from.
#
# usage: clean-format-controls.sh <c|cpp> <canonical-tree> <scratch-dir>
# env:   CLANG_FORMAT
set -eu
lang=$1
clean=$2
out=$3
CLANG_FORMAT=${CLANG_FORMAT:-clang-format}
case "$lang" in
c) engine="make/checks/c-clean-format.sh"; mode="" ;;
cpp) engine="make/checks/cpp-clean-format.sh"; mode="check" ;;
*) echo "clean-format-controls: unknown language $lang"; exit 1 ;;
esac

rm -rf "$out" && mkdir -p "$out/empty" "$out/tree"
: > "$out/allow.txt"
cp -R "$clean" "$out/tree/unit"
mkdir -p "$out/tree/hollow"

# a formatter that writes the right bytes and then fails
real=$(command -v "$CLANG_FORMAT")
printf '#!/bin/sh\n"%s" "$@"\nexit 9\n' "$real" > "$out/exit9"
chmod +x "$out/exit9"

# run <name> <formatter> <expect-dirs|UNSET> <tree>...
run() {
	name=$1; cf=$2; expect=$3; shift 3
	if [ "$expect" = UNSET ]; then
		( unset EXPECT_DIRS; LC_ALL=C CLANG_FORMAT=$cf OUT="$out/$name" sh "$engine" "$out/allow.txt" $mode "$@" ) > "$out/$name.out" 2>&1
	else
		LC_ALL=C CLANG_FORMAT=$cf EXPECT_DIRS=$expect OUT="$out/$name" sh "$engine" "$out/allow.txt" $mode "$@" > "$out/$name.out" 2>&1
	fi
}

run guard "$CLANG_FORMAT" "$out/tree/unit" "$out/tree/unit" || \
	{ echo "NEGATIVE CONTROL FAILED: the $lang format gate is not green on the canonical tree, so its controls would watch a gate that was red anyway"; cat "$out/guard.out"; exit 1; }

# red <name> <phrase the output must carry> <formatter> <expect-dirs|UNSET> <tree>...
red() {
	name=$1; phrase=$2; shift 2
	if run "$name" "$@"; then
		echo "NEGATIVE CONTROL FAILED: the $lang format gate stayed green on: $name"; cat "$out/$name.out"; exit 1
	fi
	grep -Fq -- "$phrase" "$out/$name.out" || \
		{ echo "NEGATIVE CONTROL FAILED: the $lang format gate went red on $name, but not by name ($phrase)"; cat "$out/$name.out"; exit 1; }
}

red empty-tree "no generated" "$CLANG_FORMAT" "" "$out/empty"
red missing-tree "is not a directory" "$CLANG_FORMAT" "" "$out/absent"
red one-missing-tree "is not a directory" "$CLANG_FORMAT" "" "$out/tree/unit" "$out/absent"
red one-empty-tree "no generated" "$CLANG_FORMAT" "" "$out/tree/unit" "$out/empty"
red missing-unit "put no generated" "$CLANG_FORMAT" "$out/tree/unit $out/tree/gone" "$out/tree"
red empty-unit "put no generated" "$CLANG_FORMAT" "$out/tree/unit $out/tree/hollow" "$out/tree"
red unit-outside-trees "put no generated" "$CLANG_FORMAT" "$clean" "$out/tree"
red expectation-unset "EXPECT_DIRS is not set" "$CLANG_FORMAT" UNSET "$out/tree/unit"
red formatter-exit-9 "exited non-zero" "$out/exit9" "$out/tree/unit" "$out/tree/unit"

sh make/checks/clean-format-path-corpus.sh "$lang" "$out"

echo "negative control: the $lang format gate is RED on an empty tree, a missing tree, one missing tree of two, a missing or empty unit directory, an unset expectation, and a formatter that exits 9 with the right bytes"
