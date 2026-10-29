# make/cpp.mk — the C++ leg of the card corpus (schema-cards). Included by the
# Makefile's wildcard include; the Makefile names no language. The card targets
# live here, not in the root Makefile (docs/CONTRIBUTING.md, "Adding a
# language").

CARDS_CPP_GENERATED := build/tables-generated-cpp/cards

.PHONY: cards-cpp-generate
cards-cpp-generate: bin/schema
	@rm -rf $(CARDS_CPP_GENERATED)
	@mkdir -p $(CARDS_CPP_GENERATED)
	./bin/schema generate --lang cpp --out $(CARDS_CPP_GENERATED) tables/cards

CARDS_CPP_SRCS := tables/cards/cpp/cards.cpp tables/cards/cpp/sha256.cpp \
	$(CARDS_CPP_GENERATED)/CardTable.cpp $(CARDS_CPP_GENERATED)/CardsView.cpp

build/cards-cpp-test: cards-cpp-generate tables/cards/cpp/cards.cpp tables/cards/cpp/sha256.cpp tables/cards/cpp/cards_test.cpp make/cpp.mk
	@mkdir -p build
	$(CXX) $(TABLES_CXXFLAGS) -I$(CARDS_CPP_GENERATED) \
		tables/cards/cpp/cards_test.cpp $(CARDS_CPP_SRCS) -o $@

build/cards-cpp-test-asan: cards-cpp-generate tables/cards/cpp/cards.cpp tables/cards/cpp/sha256.cpp tables/cards/cpp/cards_test.cpp make/cpp.mk
	@mkdir -p build
	$(CXX) $(TABLES_CXXFLAGS) -fsanitize=address,undefined -fno-sanitize-recover=all \
		-fno-omit-frame-pointer -g -I$(CARDS_CPP_GENERATED) \
		tables/cards/cpp/cards_test.cpp $(CARDS_CPP_SRCS) -o $@

.PHONY: cards-cpp-test
cards-cpp-test: build/cards-cpp-test build/cards-cpp-test-asan
	./build/cards-cpp-test
	./build/cards-cpp-test-asan

build/cards-cpp-driver: cards-cpp-generate tables/cards/cpp/cards.cpp tables/cards/cpp/sha256.cpp tables/cards/cpp/driver.cpp make/cpp.mk
	@mkdir -p build
	$(CXX) $(TABLES_CXXFLAGS) -I$(CARDS_CPP_GENERATED) \
		tables/cards/cpp/driver.cpp $(CARDS_CPP_SRCS) -o $@

CARDS_CPP_SURFACES := load,save,render,parse,bounds,upgrade,edit,taskhash,view

.PHONY: cards-cpp-conformance
cards-cpp-conformance: build/conformance-harness build/cards-cpp-driver
	./build/conformance-harness cards --corpus tables/cards/conformance --driver tables/cards/cpp/driver --require $(CARDS_CPP_SURFACES)

.PHONY: cards-cpp-conformance-negative-control
cards-cpp-conformance-negative-control: build/conformance-harness build/cards-cpp-driver
	@mkdir -p build
	@if CARDS_CPP_RENDER_FLIP=1 ./build/conformance-harness cards --corpus tables/cards/conformance --driver tables/cards/cpp/driver --require $(CARDS_CPP_SURFACES) > build/cards-cpp-conformance-neg.log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: the card check stayed green with one rendered byte changed'; exit 1; \
	fi
	@grep -qE 'cards render .*FAIL' build/cards-cpp-conformance-neg.log || { echo 'NEGATIVE CONTROL FAILED: render did not go red'; cat build/cards-cpp-conformance-neg.log; exit 1; }
	@for s in load save parse bounds upgrade edit taskhash view; do \
		grep -qE "cards $$s pass" build/cards-cpp-conformance-neg.log || { echo "NEGATIVE CONTROL FAILED: $$s did not stay green"; cat build/cards-cpp-conformance-neg.log; exit 1; }; \
	done
	@echo 'cards-cpp negative control: one changed render byte turns render red; the other eight surfaces stay green'

