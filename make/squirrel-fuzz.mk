# make/squirrel-fuzz.mk — the SQUIRREL leg's wire-fuzz target (docs/PORT-SQUIRREL.md,
# THE CARD MAP; test/conformance/README.md, "The wire-fuzz driver";
# docs/SPEC-TABLES.md §4.2). The Makefile's wildcard include pulls this in.
#
# The harness owns the mutants and the oracle; the leg is
# test/conformance/squirrel/wirefuzz.nut, one $(SQ) process on a pipe over the
# units the target generates (build/conformance-squirrel, make/squirrel.mk).
#
# TWO ARMS, ONE DRIVER. The Squirrel load builds class instances and has no
# caller region, so it has no LoadMeasure. The plain arm holds a VARIABLE root
# to its region measure, so there the driver answers the FIXED-class roots,
# which always load and are never measured; the VARIABLE-class roots run under
# the harness's builder arm (--builder), which compares the builder's
# completion, the report and the saved bytes. Every mutant of every root the
# target generates is in exactly one of the two.

#
# THE SPLIT IS REGISTERED. Of the corpus's 364 seeds the plain arm answers
# 199 and leaves 185 absent; the builder arm answers the other 165 and counts
# 219 outside it (measured on the committed corpus; the counts do not depend
# on --n). A root that drops out of the leg, or out of both arms, moves one of
# these counts and the recipe goes red instead of fuzzing less.
SQUIRREL_WIRE_FUZZ_ABSENT_SEEDS  := 185
SQUIRREL_WIRE_FUZZ_OUTSIDE_SEEDS := 219

.PHONY: tables-squirrel-wire-fuzz
tables-squirrel-wire-fuzz: build/conformance-harness build/conformance-squirrel toolchain-squirrel
	@mkdir -p build/wire-fuzz
	./build/conformance-harness wire-fuzz --driver "$(SQ) test/conformance/squirrel/wirefuzz.nut" --seed 1 --n 2000 --failed build/wire-fuzz/failed-squirrel.bin > build/wire-fuzz/squirrel-plain.log 2>&1; \
		status=$$?; cat build/wire-fuzz/squirrel-plain.log; test $$status -eq 0
	@grep -qE ', $(SQUIRREL_WIRE_FUZZ_ABSENT_SEEDS) seeds absent \(' build/wire-fuzz/squirrel-plain.log || { \
		echo "tables-squirrel-wire-fuzz: the plain arm's absent seed count is not the $(SQUIRREL_WIRE_FUZZ_ABSENT_SEEDS) registered: $$(grep -oE '[0-9]+ seeds absent' build/wire-fuzz/squirrel-plain.log || echo none absent)"; exit 1; }
	./build/conformance-harness wire-fuzz --driver "$(SQ) test/conformance/squirrel/wirefuzz.nut builder" --builder --seed 1 --n 2000 --failed build/wire-fuzz/failed-squirrel-builder.bin > build/wire-fuzz/squirrel-builder.log 2>&1; \
		status=$$?; cat build/wire-fuzz/squirrel-builder.log; test $$status -eq 0
	@grep -qE ', $(SQUIRREL_WIRE_FUZZ_OUTSIDE_SEEDS) seeds outside the arm,' build/wire-fuzz/squirrel-builder.log || { \
		echo "tables-squirrel-wire-fuzz: the builder arm's outside seed count is not the $(SQUIRREL_WIRE_FUZZ_OUTSIDE_SEEDS) registered: $$(grep -oE '[0-9]+ seeds outside the arm' build/wire-fuzz/squirrel-builder.log || echo none outside)"; exit 1; }

.PHONY: tables-squirrel-release-negative-control
# THE RELEASE TIER'S CONTROL (test/conformance/squirrel/release-negative-control):
# the load overlaid so a stopped builder reports finished, the corpus
# regenerated, and the release tier's builder-arm fuzz run against it at seed 1
# and the PR count must go red on the builder's completion.
tables-squirrel-release-negative-control: build/conformance-harness toolchain-squirrel
	SQ="$(SQ)" sh test/conformance/squirrel/release-negative-control --seed 1 --n 2000
