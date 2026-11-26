#!/bin/sh
# make/goose-cc-lto.sh — the C compiler `goose build` runs for a fuzz leg
# (make/goose-wirefuzz-build.sh, `goose build --cc`): the machine's cc with
# -flto=auto added. goose writes one C file per unit, and the largest units'
# files (tabledemo, vocab9demo: 6 to 7 MB of C) took 36 to 43 s each in one
# cc -O2 process on bench vision, the whole critical path of a per-commit fuzz
# part. With gcc's -flto=auto the optimizer's work is cut into partitions that
# compile in parallel at link time (tabledemo 20 s).
#
# WHAT THIS CHANGES IN THE BINARY UNDER TEST: the C sources and every flag goose
# passes (-O2 among them) are the same, but link-time optimization also
# optimizes ACROSS the leg's C files (the unit and wirefuzz_io.c), so the fuzzed
# leg is an LTO build, where a user's default `goose build` is not. A fault
# that shows only without cross-file optimization would not show in a part;
# so only the per-commit slices use this (GOOSE_LEG_LTO, set under
# GOOSE_FUZZ_SLICE), and the nightly whole gate (leg-gates-nightly.yml, `make
# verify-goose-fuzz`, unsliced) and the negative controls still build plain
# -O2, as before. With clang as cc, -flto=auto is full LTO, which does not run in parallel:
# no speedup there.
#
# GOOSE_LEG_CC names the compiler (default cc). The script fails loudly: set -e,
# and exec hands the make cc's own exit status, so a compile that fails fails
# goose build, make/goose-wirefuzz-build.sh and the make that runs it
# (TestTheGooseLTOCompilerFailsLoudly, internal/ci).
set -eu
exec "${GOOSE_LEG_CC:-cc}" "$@" -flto=auto