.PHONY: cards-cpp-conformance-upgrade-negative-control
cards-cpp-conformance-upgrade-negative-control: build/conformance-harness build/cards-cpp-driver
	@mkdir -p build
	@if CARDS_CPP_UPGRADE_DROP=1 ./build/conformance-harness cards --corpus tables/cards/conformance --driver tables/cards/cpp/driver --require $(CARDS_CPP_SURFACES) > build/cards-cpp-conformance-upgrade-neg.log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: the card check stayed green when one retained field was dropped'; exit 1; \
	fi
	@grep -qE 'cards upgrade .*FAIL' build/cards-cpp-conformance-upgrade-neg.log || { echo 'NEGATIVE CONTROL FAILED: upgrade did not go red'; cat build/cards-cpp-conformance-upgrade-neg.log; exit 1; }
	@for s in load save render parse bounds edit taskhash view; do \
		grep -qE "cards $$s pass" build/cards-cpp-conformance-upgrade-neg.log || { echo "NEGATIVE CONTROL FAILED: $$s did not stay green"; cat build/cards-cpp-conformance-upgrade-neg.log; exit 1; }; \
	done
	@echo 'cards-cpp upgrade negative control: dropping one retained field turns upgrade red; the other eight surfaces stay green'

.PHONY: cards-cpp-conformance-hash-negative-control
cards-cpp-conformance-hash-negative-control: build/conformance-harness build/cards-cpp-driver
	@mkdir -p build
	@if CARDS_CPP_HASH_NEEDS=1 ./build/conformance-harness cards --corpus tables/cards/conformance --driver tables/cards/cpp/driver --require $(CARDS_CPP_SURFACES) > build/cards-cpp-conformance-hash-neg.log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: the card check stayed green when needs was hashed'; exit 1; \
	fi
	@grep -qE 'cards taskhash .*FAIL' build/cards-cpp-conformance-hash-neg.log || { echo 'NEGATIVE CONTROL FAILED: taskhash did not go red'; cat build/cards-cpp-conformance-hash-neg.log; exit 1; }
	@for s in load save render parse bounds upgrade edit view; do \
		grep -qE "cards $$s pass" build/cards-cpp-conformance-hash-neg.log || { echo "NEGATIVE CONTROL FAILED: $$s did not stay green"; cat build/cards-cpp-conformance-hash-neg.log; exit 1; }; \
	done
	@echo 'cards-cpp hash negative control: hashing needs turns taskhash red; the other eight surfaces stay green'

test: cards-cpp-test cards-cpp-conformance cards-cpp-conformance-negative-control cards-cpp-conformance-upgrade-negative-control cards-cpp-conformance-hash-negative-control

# THE C++ TABLES SOAK (docs/PORTING.md I9, issue #416's C++ half). The one-shot
# tables-cpp-alloc counts operator new across ONE pass on the richest root; a
# path that retains a little per call, or allocates where that pass does not
# go, is invisible to it. test/tables/soak_main.cpp loops the C soak's corpus
# (every pinned root, exact and evolution seams) through Load, the indexed and
# iterated reads, Measure, Save and the text form for
# TABLES_CPP_SOAK_ITERATIONS passes (a count, not a clock), reads the
# allocation COUNT at every sample and fails the moment it moves after the
# warmup pass. The count is operator new's, plus the C
# allocator's own entry points on glibc, where the generated schema_allocate
# (calloc) would otherwise go unseen. Live-heap drift is reported and gated
# beside it as the weaker instrument. It is a FUNCTIONAL test: the release
# act is the Makefile's default count (about an hour), and `make test` runs
# 4096 passes of it.
TABLES_CPP_SOAK_SRCS = test/tables/soak_main.cpp \
	build/tables-generated/examples/TablesTable.cpp build/tables-generated/examples/KeyedTable.cpp \
	build/tables-generated/examples/NestedTable.cpp build/tables-generated/examples/WideTable.cpp \
	build/tables-generated/v1/V1Table.cpp build/tables-generated/v2/V2Table.cpp \
	build/tables-generated/p1/P1Table.cpp build/tables-generated/p3/P3Table.cpp

