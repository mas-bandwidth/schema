#!/bin/sh
# One schema of a nightly leg: compile the generated reader with the leg's
# sanitizer flags, then fuzz it for the per-schema slice of the budget.
# The Makefile exports the build paths and flags; this file is the xargs
# worker that lets the 49-schema corpus build and fuzz on more than one core.
set -e

tag=$1
schema=$2
per=$3

slug=$(printf '%s' "$schema" | tr '/.' '--')
dir=${CPP_FUZZ_BUILD}/generated/$slug
bin=${CPP_FUZZ_BUILD}/$tag/$slug

mkdir -p "$(dirname "$bin")" "${CPP_FUZZ_BUILD}/work/$slug" "${CPP_FUZZ_CORPUS}/$slug"

${CPP_FUZZ_CXX} ${CPP_FUZZ_BASE_FLAGS} ${CPP_FUZZ_FLAGS} -I"$dir" \
	-DFUZZ_ROOTS_INC='"roots.inc"' \
	test/cpp-fuzz/nightly_fuzz_main.cpp "$dir"/*Table.cpp -o "$bin"

"$bin" -max_total_time="$per" -timeout=25 -rss_limit_mb=2048 \
	-artifact_prefix="${CPP_FUZZ_CORPUS}/$slug/" \
	"${CPP_FUZZ_BUILD}/work/$slug" "${CPP_FUZZ_CORPUS}/$slug" \
	"${CPP_FUZZ_SEED_DIR}/wire" "${CPP_FUZZ_SEED_DIR}/text" \
	"${CPP_FUZZ_SEED_DIR}/cook" "${CPP_FUZZ_SEED_DIR}/go"
