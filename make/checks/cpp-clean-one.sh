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
		$CLANGXX $WFLAGS -I"$d" $incs -isystem "$SERIALIZE_DIR" -x c++ - > "$log" 2>&1 ;;
*)
	$CLANGXX $WFLAGS -I"$d" $incs -isystem "$SERIALIZE_DIR" "$f" > "$log" 2>&1 ;;
esac
# a compile ERROR is a failure of the gate, never a clean file
if grep -q ': error: ' "$log"; then
	echo "tables-cpp-clean: $f does not compile:" >&2
	grep ': error: ' "$log" | head -5 >&2
	exit 255
fi
exit 0