build/schema_test_tables_soak: build/tables-generated/.stamp test/tables/soak_main.cpp make/cpp.mk
	@mkdir -p build
	$(CXX) $(TABLES_CXXFLAGS) -O2 $(TABLES_INCLUDES) $(TABLES_CPP_SOAK_SRCS) -o $@

.PHONY: tables-cpp-soak
tables-cpp-soak: build/schema_test_tables_soak
	./build/schema_test_tables_soak $(TABLES_CPP_SOAK_ITERATIONS)

# THE SOAK'S NEGATIVE CONTROL. The live-heap sample is a LEAK instrument: a
# matched new/delete pair inside the loop returns it to where it was before it
# is ever read, so a soak gated on drift alone stays green over an allocation
# per iteration. -DSCHEMA_SOAK_PLANT_PAIR builds the same file with one such
# pair planted per iteration (a compile-time define: the generated code is not
# touched); the soak must exit non-zero ON THE COUNT while the drift line stays
# flat. A soak that goes red for any other reason fails this control.
.PHONY: tables-cpp-soak-negative-control
tables-cpp-soak-negative-control: build/tables-generated/.stamp test/tables/soak_main.cpp
	@rm -rf build/cpp-soak-sabotage && mkdir -p build/cpp-soak-sabotage
	$(CXX) $(TABLES_CXXFLAGS) -O2 -DSCHEMA_SOAK_PLANT_PAIR $(TABLES_INCLUDES) $(TABLES_CPP_SOAK_SRCS) \
		-o build/cpp-soak-sabotage/soak
	@if ./build/cpp-soak-sabotage/soak 4096 > build/cpp-soak-sabotage/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a new/delete pair per iteration left the C++ soak green"; \
		cat build/cpp-soak-sabotage/log; exit 1; \
	fi
	@grep -q "PLANTED" build/cpp-soak-sabotage/log || \
		{ echo "NEGATIVE CONTROL FAILED: the build did not plant the pair"; cat build/cpp-soak-sabotage/log; exit 1; }
	@grep -q "^SOAK FAILED: the allocation count moved" build/cpp-soak-sabotage/log || \
		{ echo "NEGATIVE CONTROL FAILED: the soak went red, but not on the allocation count"; \
		  cat build/cpp-soak-sabotage/log; exit 1; }
	@grep -q "drift flat" build/cpp-soak-sabotage/log || \
		{ echo "NEGATIVE CONTROL FAILED: the drift line did not stay flat (or did not run)"; \
		  cat build/cpp-soak-sabotage/log; exit 1; }
	@if grep -q "^SOAK FAILED: live heap" build/cpp-soak-sabotage/log; then \
		echo "NEGATIVE CONTROL FAILED: the drift gate refused a matched pair, so the count was not what turned it red"; \
		cat build/cpp-soak-sabotage/log; exit 1; \
	fi
	@grep -m1 "^SOAK FAILED" build/cpp-soak-sabotage/log
	@grep -m1 "drift flat" build/cpp-soak-sabotage/log
	@echo "negative control: a matched new/delete pair per iteration leaves the drift line flat and turns the C++ soak's ALLOCATION COUNT red"


# J5 (docs/PORTING.md) — THE BENCH LEG'S GOLDEN GATE RUNS BEFORE THE CLOCK.
# The C++ table leg's `--gate` verb (bench/tables/cpp/table_main.cpp) runs
# every correctness gate it runs before a timed pass — variant 0 against
# testdata/wire/bench_table.bin, all 64 variants loaded, re-saved and compared
# byte for byte, reused storage — and returns before the clock starts. This
# builds the leg the way the bench builds it (bench/tables/cpp/leg, -O3
# -DNDEBUG) and runs ONLY the gate: a correctness check, no timing.
.PHONY: tables-cpp-bench-gate
tables-cpp-bench-gate: generated/bench/tables/cpp/.stamp
	bench/tables/cpp/leg build
	bench/tables/cpp/leg run --gate
