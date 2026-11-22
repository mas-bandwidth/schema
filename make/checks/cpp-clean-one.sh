#!/bin/sh
# One translation unit of the C++ warning gate (cpp-clean-warnings.sh): $1 is a
# generated .h (compiled as `#include "<name>"`, the way a consumer includes it)
# or a generated .cpp (compiled as it is). Its diagnostics go to $OUT/logs.
set -u
f=$1
log="$OUT/logs/$(printf '%s' "$f" | tr / _).log"
d=$(dirname "$f")
case $f in
*.h)
	printf '#include "%s"\n' "$(basename "$f")" | \
		$CLANGXX $WFLAGS -I"$d" $incs -isystem "$SERIALIZE_DIR" -x c++ - > "$log" 2>&1
	status=$? ;;
*)
	$CLANGXX $WFLAGS -I"$d" $incs -isystem "$SERIALIZE_DIR" "$f" > "$log" 2>&1
	status=$? ;;
esac
# A compile ERROR is a failure of the gate, never a clean file. The compiler's
# own exit status is the verdict (warnings alone exit 0: there is no -Werror
# here), so a `fatal error:` such as a missing header, a crash, or a compiler
# that could not start is red too. Matching `: error: ` alone let a file that
# stopped at a fatal error count as warning-free.
if [ "$status" -ne 0 ] || grep -qE ': (fatal )?error: ' "$log"; then
	echo "tables-cpp-clean: $f does not compile (clang exit $status):" >&2
	grep -E ': (fatal )?error: ' "$log" | head -5 >&2
	exit 255
fi
exit 0
