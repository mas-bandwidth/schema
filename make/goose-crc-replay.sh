#!/bin/sh
# make/goose-crc-replay.sh — one featurewitnesses leg with its generated
# crc32c edited, replayed against the short-body integrity vector
# (make/goose-fuzz.mk, the integrity controls and diagnostic).
#
#   goose-crc-replay.sh <goose> <schema> <harness> <work dir> <sed script>
#
# Exits 0 when the replay is green, 3 when the driver stalled (the harness's
# watchdog), 1 on any other red, 2 when the sed patched nothing or the leg
# does not build. The harness's output is <work dir>/log.
set -u
goose=$1 schema=$2 harness=$3 work=$4 script=$5
rm -rf "$work" && mkdir -p "$work" || exit 2
"$schema" generate --lang goose --out "$work/unit" testdata/conformance/feature-witnesses/full/Features.schema >/dev/null || exit 2
sed "$script" "$work/unit/schema_tables.goose" >"$work/schema_tables.goose" || exit 2
if ! grep -q SABOTAGED "$work/schema_tables.goose"; then
	echo "goose-crc-replay: the edit patched nothing" >&2
	exit 2
fi
mv "$work/schema_tables.goose" "$work/unit/schema_tables.goose"
sh make/goose-wirefuzz-build.sh "$goose" "$work/unit" "$work/leg" || exit 2
if "$harness" wire-fuzz --driver "$work/leg/wirefuzz featurewitnesses builder" \
	--replay testdata/wire/tables/fuzz-vectors/integrity_short_body_mismatch.bin \
	--unit featurewitnesses --root Root --builder --retain >"$work/log" 2>&1; then
	exit 0
fi
grep -q 'the driver stalled' "$work/log" && exit 3
exit 1
