#!/bin/sh
# Run from the repository root: clean-format-path-corpus.sh <c|cpp> <scratch-dir>.
set -eu
lang=$1
root=$(pwd)
corpus=$root/make/checks/testdata/format-paths
case "$lang" in
c|cpp) engine=$root/make/checks/$lang-clean-format.sh ;;
*) echo "unknown format engine: $lang" >&2; exit 2 ;;
esac

mkdir -p "$2"
work=$(mktemp -d "$2/path-cases.XXXXXX")
trap 'rm -rf "$work"' 0

# Freeze the roster before invoking an engine. awk also writes the final row
# with a newline when the input file ends immediately after its last field.
awk -F "$(printf '\t')" '
BEGIN {
	n = split("ordinary-path empty-ratchet missing-ratchet stale-outside-tree space-in-tree", required, " ")
}
/^#/ || /^$/ { next }
{
	if (NF != 5 || $1 == "" || $2 == "" || $3 == "" || $4 == "" || $5 == "") {
		print FILENAME ":" FNR ": expected five nonempty fields" > "/dev/stderr"
		bad = 1
		next
	}
	if (seen[$1]++) {
		print FILENAME ":" FNR ": duplicate case " $1 > "/dev/stderr"
		bad = 1
	}
	print
}
END {
	for (i = 1; i <= n; i++) {
		if (!(required[i] in seen)) {
			print FILENAME ": missing required case " required[i] > "/dev/stderr"
			bad = 1
		}
	}
	exit bad
}
' "$corpus/cases.tsv" > "$work/cases.tsv"

# The ratchet's outside.h exists relative to the working directory. Both scan
# trees contain the same clean bytes, so only the tree's spelling varies.
cp "$root/.clang-format" "$work/.clang-format"
cp "$corpus/clean.h" "$work/outside.h"
mkdir -p "$work/ordinary" "$work/tree with space"
cp "$corpus/clean.h" "$work/ordinary/clean.h"
cp "$corpus/clean.h" "$work/tree with space/clean.h"

# Log the path the real formatter receives so ignoring the spaced input cannot pass.
real_format=$(command -v "${CLANG_FORMAT:-clang-format}")
case "$real_format" in /*) ;; *) real_format=$root/$real_format ;; esac
export real_format
cat > "$work/trace-format" <<'SH'
#!/bin/sh
for input do :; done
printf '%s\n' "$input" >> visits
exec "$real_format" "$@"
SH
chmod +x "$work/trace-format"

failed=0
while IFS="$(printf '\t')" read -r name tree ratchet want detail; do
	case "$name" in ''|'#'*) continue ;; esac
	set -- "$corpus/$ratchet"
	if [ "$lang" = cpp ]; then set -- "$@" check; fi
	status=0
	: > "$work/visits"
	( cd "$work" && LC_ALL=C EXPECT_DIRS='' CLANG_FORMAT=./trace-format OUT=results sh "$engine" "$@" "$tree" ) > "$work/case.log" 2>&1 || status=$?
	case "$want" in
	pass) [ "$status" -eq 0 ] && [ "$(cat "$work/visits")" = "$detail" ] ;;
	reject) [ "$status" -ne 0 ] && grep -Fq -- "$detail" "$work/case.log" ;;
	*) echo "unknown outcome $want in $name" >&2; exit 2 ;;
	esac && { echo "PASS $lang $name (exit $status)"; continue; }
	echo "FAIL $lang $name (expected $want: $detail; exit $status)"
	cat "$work/case.log"
	failed=1
done < "$work/cases.tsv"
exit "$failed"
