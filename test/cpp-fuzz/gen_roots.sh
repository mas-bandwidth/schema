#!/bin/sh
# gen_roots.sh <generated-dir> <roots.inc>
#
# Emit the compile-time list of roots for one generated schema unit. The entry
# point is one function per root, driven through every reader kind; fuzz_all
# calls them in turn so a single libFuzzer binary covers every table the schema
# declares. A root is VARIABLE class when its LoadMeasure takes the wire file
# directly (it then needs a region), and FIXED class when Load takes a value.
#
# The counts it emits are the harness's coverage assertion: a generated unit
# with no readable root is a broken corpus entry, not a corpus entry that
# fuzzes nothing, so this refuses by name. FUZZ_OPEN_ROOTS counts the roots
# that carry the strict cooked Open; the rest carry Load and FromJson only.
set -eu

dir=$1
out=$2

ns=$(grep -h -m1 -E '^namespace [A-Za-z_][A-Za-z0-9_]* \{' "$dir"/*Table.h 2>/dev/null \
    | sed -E 's/^namespace ([A-Za-z_][A-Za-z0-9_]*) \{.*/\1/' | head -n1)
[ -n "${ns:-}" ] || { echo "gen_roots: no namespace in $dir" >&2; exit 1; }

: > "$out"
for h in "$dir"/*Table.h; do
    [ -e "$h" ] || continue
    echo "#include \"$(basename "$h")\"" >> "$out"
done
echo "using namespace $ns;" >> "$out"

vars=$(grep -h -E '^inline int64_t [A-Za-z_][A-Za-z0-9_]*LoadMeasure\( const uint8_t \* wire_file' "$dir"/*Table.h 2>/dev/null \
    | sed -E 's/^inline int64_t ([A-Za-z_][A-Za-z0-9_]*)LoadMeasure.*/\1/' | sort -u)
fixeds=$(grep -h -E '^inline bool [A-Za-z_][A-Za-z0-9_]*Load\( [A-Za-z_][A-Za-z0-9_]* & value, const uint8_t \* buffer' "$dir"/*Table.h 2>/dev/null \
    | sed -E 's/^inline bool ([A-Za-z_][A-Za-z0-9_]*)Load\(.*/\1/' | sort -u)

if [ -z "$vars" ] && [ -z "$fixeds" ]; then
    echo "gen_roots: no readable root in $dir (no LoadMeasure or fixed Load)" >&2
    exit 1
fi

var_count=0
for r in $vars; do var_count=$((var_count + 1)); done
fixed_count=0
for r in $fixeds; do fixed_count=$((fixed_count + 1)); done
open_count=0

for r in $vars; do
    if grep -q -E "inline .* ${r}Open\(" "$dir"/*Table.h 2>/dev/null; then
        k=FUZZ_VAR_OPEN
        open_count=$((open_count + 1))
    else
        k=FUZZ_VAR
    fi
    printf 'static void fuzz_%s( const uint8_t * data, size_t size ) { %s(%s); }\n' "$r" "$k" "$r" >> "$out"
done
for r in $fixeds; do
    if grep -q -E "inline .* ${r}Open\(" "$dir"/*Table.h 2>/dev/null; then
        k=FUZZ_FIXED_OPEN
        open_count=$((open_count + 1))
    else
        k=FUZZ_FIXED
    fi
    printf 'static void fuzz_%s( const uint8_t * data, size_t size ) { %s(%s); }\n' "$r" "$k" "$r" >> "$out"
done

printf '#define FUZZ_VAR_ROOTS %d\n' "$var_count" >> "$out"
printf '#define FUZZ_FIXED_ROOTS %d\n' "$fixed_count" >> "$out"
printf '#define FUZZ_OPEN_ROOTS %d\n' "$open_count" >> "$out"
printf '#define FUZZ_ROOT_COUNT %d\n' "$((var_count + fixed_count))" >> "$out"

printf 'static void fuzz_all( const uint8_t * data, size_t size )\n{\n' >> "$out"
for r in $vars $fixeds; do
    printf '    fuzz_%s( data, size );\n' "$r" >> "$out"
done
printf '}\n' >> "$out"
