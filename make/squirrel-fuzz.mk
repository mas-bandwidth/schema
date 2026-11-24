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
# THE SPLIT IS REGISTERED. The roots the plain arm leaves absent are the exact
# set in test/conformance/wire-absent/squirrel-plain.tsv (test/conformance/harness/wirefuzz_absent.go,
# W-ROSTER), each with its reason: the variable roots the builder arm fuzzes,
# and the roots owed. The builder arm counts the seeds outside it. A root that
# drops out of the leg, or out of both arms, goes red instead of fuzzing less.
# The registered absent count this replaced (185) was stale: the plain arm
# left 227 absent on the corpus of 2026-11-24.
#
# THE OUTSIDE COUNT IS AN EXACT PIN (the grep below matches `, N seeds outside
# the arm,`): the corpus seeds of the roots the builder arm does not read, the
# fixed-class and message roots. It depends on the corpus alone, never on the
# driver. It was 219 at 1146f49490 (2026-11-21). It is 321 at main e5ef2e0604,
# measured on hetzner1 with this target. The 102 more are corpus seeds added
# since then to such roots, by root: tabledemo.ProfileConfig 33,
# shapes.FixedBounds 18, tblfixedevo.EvoRoot 15, tabledemo.CountedMin 6,
# tabledemo.MinArmFix, MinPlainFix, MoodField and Moods 3 each, tblsurf.Arrays,
# Narrow and Wide 1 each, backenddemo.LoginRequest@message 2,
# packetunions.ShapeMsg and ShapeMsg@message 2 each,
# tblvaluedefaults.FixedDefaults 2, and 1 each to backenddemo.StorePurchase,
# tabledemo.ArchiveConfig, tabledemo.KeyedConfig, tbla2.Root, tblk1.Root,
# tblk2.Root and tblscalars2.SimState.
# The #4954 vectors add one (the fixed root tblfixedevo.EvoRoot is outside the
# builder arm): 322 on the combined head, measured.
SQUIRREL_WIRE_FUZZ_OUTSIDE_SEEDS := 322

.PHONY: tables-squirrel-wire-fuzz
tables-squirrel-wire-fuzz: build/conformance-harness build/conformance-squirrel toolchain-squirrel
	@mkdir -p build/wire-fuzz
	./build/conformance-harness wire-fuzz --leg squirrel --driver "$(SQ) test/conformance/squirrel/wirefuzz.nut" --seed 1 --n 2000 --failed build/wire-fuzz/failed-squirrel.bin > build/wire-fuzz/squirrel-plain.log 2>&1; \
		status=$$?; cat build/wire-fuzz/squirrel-plain.log; test $$status -eq 0
	./build/conformance-harness wire-fuzz --leg squirrel --driver "$(SQ) test/conformance/squirrel/wirefuzz.nut builder" --builder --seed 1 --n 2000 --failed build/wire-fuzz/failed-squirrel-builder.bin > build/wire-fuzz/squirrel-builder.log 2>&1; \
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