test: tables-cpp-bench-gate

# ITS NEGATIVE CONTROL. A gate that has never been red proves nothing, so this
# feeds the built leg's --gate a golden with ONE byte changed, and then the
# true golden with one byte changed in variant 0 of the variant corpus, and
# requires each to be refused with the gate's own words before any timing.
# The same leg over the true files must pass first, so a red here is the
# corruption and not the harness.
.PHONY: tables-cpp-bench-gate-negative-control
tables-cpp-bench-gate-negative-control: generated/bench/tables/cpp/.stamp
	bench/tables/cpp/leg build
	@rm -rf build/cpp-bench-gate-sabotage && mkdir -p build/cpp-bench-gate-sabotage/wire build/cpp-bench-gate-sabotage/variants
	./build/schema_tables_bench_cpp --gate > build/cpp-bench-gate-sabotage/clean.log 2>&1 || \
		{ echo "NEGATIVE CONTROL FAILED: the gate is red over the true corpus"; cat build/cpp-bench-gate-sabotage/clean.log; exit 1; }
	@cp testdata/wire/bench_table.bin build/cpp-bench-gate-sabotage/wire/bench_table.bin
	@cp bench/corpus/variants/bench_table.variants.bin build/cpp-bench-gate-sabotage/variants/bench_table.variants.bin
	@b=$$(od -An -tu1 -j1000 -N1 build/cpp-bench-gate-sabotage/wire/bench_table.bin | tr -d ' '); \
		printf "$$(printf '\\%03o' $$(( (b + 1) % 256 )))" | dd of=build/cpp-bench-gate-sabotage/wire/bench_table.bin bs=1 seek=1000 conv=notrunc 2>/dev/null; \
		cmp -s testdata/wire/bench_table.bin build/cpp-bench-gate-sabotage/wire/bench_table.bin && \
		{ echo "NEGATIVE CONTROL FAILED: the golden was not changed"; exit 1; } || true
	@if ./build/schema_tables_bench_cpp --gate --wire-dir build/cpp-bench-gate-sabotage/wire > build/cpp-bench-gate-sabotage/golden.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a golden one byte off left the bench gate green"; \
		cat build/cpp-bench-gate-sabotage/golden.log; exit 1; \
	fi
	@grep -q "WIRE GOLDEN MISMATCH" build/cpp-bench-gate-sabotage/golden.log || \
		{ echo "NEGATIVE CONTROL FAILED: the gate went red, but not on the golden"; cat build/cpp-bench-gate-sabotage/golden.log; exit 1; }
	@b=$$(od -An -tu1 -j1000 -N1 build/cpp-bench-gate-sabotage/variants/bench_table.variants.bin | tr -d ' '); \
		printf "$$(printf '\\%03o' $$(( (b + 1) % 256 )))" | dd of=build/cpp-bench-gate-sabotage/variants/bench_table.variants.bin bs=1 seek=1000 conv=notrunc 2>/dev/null
	@if ./build/schema_tables_bench_cpp --gate --variant-dir build/cpp-bench-gate-sabotage/variants > build/cpp-bench-gate-sabotage/variant.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a variant one byte off left the bench gate green"; \
		cat build/cpp-bench-gate-sabotage/variant.log; exit 1; \
	fi
	@grep -q "WIRE GOLDEN MISMATCH" build/cpp-bench-gate-sabotage/variant.log || \
		{ echo "NEGATIVE CONTROL FAILED: the gate went red, but not on the golden"; cat build/cpp-bench-gate-sabotage/variant.log; exit 1; }
	@grep -m1 "WIRE GOLDEN MISMATCH" build/cpp-bench-gate-sabotage/golden.log
	@echo "negative control: one changed byte in the golden, or in variant 0, turns the C++ bench gate red before any clock"
