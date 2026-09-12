# make/c.mk — the C leg (docs/CONTRIBUTING.md, "Adding a language"). Included
# by the Makefile's wildcard include; the Makefile names no language. The leg
# registers itself at the end of this file.

# the serialize.c runtime the generated C targets, a sibling checkout
SERIALIZE_C ?= ../serialize.c

build/packet-text/c/.stamp: bin/schema test/packet-text/Narrow.schema
	./bin/schema generate --lang c --out build/packet-text/c test/packet-text/Narrow.schema
	@touch $@

.PHONY: packet-utf8-c packet-utf8-c-negative-control
packet-utf8-c: build/packet-text/c/.stamp build/packet-text/cpp/driver build/packet-text/harness
	$(CC) -std=c99 -Wall -Wextra -Werror -I$(SERIALIZE_C) -Ibuild/packet-text/c test/packet-text/driver.c $(SERIALIZE_C)/serialize.c -lm -o build/packet-text/c/debug
	./build/packet-text/harness ./build/packet-text/c/debug
	$(CC) -std=c99 -Wall -Wextra -Werror -O2 -DNDEBUG -I$(SERIALIZE_C) -Ibuild/packet-text/c test/packet-text/driver.c $(SERIALIZE_C)/serialize.c -lm -o build/packet-text/c/release
	./build/packet-text/harness ./build/packet-text/c/release

packet-utf8-c-negative-control: packet-utf8-c
	@mkdir -p build/packet-text/c-negative
	go run ./tools/sabotage -name packet-utf8-c-read -out build/packet-text/c-negative/fields.gotext internal/codegen/c/fields.go
	@printf '{"Replace":{"%s/internal/codegen/c/fields.go":"%s/build/packet-text/c-negative/fields.gotext"}}\n' "$(CURDIR)" "$(CURDIR)" > build/packet-text/c-negative/overlay.json
	go run -overlay=build/packet-text/c-negative/overlay.json ./cmd/schema generate --lang c --out build/packet-text/c-negative test/packet-text/Narrow.schema
	$(CC) -std=c99 -Wall -Wextra -Werror -O2 -DNDEBUG -I$(SERIALIZE_C) -Ibuild/packet-text/c-negative test/packet-text/driver.c $(SERIALIZE_C)/serialize.c -lm -o build/packet-text/c-negative/driver
	@if ./build/packet-text/harness -mutations-only ./build/packet-text/c-negative/driver > build/packet-text/c-negative/log 2>&1; then echo 'NEGATIVE CONTROL FAILED: C UTF-8 removal passed'; exit 1; fi
	@grep -Fq 'FAILED: packet-text verdict on ' build/packet-text/c-negative/log || { cat build/packet-text/c-negative/log; exit 1; }
	@echo 'packet UTF-8 C negative control: removed read validation fails bit-flip agreement'

test-c: packet-utf8-c packet-utf8-c-negative-control

generated/c/.stamp: bin/schema $(SCHEMAS)
	./bin/schema generate --lang c --out generated/c examples
	@touch $@

# -Wtype-limits is where gcc reports a vacuous comparison and clang stays quiet,
# so it rides unconditionally. clang says the same thing under a flag gcc does
# not recognise, so that one is FEATURE TESTED rather than assumed -- hardcoding
# it broke the Linux leg once already, which is the argument for testing rather
# than guessing which compiler is which.
C_TAUTOLOGICAL := $(shell $(CC) -Wtautological-type-limit-compare -E - < /dev/null > /dev/null 2>&1 && echo -Wtautological-type-limit-compare)

# The C corpus test. -Werror on purpose: generated headers are included by the
# CONSUMER's translation units, so a warning here is a build failure in their
# tree, not ours.
build/schema_test_c: generated/c/.stamp test/c/main.c
	@mkdir -p build
	$(CC) -std=c99 -Wall -Wextra -Werror -Wtype-limits $(C_TAUTOLOGICAL) \
		-O2 -ffp-contract=off -Igenerated/c -I$(SERIALIZE_C) \
		test/c/main.c $(SERIALIZE_C)/serialize.c -o $@ -lm

generated/c-ludicrous/.stamp: bin/schema $(SCHEMAS128)
	./bin/schema generate --lang c --out generated/c-ludicrous examples128
	@touch $@

generated/bench/c/.stamp: bin/schema $(SCHEMAS_BENCH)
	./bin/schema generate --lang c --out generated/bench/c bench/corpus/Bench.schema
	./bin/schema generate --lang c --out generated/bench/c bench/corpus/RealWorld.schema
	@touch $@

generated/bench/tables/c/.stamp: bin/schema bench/corpus/BenchTable.schema
	@mkdir -p generated/bench/tables/c
	./bin/schema generate --lang c --out generated/bench/tables/c bench/corpus/BenchTable.schema
	@touch $@

# Explicit generated/ stamp: CI discovers it without a hand-maintained list.
generated/bench/paired/c/.stamp: bin/schema bench/corpus/Bench.schema bench/corpus/FixedTable.schema
	@mkdir -p generated/bench/paired/c
	./bin/schema generate --lang c --out generated/bench/paired/c bench/corpus/Bench.schema bench/corpus/FixedTable.schema
	@touch $@

# Compile the shared table harness against the paired closure. This catches
# storage/layout drift in this unit without building another language or timing.
build/schema_test_bench_paired_c: generated/bench/paired/c/.stamp bench/tables/c/table_main.c
	@mkdir -p build
	$(CC) -std=c99 -Wall -Wextra -Werror -O2 -DNDEBUG -ffp-contract=off \
		-DBENCH_MATCHED -Igenerated/bench/paired/c -I$(SERIALIZE_C) \
		bench/tables/c/table_main.c -o $@ -lm

test-c: build/schema_test_bench_paired_c

build/schema_test_bench_c: generated/bench/c/.stamp test/bench/c_main.c
	@mkdir -p build
	$(CC) -std=c99 -Wall -Wextra -Werror -Wtype-limits $(C_TAUTOLOGICAL) \
		-O2 -ffp-contract=off -Igenerated/bench/c -I$(SERIALIZE_C) \
		test/bench/c_main.c $(SERIALIZE_C)/serialize.c -o $@ -lm

# The C half of the fixed-point and 128-bit corpus. Its ABSENCE is why a C
# codec that wrote nothing for every fixed field passed every gate: the C
# target was generated from examples/ only, and examples/ has no `fixed(`.
build/schema_test_c_ludicrous: generated/c-ludicrous/.stamp test/c-ludicrous/main.c
	@mkdir -p build
	$(CC) -std=c99 -Wall -Wextra -Werror -Wtype-limits $(C_TAUTOLOGICAL) \
		-O2 -ffp-contract=off -Igenerated/c-ludicrous -I$(SERIALIZE_C) \
		test/c-ludicrous/main.c $(SERIALIZE_C)/serialize.c -o $@ -lm

# THE C TABLES LEG (docs/SPEC-TABLES.md, test/conformance/README.md) ----------
#
# The same corpus the C++ leg generates, in C. It goes into its own build
# directory rather than beside the C++ output because both emitters write
# <Base>Table.h — one name, two languages — and a shared directory would have
# them overwrite each other.
#
# UNIT PER DIRECTORY IS LOAD-BEARING HERE and not a convention: C has no
# namespaces, so tblv1's Cfg and tblv2's Cfg are one struct name, and every
# unit's Table header defines the same TableReport. Two units can be LINKED
# together — the generated externals carry the package (internal/codegen/ctable's
# `sym`) — but they cannot be INCLUDED into one translation unit, which is what
# the conformance driver's file-per-unit shape is about.
build/tables-generated-c/.stamp: bin/schema build/tables-generated-c/collections.stamp make/c.mk test/tables/W1.schema test/tables/W2.schema test/tables/G1.schema $(wildcard tables/stream/*.schema) $(wildcard tables/blobs/*.schema) $(wildcard tables/vocab9/*.schema) $(wildcard tables/vocab/*.schema) $(wildcard tables/backend/*.schema) test/tables/R2.schema test/tables/R1.schema test/tables/K2.schema test/tables/K1.schema test/tables/A2.schema test/tables/A1.schema test/tables/M2.schema test/tables/M1.schema $(wildcard tables/messages/*.schema) $(SCHEMAS_TABLES) $(SCHEMAS_TABLES_POINTERS) $(SCHEMAS_TABLES_BLOCK) test/tables/V1.schema test/tables/V2.schema test/tables/P1.schema test/tables/P2.schema test/tables/P3.schema test/tables/JsonKeys.schema tables/scalars/Scalars.schema test/tables/Scalars2.schema examples-wide/Caption.schema examples-wide/WideText.schema
	@mkdir -p build/tables-generated-c
	./bin/schema generate --lang c --out build/tables-generated-c/w1 test/tables/W1.schema
	./bin/schema generate --lang c --out build/tables-generated-c/w2 test/tables/W2.schema
	./bin/schema generate --lang c --out build/tables-generated-c/g1 test/tables/G1.schema
	./bin/schema generate --lang c --out build/tables-generated-c/stream tables/stream
	./bin/schema generate --lang c --out build/tables-generated-c/blobs tables/blobs
	./bin/schema generate --lang c --out build/tables-generated-c/vocab9 tables/vocab9
	./bin/schema generate --lang c --out build/tables-generated-c/vocab tables/vocab
	./bin/schema generate --lang c --out build/tables-generated-c/backend tables/backend
	./bin/schema generate --lang c --out build/tables-generated-c/r2 test/tables/R2.schema
	./bin/schema generate --lang c --out build/tables-generated-c/r1 test/tables/R1.schema
	./bin/schema generate --lang c --out build/tables-generated-c/rt1 test/tables/RT1.schema
	./bin/schema generate --lang c --out build/tables-generated-c/k2 test/tables/K2.schema
	./bin/schema generate --lang c --out build/tables-generated-c/k1 test/tables/K1.schema
	./bin/schema generate --lang c --out build/tables-generated-c/a2 test/tables/A2.schema
	./bin/schema generate --lang c --out build/tables-generated-c/a1 test/tables/A1.schema
	./bin/schema generate --lang c --out build/tables-generated-c/m2 test/tables/M2.schema
	./bin/schema generate --lang c --out build/tables-generated-c/m1 test/tables/M1.schema
	./bin/schema generate --lang c --out build/tables-generated-c/messages tables/messages
	./bin/schema generate --lang c --out build/tables-generated-c/examples tables/examples
	./bin/schema generate --lang c --out build/tables-generated-c/pointers tables/pointers
	./bin/schema generate --lang c --out build/tables-generated-c/block tables/block
	./bin/schema generate --lang c --out build/tables-generated-c/blockhome tables/blockhome
	./bin/schema generate --lang c --out build/tables-generated-c/v1 test/tables/V1.schema
	./bin/schema generate --lang c --out build/tables-generated-c/v2 test/tables/V2.schema
	./bin/schema generate --lang c --out build/tables-generated-c/p1 test/tables/P1.schema
	./bin/schema generate --lang c --out build/tables-generated-c/p2 test/tables/P2.schema
	./bin/schema generate --lang c --out build/tables-generated-c/p3 test/tables/P3.schema
	./bin/schema generate --lang c --out build/tables-generated-c/wide examples-wide
	./bin/schema generate --lang c --out build/tables-generated-c/scalars tables/scalars
	./bin/schema generate --lang c --out build/tables-generated-c/scalars2 test/tables/Scalars2.schema
	./bin/schema generate --lang c --out build/tables-generated-c/jsonkeys test/tables/JsonKeys.schema
	@touch $@

# -Werror on purpose: generated headers are included by the CONSUMER's
# translation units, so a warning here is a build failure in their tree, not
# ours. The flags are the type wire's C leg's, clause for clause.
TABLES_CFLAGS := -std=c99 -Wall -Wextra -Werror -Wshadow -Wtype-limits $(C_TAUTOLOGICAL) \
	-O2 -ffp-contract=off

# The flags a NEGATIVE CONTROL builds with. A control is built to be RUN ONCE
# and thrown away — it proves a gate can go red and is never measured — so it
# pays the warnings and skips the optimiser. On the driver's twenty-eight
# translation units that is most of the build, and `make test` runs two of
# these.
TABLES_CFLAGS_CONTROL := $(subst -O2,-O0,$(TABLES_CFLAGS))

C_CONFORMANCE_SOURCES = test/c-tables/collections_fuzz_maps.c test/c-tables/collections_fuzz_lists.c test/c-tables/collections_fuzz_arms.c \
	$(patsubst tables/%.schema,build/tables-generated-c/%Table.c,$(SCHEMAS_TABLES_MAPS) $(SCHEMAS_TABLES_LISTS) $(SCHEMAS_TABLES_ARMS)) \
	test/conformance/c/unit_tblw1.c build/tables-generated-c/w1/W1Table.c \
	test/conformance/c/unit_tblw2.c build/tables-generated-c/w2/W2Table.c \
	test/conformance/c/main.c test/conformance/c/retain.c build/tables-generated-c/rt1/RT1Table.c \
	test/conformance/c/unit_tblg1.c build/tables-generated-c/g1/G1Table.c \
	test/conformance/c/unit_tblp2.c build/tables-generated-c/p2/P2Table.c \
	test/conformance/c/unit_streamdemo.c build/tables-generated-c/stream/StreamTable.c \
	test/conformance/c/unit_blobdemo.c build/tables-generated-c/blobs/AssetsTable.c \
	test/conformance/c/unit_vocab9demo.c build/tables-generated-c/vocab9/Vocab9Table.c \
	test/conformance/c/unit_vocabdemo.c build/tables-generated-c/vocab/VocabTable.c \
	test/conformance/c/unit_backenddemo.c build/tables-generated-c/backend/BackendTable.c \
	test/conformance/c/unit_tblr2.c build/tables-generated-c/r2/R2Table.c \
	test/conformance/c/unit_tblr1.c build/tables-generated-c/r1/R1Table.c \
	test/conformance/c/unit_tblk2.c build/tables-generated-c/k2/K2Table.c \
	test/conformance/c/unit_tblk1.c build/tables-generated-c/k1/K1Table.c \
	test/conformance/c/unit_tbla2.c build/tables-generated-c/a2/A2Table.c \
	test/conformance/c/unit_tbla1.c build/tables-generated-c/a1/A1Table.c \
	test/conformance/c/unit_tblm2.c build/tables-generated-c/m2/M2Table.c \
	test/conformance/c/unit_tblm1.c build/tables-generated-c/m1/M1Table.c \
	test/conformance/c/unit_messagedemo.c build/tables-generated-c/messages/MessagesTable.c \
	test/conformance/c/unit_widedemo.c build/tables-generated-c/wide/CaptionTable.c \
	test/conformance/c/unit_scalars.c test/conformance/c/unit_tblscalars2.c \
	build/tables-generated-c/scalars/ScalarsTable.c build/tables-generated-c/scalars2/Scalars2Table.c \
	test/conformance/c/unit_tabledemo.c test/conformance/c/unit_tblv1.c \
	test/conformance/c/unit_tblv2.c test/conformance/c/unit_tblp1.c \
	test/conformance/c/unit_tblp3.c test/conformance/c/unit_blockdemo.c \
	test/conformance/c/unit_graphdemo.c \
	build/tables-generated-c/examples/TablesTable.c build/tables-generated-c/examples/WideTable.c \
	build/tables-generated-c/examples/NestedTable.c build/tables-generated-c/examples/KeyedTable.c \
	build/tables-generated-c/examples/PackTable.c build/tables-generated-c/examples/GuardedTable.c \
	build/tables-generated-c/examples/RangesTable.c \
	build/tables-generated-c/v1/V1Table.c build/tables-generated-c/v2/V2Table.c \
	build/tables-generated-c/p1/P1Table.c build/tables-generated-c/p3/P3Table.c \
	build/tables-generated-c/block/RenderBlock.c build/tables-generated-c/block/RenderTable.c \
	build/tables-generated-c/block/PaddedBlock.c build/tables-generated-c/block/PaddedTable.c \
	build/tables-generated-c/pointers/GraphTable.c build/tables-generated-c/pointers/MarksTable.c \
	build/tables-generated-c/pointers/PartsTable.c

# Each unit's translation unit gets ONLY its own unit on the include path, which
# is what keeps two units' identically-named headers from meeting. The driver's
# own headers come from test/conformance/c.
C_CONFORMANCE_INCLUDES := -Ibuild/tables-generated-c -Ibuild/tables-generated-c/w1 -Ibuild/tables-generated-c/w2 -Ibuild/tables-generated-c/rt1 -Ibuild/tables-generated-c/g1 -Ibuild/tables-generated-c/p2 -Ibuild/tables-generated-c/stream -Ibuild/tables-generated-c/blobs -Ibuild/tables-generated-c/vocab9 -Ibuild/tables-generated-c/vocab -Ibuild/tables-generated-c/backend -Ibuild/tables-generated-c/r2 -Ibuild/tables-generated-c/r1 -Ibuild/tables-generated-c/k2 -Ibuild/tables-generated-c/k1 -Ibuild/tables-generated-c/a2 -Ibuild/tables-generated-c/a1 -Ibuild/tables-generated-c/m2 -Ibuild/tables-generated-c/m1 -Ibuild/tables-generated-c/messages -Ibuild/tables-generated-c/wide -I$(SERIALIZE_C) -Ibuild/tables-generated-c/scalars -Ibuild/tables-generated-c/scalars2 -Itest/conformance/c -Ibuild/tables-generated-c/examples \
	-Ibuild/tables-generated-c/v1 -Ibuild/tables-generated-c/v2 \
	-Ibuild/tables-generated-c/p1 -Ibuild/tables-generated-c/p3 \
	-Ibuild/tables-generated-c/block -Ibuild/tables-generated-c/pointers

build/conformance-c: build/tables-generated-c/.stamp $(wildcard test/conformance/c/*.c) $(wildcard test/conformance/c/*.h)
	@mkdir -p build
	$(CC) $(TABLES_CFLAGS) $(C_CONFORMANCE_INCLUDES) $(C_CONFORMANCE_SOURCES) -o $@ -lm

# THE ZERO-COST GATE, C side (docs/SPEC-TABLES.md §2.2). A table with no pointer
# in its by-value closure must pay NOTHING for the pointer machinery — no
# builder, no arena, no reference slot, no lifecycle surface, no extra
# descriptor column. The pointer-free corpus's generated headers must not
# contain one symbol of it.
#
# `examples` IS NOT ON THIS LIST ANY MORE, for the reason its C++ twin is not
# (see TABLES_ZERO_COST_HEADERS in the root Makefile): under the KEYWORD (#823)
# `Guarded.schema`'s `Patrol` is a plain `table` — a guarded branch is exactly
# the construct that keeps it one — so `tables/examples` is a VARIABLE unit and
# carries the machinery by right. The follow-on that brings it back is the same
# one: move `Guarded.schema` into a unit of its own.
.PHONY: tables-c-zero-cost
tables-c-zero-cost: build/tables-generated-c/.stamp
	@for f in build/tables-generated-c/v1/*Table.h \
	          build/tables-generated-c/v2/*Table.h build/tables-generated-c/p1/*Table.h \
	          build/tables-generated-c/p3/*Table.h; do \
		if grep -nE "TableArena|TableWorker|TableRef([^u]|$$)|TableSink|TableCtx|TableRegionSink|kTableSegment|kTableSlab|kTableMaxDepth|is_pointer|Builder|PackMeasure|LoadMeasure|stdatomic" $$f; then \
			echo "ZERO-COST GATE FAILED: pointer machinery leaked into $$f"; exit 1; \
		fi; \
	done
	@echo "tables C zero-cost gate: value-only tables carry no pointer machinery"

# THE GENERIC-WALK GATE, C side (docs/SPEC-TABLES.md §16). The text form is ONE
# walk over the reflection descriptors, not a per-table codec — that is the
# property which makes it schema's rather than a packer's. The walker's source
# must therefore be the SAME BYTES in every generated .c of the corpus, whose
# units disagree about packages, tables, kinds and pointer modes. Nothing
# outside the markers is compared and nothing inside them is normalised away:
# the C walk names no package at all, because its entry points are reached
# through the prefixed wrappers rather than through a namespace.
.PHONY: tables-c-json-walk
tables-c-json-walk: build/tables-generated-c/.stamp
	@rm -rf build/json-walk-c && mkdir -p build/json-walk-c
	@for f in build/tables-generated-c/*/*Table.c; do \
		out=build/json-walk-c/$$(echo $$f | tr / _); \
		awk '/---- json walk: begin ----/,/---- json walk: end ----/' $$f > $$out; \
		if [ ! -s $$out ]; then \
			if grep -q "VARIABLE-LENGTH. Its text form reads through the builder" $${f%.c}.h; then rm -f $$out; continue; fi; \
			echo "GENERIC-WALK GATE FAILED: no walker in $$f"; exit 1; \
		fi; \
	done
	@first=""; for f in build/json-walk-c/*; do \
		if [ -z "$$first" ]; then first=$$f; else \
			cmp -s $$first $$f || { echo "GENERIC-WALK GATE FAILED: the walker in $$f is not the walker in $$first"; exit 1; }; \
		fi; \
	done
	@echo "tables C generic-walk gate: one walker, byte-identical in $$(ls build/json-walk-c | wc -l | tr -d ' ') generated .c files"

# THE C LEG's SANITIZED BUILD (docs/SPEC-TABLES.md §19.5, §7.5): the same driver,
# under ASan and UBSan with no recovery, so a forged block or a forged cook that
# walked one byte past its extent is a crash rather than a silent pass. The
# forgery batteries allocate EXACTLY the extent their caller claims, so an
# over-read lands in a redzone.
C_SANITIZE := -fsanitize=address,undefined -fno-sanitize-recover=all -fno-omit-frame-pointer -g

build/conformance-c-asan: build/tables-generated-c/.stamp $(wildcard test/conformance/c/*.c) $(wildcard test/conformance/c/*.h)
	@mkdir -p build
	$(CC) -std=c99 -Wall -Wextra -Werror -Wshadow -Wtype-limits $(C_TAUTOLOGICAL) \
		-O1 -ffp-contract=off $(C_SANITIZE) $(C_CONFORMANCE_INCLUDES) $(C_CONFORMANCE_SOURCES) -o $@ -lm

# THE FORGERY FUZZER, C side. The conformance batteries are the PINNED damage —
# 11 block rows and 111 cook rows a person reviewed; this is the unpinned half:
# random single-word damage over the same two forms, under the sanitizers, with
# the one invariant an Open owes an untrusted file. It never CRASHES and it
# never reads past the extent its caller claimed, whatever it answers.
# The cook fixtures are a DECLARED prerequisite and not an accident of the
# tree: test/cookgen writes them deterministically, and a fuzzer whose subject
# happens to be lying around is a fuzzer that passes by not running.
build/schema_test_c_fuzz: build/tables-generated-c/.stamp build/cook-open/.stamp test/c-tables/fuzz_main.c $(wildcard test/conformance/c/*.c) $(wildcard test/conformance/c/*.h)
	@mkdir -p build
	$(CC) -std=c99 -Wall -Wextra -Werror -Wshadow -Wtype-limits $(C_TAUTOLOGICAL) \
		-O1 -ffp-contract=off $(C_SANITIZE) $(C_CONFORMANCE_INCLUDES) \
		test/c-tables/fuzz_main.c test/conformance/c/unit_blockdemo.c test/conformance/c/unit_graphdemo.c \
		build/tables-generated-c/block/RenderBlock.c build/tables-generated-c/block/RenderTable.c \
		build/tables-generated-c/block/PaddedBlock.c build/tables-generated-c/block/PaddedTable.c \
		build/tables-generated-c/pointers/GraphTable.c build/tables-generated-c/pointers/MarksTable.c \
		build/tables-generated-c/pointers/PartsTable.c -o $@ -lm

build/schema_test_c_soak: build/tables-generated-c/.stamp test/c-tables/soak_main.c $(wildcard test/conformance/c/*.c) $(wildcard test/conformance/c/*.h)
	@mkdir -p build
	$(CC) -std=c99 -Wall -Wextra -Werror -Wshadow -Wtype-limits $(C_TAUTOLOGICAL) \
		-O2 -ffp-contract=off $(C_CONFORMANCE_INCLUDES) \
		test/c-tables/soak_main.c test/conformance/c/unit_tabledemo.c test/conformance/c/unit_tblv1.c \
		test/conformance/c/unit_tblv2.c test/conformance/c/unit_tblp1.c test/conformance/c/unit_tblp3.c \
		build/tables-generated-c/examples/TablesTable.c build/tables-generated-c/examples/WideTable.c \
		build/tables-generated-c/examples/NestedTable.c build/tables-generated-c/examples/KeyedTable.c \
		build/tables-generated-c/examples/PackTable.c build/tables-generated-c/examples/GuardedTable.c \
		build/tables-generated-c/examples/RangesTable.c \
		build/tables-generated-c/v1/V1Table.c build/tables-generated-c/v2/V2Table.c \
		build/tables-generated-c/p1/P1Table.c build/tables-generated-c/p3/P3Table.c -o $@ -lm

# The file-wire corpus and allocation counters run together.
.PHONY: tables-c-soak
tables-c-soak: build/schema_test_c_soak
	./build/schema_test_c_soak $(SOAK_SECONDS)

.PHONY: tables-c-fuzz
tables-c-fuzz: build/schema_test_c_fuzz
	SEED=$(SEED) N=$(N) ./build/schema_test_c_fuzz

# THE C TABLES LEG, whole. Everything above, plus the conformance driver under
# the sanitizers over every surface it answers.
.PHONY: tables-c
tables-c: tables-c-wire-fuzz build/conformance-c build/conformance-c-asan tables-c-zero-cost tables-c-json-walk tables-c-fuzz tables-c-fuzz-negative-control tables-c-variable
	./build/conformance-harness run --drivers test/conformance/c/drivers-asan.txt --work build/conformance-c-asan-work
	$(MAKE) tables-js-leg
	$(MAKE) tables-js-accessor-negative-control
	$(MAKE) tables-js-fuzz
	$(MAKE) tables-c-keyed-none-refusal-ndebug
	$(MAKE) tables-c-keyed-none-refusal-negative-control
	$(MAKE) tables-c-keyed-max-refusal-ndebug
	$(MAKE) tables-c-keyed-max-refusal-negative-control
	$(MAKE) tables-c-soak SOAK_SECONDS=20
	$(MAKE) tables-c-soak-negative-control
	# THE ORDINAL SLOT CACHE (docs/SPEC-TABLES.md §3): a save that answers a
	# repeat header out of slot[ordinal] is held to main's own bytes by
	# compiler/ctablerefordinal_test.go, and the ONE rule a green pin cannot be
	# read for is the rewind taking the slot back — so its control is here.
	$(MAKE) tables-c-ref-ordinal-negative-control

# THE NEGATIVE CONTROL FOR THE C LEG, and it is the C# control's twin over the
# C emitter: a green matrix row proves nothing until the row is shown capable
# of going red. One field index in the C WALK is sabotaged — the reader takes
# its neighbour's descriptor — and the harness must go red on `json-read`
# ALONE. The second half is the point: json-write must stay green, because the
# sabotage is in the READER; and `wire` must stay green, because the wire codec
# is a different half of the same backend. A control that turned the whole
# column red would be saying "something broke" rather than "the C reader broke".
#
# Nothing tracked is written to: the emitter source is patched into a COPY and
# reached through a Go build overlay, so an interrupt cannot leave a sabotaged
# working tree.
CONFORMANCE_NEGATIVE_C := build/conformance-negative-c
CONFORMANCE_NEGATIVE_C_SED := s|const TableFieldInfo \* f = &info->fields\[index\];|const TableFieldInfo * f = \&info->fields[( index ^ 1 ) < info->num_fields ? ( index ^ 1 ) : index]; /* SABOTAGED */|
.PHONY: conformance-negative-control-c
conformance-negative-control-c: build/conformance-harness
	@rm -rf $(CONFORMANCE_NEGATIVE_C) && mkdir -p $(CONFORMANCE_NEGATIVE_C)
	@sed '$(CONFORMANCE_NEGATIVE_C_SED)' internal/codegen/ctable/json.go > $(CONFORMANCE_NEGATIVE_C)/ctable-json.go.txt
	@cmp -s internal/codegen/ctable/json.go $(CONFORMANCE_NEGATIVE_C)/ctable-json.go.txt && \
		{ echo "NEGATIVE CONTROL: the C emitter sabotage did not apply"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/codegen/ctable/json.go":"%s/$(CONFORMANCE_NEGATIVE_C)/ctable-json.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > $(CONFORMANCE_NEGATIVE_C)/overlay.json
	go build -overlay $(CONFORMANCE_NEGATIVE_C)/overlay.json -o $(CONFORMANCE_NEGATIVE_C)/schema ./cmd/schema
	$(CONFORMANCE_NEGATIVE_C)/schema generate --lang c --out $(CONFORMANCE_NEGATIVE_C)/generated/maps tables/maps
	$(CONFORMANCE_NEGATIVE_C)/schema generate --lang c --out $(CONFORMANCE_NEGATIVE_C)/generated/lists tables/lists
	$(CONFORMANCE_NEGATIVE_C)/schema generate --lang c --out $(CONFORMANCE_NEGATIVE_C)/generated/arms tables/arms
	$(CONFORMANCE_NEGATIVE_C)/schema generate --lang c --out $(CONFORMANCE_NEGATIVE_C)/generated/w1 test/tables/W1.schema
	$(CONFORMANCE_NEGATIVE_C)/schema generate --lang c --out $(CONFORMANCE_NEGATIVE_C)/generated/w2 test/tables/W2.schema
	$(CONFORMANCE_NEGATIVE_C)/schema generate --lang c --out $(CONFORMANCE_NEGATIVE_C)/generated/stream tables/stream
	$(CONFORMANCE_NEGATIVE_C)/schema generate --lang c --out $(CONFORMANCE_NEGATIVE_C)/generated/g1 test/tables/G1.schema
	$(CONFORMANCE_NEGATIVE_C)/schema generate --lang c --out $(CONFORMANCE_NEGATIVE_C)/generated/blobs tables/blobs
	$(CONFORMANCE_NEGATIVE_C)/schema generate --lang c --out $(CONFORMANCE_NEGATIVE_C)/generated/vocab9 tables/vocab9
	$(CONFORMANCE_NEGATIVE_C)/schema generate --lang c --out $(CONFORMANCE_NEGATIVE_C)/generated/vocab tables/vocab
	$(CONFORMANCE_NEGATIVE_C)/schema generate --lang c --out $(CONFORMANCE_NEGATIVE_C)/generated/backend tables/backend
	$(CONFORMANCE_NEGATIVE_C)/schema generate --lang c --out $(CONFORMANCE_NEGATIVE_C)/generated/r2 test/tables/R2.schema
	$(CONFORMANCE_NEGATIVE_C)/schema generate --lang c --out $(CONFORMANCE_NEGATIVE_C)/generated/r1 test/tables/R1.schema
	$(CONFORMANCE_NEGATIVE_C)/schema generate --lang c --out $(CONFORMANCE_NEGATIVE_C)/generated/rt1 test/tables/RT1.schema
	$(CONFORMANCE_NEGATIVE_C)/schema generate --lang c --out $(CONFORMANCE_NEGATIVE_C)/generated/k2 test/tables/K2.schema
	$(CONFORMANCE_NEGATIVE_C)/schema generate --lang c --out $(CONFORMANCE_NEGATIVE_C)/generated/k1 test/tables/K1.schema
	$(CONFORMANCE_NEGATIVE_C)/schema generate --lang c --out $(CONFORMANCE_NEGATIVE_C)/generated/a2 test/tables/A2.schema
	$(CONFORMANCE_NEGATIVE_C)/schema generate --lang c --out $(CONFORMANCE_NEGATIVE_C)/generated/a1 test/tables/A1.schema
	$(CONFORMANCE_NEGATIVE_C)/schema generate --lang c --out $(CONFORMANCE_NEGATIVE_C)/generated/m2 test/tables/M2.schema
	$(CONFORMANCE_NEGATIVE_C)/schema generate --lang c --out $(CONFORMANCE_NEGATIVE_C)/generated/m1 test/tables/M1.schema
	$(CONFORMANCE_NEGATIVE_C)/schema generate --lang c --out $(CONFORMANCE_NEGATIVE_C)/generated/messages tables/messages
	$(CONFORMANCE_NEGATIVE_C)/schema generate --lang c --out $(CONFORMANCE_NEGATIVE_C)/generated/examples tables/examples
	$(CONFORMANCE_NEGATIVE_C)/schema generate --lang c --out $(CONFORMANCE_NEGATIVE_C)/generated/pointers tables/pointers
	$(CONFORMANCE_NEGATIVE_C)/schema generate --lang c --out $(CONFORMANCE_NEGATIVE_C)/generated/block tables/block
	$(CONFORMANCE_NEGATIVE_C)/schema generate --lang c --out $(CONFORMANCE_NEGATIVE_C)/generated/blockhome tables/blockhome
	$(CONFORMANCE_NEGATIVE_C)/schema generate --lang c --out $(CONFORMANCE_NEGATIVE_C)/generated/v1 test/tables/V1.schema
	$(CONFORMANCE_NEGATIVE_C)/schema generate --lang c --out $(CONFORMANCE_NEGATIVE_C)/generated/v2 test/tables/V2.schema
	$(CONFORMANCE_NEGATIVE_C)/schema generate --lang c --out $(CONFORMANCE_NEGATIVE_C)/generated/p1 test/tables/P1.schema
	$(CONFORMANCE_NEGATIVE_C)/schema generate --lang c --out $(CONFORMANCE_NEGATIVE_C)/generated/p2 test/tables/P2.schema
	$(CONFORMANCE_NEGATIVE_C)/schema generate --lang c --out $(CONFORMANCE_NEGATIVE_C)/generated/p3 test/tables/P3.schema
	$(CONFORMANCE_NEGATIVE_C)/schema generate --lang c --out $(CONFORMANCE_NEGATIVE_C)/generated/wide examples-wide
	$(CONFORMANCE_NEGATIVE_C)/schema generate --lang c --out $(CONFORMANCE_NEGATIVE_C)/generated/scalars tables/scalars
	$(CONFORMANCE_NEGATIVE_C)/schema generate --lang c --out $(CONFORMANCE_NEGATIVE_C)/generated/scalars2 test/tables/Scalars2.schema
	$(CONFORMANCE_NEGATIVE_C)/schema generate --lang c --out $(CONFORMANCE_NEGATIVE_C)/generated/jsonkeys test/tables/JsonKeys.schema
	@grep -lq SABOTAGED $(CONFORMANCE_NEGATIVE_C)/generated/*/*Table.c || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotaged emitter emitted an unsabotaged walk"; exit 1; }
	$(CC) $(TABLES_CFLAGS_CONTROL) $(subst build/tables-generated-c,$(CONFORMANCE_NEGATIVE_C)/generated,$(C_CONFORMANCE_INCLUDES)) \
		$(subst build/tables-generated-c,$(CONFORMANCE_NEGATIVE_C)/generated,$(C_CONFORMANCE_SOURCES)) -o $(CONFORMANCE_NEGATIVE_C)/driver-bin -lm
	@printf '#!/bin/sh\nexec "%s/driver-bin" "$$@"\n' "$(CURDIR)/$(CONFORMANCE_NEGATIVE_C)" > $(CONFORMANCE_NEGATIVE_C)/driver
	@chmod +x $(CONFORMANCE_NEGATIVE_C)/driver
	@printf 'c %s/driver\n' "$(CONFORMANCE_NEGATIVE_C)" > $(CONFORMANCE_NEGATIVE_C)/drivers.txt
	@if ./build/conformance-harness run --drivers $(CONFORMANCE_NEGATIVE_C)/drivers.txt \
			--work $(CONFORMANCE_NEGATIVE_C)/work > $(CONFORMANCE_NEGATIVE_C)/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a sabotaged C walker left the harness green"; \
		cat $(CONFORMANCE_NEGATIVE_C)/log; exit 1; \
	fi
	@grep -q "c / json-read" $(CONFORMANCE_NEGATIVE_C)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the harness went red, but not on the sabotaged surface"; \
		  cat $(CONFORMANCE_NEGATIVE_C)/log; exit 1; }
	@grep -q "json-write    pass" $(CONFORMANCE_NEGATIVE_C)/log || \
		{ echo "NEGATIVE CONTROL FAILED: json-write went red too, so the control does not localise the READER"; \
		  cat $(CONFORMANCE_NEGATIVE_C)/log; exit 1; }
	@grep -q "wire          pass" $(CONFORMANCE_NEGATIVE_C)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the whole matrix went red, so it localises nothing"; \
		  cat $(CONFORMANCE_NEGATIVE_C)/log; exit 1; }
	@grep -m1 "c / json-read" $(CONFORMANCE_NEGATIVE_C)/log
	@echo "negative control: one field index off in the C walk turns the harness RED on json-read alone"

# THE NEGATIVE CONTROL FOR THE TWO FOREIGN SURFACES. `cook-foreign` and
# `block-foreign` are the only rows whose EXPECTED ANSWER IS A REFUSAL, so a
# driver that never made the file foreign in the first place would pass them by
# accident on every host: it would open a perfectly good file and, if `open` had
# been the expectation, be right. The control neuters the byte swap and requires
# BOTH foreign rows to go red while `cook` and `block` — the same Opens over the
# same files, unswapped — stay green. That second half is what says the control
# localises the swap rather than breaking the reader.
CONFORMANCE_NEGATIVE_C_FOREIGN := build/conformance-negative-c-foreign
.PHONY: conformance-negative-control-c-foreign
conformance-negative-control-c-foreign: build/conformance-harness build/tables-generated-c/.stamp
	@rm -rf $(CONFORMANCE_NEGATIVE_C_FOREIGN) && mkdir -p $(CONFORMANCE_NEGATIVE_C_FOREIGN)
	@sed 's|if ( bytes < 8 ) { return; }|if ( bytes < 8 ) { return; } /* SABOTAGED */ return;|' \
		test/conformance/c/main.c > $(CONFORMANCE_NEGATIVE_C_FOREIGN)/main.c
	@grep -q SABOTAGED $(CONFORMANCE_NEGATIVE_C_FOREIGN)/main.c || \
		{ echo "NEGATIVE CONTROL: the byte-swap sabotage did not apply"; exit 1; }
	$(CC) $(TABLES_CFLAGS_CONTROL) $(C_CONFORMANCE_INCLUDES) \
		$(CONFORMANCE_NEGATIVE_C_FOREIGN)/main.c $(filter-out test/conformance/c/main.c,$(C_CONFORMANCE_SOURCES)) \
		-o $(CONFORMANCE_NEGATIVE_C_FOREIGN)/driver-bin -lm
	@printf '#!/bin/sh\nexec "%s/driver-bin" "$$@"\n' "$(CURDIR)/$(CONFORMANCE_NEGATIVE_C_FOREIGN)" > $(CONFORMANCE_NEGATIVE_C_FOREIGN)/driver
	@chmod +x $(CONFORMANCE_NEGATIVE_C_FOREIGN)/driver
	@printf 'c %s/driver\n' "$(CONFORMANCE_NEGATIVE_C_FOREIGN)" > $(CONFORMANCE_NEGATIVE_C_FOREIGN)/drivers.txt
	@if ./build/conformance-harness run --drivers $(CONFORMANCE_NEGATIVE_C_FOREIGN)/drivers.txt \
			--work $(CONFORMANCE_NEGATIVE_C_FOREIGN)/work > $(CONFORMANCE_NEGATIVE_C_FOREIGN)/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a driver that never swapped the magic left the harness green"; \
		cat $(CONFORMANCE_NEGATIVE_C_FOREIGN)/log; exit 1; \
	fi
	@grep -q "c / cook-foreign" $(CONFORMANCE_NEGATIVE_C_FOREIGN)/log || \
		{ echo "NEGATIVE CONTROL FAILED: cook-foreign stayed green with no swap"; \
		  cat $(CONFORMANCE_NEGATIVE_C_FOREIGN)/log; exit 1; }
	@grep -q "c / block-foreign" $(CONFORMANCE_NEGATIVE_C_FOREIGN)/log || \
		{ echo "NEGATIVE CONTROL FAILED: block-foreign stayed green with no swap"; \
		  cat $(CONFORMANCE_NEGATIVE_C_FOREIGN)/log; exit 1; }
	@grep -q "^cook          pass" $(CONFORMANCE_NEGATIVE_C_FOREIGN)/log || \
		{ echo "NEGATIVE CONTROL FAILED: cook went red too, so the control does not localise the swap"; \
		  cat $(CONFORMANCE_NEGATIVE_C_FOREIGN)/log; exit 1; }
	@grep -q "^block         pass" $(CONFORMANCE_NEGATIVE_C_FOREIGN)/log || \
		{ echo "NEGATIVE CONTROL FAILED: block went red too, so the control does not localise the swap"; \
		  cat $(CONFORMANCE_NEGATIVE_C_FOREIGN)/log; exit 1; }
	@grep -m1 "c / cook-foreign" $(CONFORMANCE_NEGATIVE_C_FOREIGN)/log
	@echo "negative control: a driver that never makes the file foreign turns cook-foreign and block-foreign RED, and only those"

# THE BIG-ENDIAN C LEG (docs/SPEC-TABLES.md §3). The tolerant wire is
# little-endian by construction — the generated writer spells every width out
# byte by byte and the reader reassembles them the same way — so a BIG-ENDIAN
# build has to reproduce the same goldens a little-endian host wrote. The soak
# binary's golden gate is exactly that assertion, so the leg is the same binary
# cross-compiled and run for zero seconds: it loads the whole corpus, re-saves
# every exact case and byte-compares, then stops.
#
# BE_CC names what CI installed, the way BE_CXX does for the C++ legs; the pair
# is not a system binary and not assumed.
BE_CC ?= s390x-linux-gnu-gcc

build/schema_test_c_soak_be: build/tables-generated-c/.stamp test/c-tables/soak_main.c $(wildcard test/conformance/c/*.c) $(wildcard test/conformance/c/*.h)
	@mkdir -p build
	$(BE_CC) -std=c99 -Wall -Wextra -Werror -Wshadow -O2 -ffp-contract=off -static \
		-DSCHEMA_SOAK_NO_INTERPOSE \
		$(C_CONFORMANCE_INCLUDES) \
		test/c-tables/soak_main.c test/conformance/c/unit_tabledemo.c test/conformance/c/unit_tblv1.c \
		test/conformance/c/unit_tblv2.c test/conformance/c/unit_tblp1.c test/conformance/c/unit_tblp3.c \
		build/tables-generated-c/examples/TablesTable.c build/tables-generated-c/examples/WideTable.c \
		build/tables-generated-c/examples/NestedTable.c build/tables-generated-c/examples/KeyedTable.c \
		build/tables-generated-c/examples/PackTable.c build/tables-generated-c/examples/GuardedTable.c \
		build/tables-generated-c/examples/RangesTable.c \
		build/tables-generated-c/v1/V1Table.c build/tables-generated-c/v2/V2Table.c \
		build/tables-generated-c/p1/P1Table.c build/tables-generated-c/p3/P3Table.c -o $@ -lm

.PHONY: tables-c-big-endian
tables-c-big-endian: build/schema_test_c_soak_be
	$(BE_RUN) ./build/schema_test_c_soak_be 0
	@echo "big-endian C leg: the tolerant wire crosses the byte order — same goldens, byte for byte"

# THE NEGATIVE CONTROL FOR THAT LEG. tables-big-endian-negative-control
# (Makefile) sabotages cpptable's put16, so a C-side byte-order defect in
# table_writer_finish's #else arm — the arm #815 put behind this gate — would
# not turn that control red. This one puts the fallback's explicit little-endian
# stores back to a host-order copy, the same defect class as put16, and requires
# the s390x soak golden to go red. The same sabotage stays green on this
# little-endian host: the #else is not compiled there, and that pair is the
# argument for the C leg the way put16 is for the C++ one. The memcpy arm this
# overlay leaves standing is tables-c-little-endian-negative-control, a host soak.
#
# Overlay, so no tracked file is written.
C_BE_NC := build/c-be-sabotage
C_BE_NC_GEN := $(C_BE_NC)/generated
C_BE_NC_INCLUDES := $(subst build/tables-generated-c,$(C_BE_NC_GEN),$(C_CONFORMANCE_INCLUDES))
C_BE_NC_SOAK_SRCS := test/c-tables/soak_main.c test/conformance/c/unit_tabledemo.c \
	test/conformance/c/unit_tblv1.c test/conformance/c/unit_tblv2.c \
	test/conformance/c/unit_tblp1.c test/conformance/c/unit_tblp3.c \
	$(C_BE_NC_GEN)/examples/TablesTable.c $(C_BE_NC_GEN)/examples/WideTable.c \
	$(C_BE_NC_GEN)/examples/NestedTable.c $(C_BE_NC_GEN)/examples/KeyedTable.c \
	$(C_BE_NC_GEN)/examples/PackTable.c $(C_BE_NC_GEN)/examples/GuardedTable.c \
	$(C_BE_NC_GEN)/examples/RangesTable.c \
	$(C_BE_NC_GEN)/v1/V1Table.c $(C_BE_NC_GEN)/v2/V2Table.c \
	$(C_BE_NC_GEN)/p1/P1Table.c $(C_BE_NC_GEN)/p3/P3Table.c
.PHONY: tables-c-big-endian-negative-control
tables-c-big-endian-negative-control: test/c-tables/soak_main.c
	@rm -rf $(C_BE_NC) && mkdir -p $(C_BE_NC)
	@sed 's|for ( j = 0; j < 8; j++ ) { dst\[i \* 8 + j\] = (uint8_t) ( v >> ( j \* 8 ) ); }|for ( j = 0; j < 8; j++ ) { dst[i * 8 + j] = ( (const uint8_t *) \&v )[j]; } /* SABOTAGED: host order */|' \
		internal/codegen/ctable/wire.go > $(C_BE_NC)/wire.go.txt
	@cmp -s internal/codegen/ctable/wire.go $(C_BE_NC)/wire.go.txt && \
		{ echo "NEGATIVE CONTROL: the C fallback-arm sabotage patched nothing"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/codegen/ctable/wire.go":"%s/$(C_BE_NC)/wire.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > $(C_BE_NC)/overlay.json
	go build -overlay $(C_BE_NC)/overlay.json -o $(C_BE_NC)/schema ./cmd/schema
	$(C_BE_NC)/schema generate --lang c --out $(C_BE_NC_GEN)/examples tables/examples
	$(C_BE_NC)/schema generate --lang c --out $(C_BE_NC_GEN)/v1 test/tables/V1.schema
	$(C_BE_NC)/schema generate --lang c --out $(C_BE_NC_GEN)/v2 test/tables/V2.schema
	$(C_BE_NC)/schema generate --lang c --out $(C_BE_NC_GEN)/p1 test/tables/P1.schema
	$(C_BE_NC)/schema generate --lang c --out $(C_BE_NC_GEN)/p3 test/tables/P3.schema
	@grep -q "SABOTAGED: host order" $(C_BE_NC_GEN)/examples/TablesTable.h || \
		{ echo "NEGATIVE CONTROL: the sabotaged emitter emitted an unsabotaged fallback arm"; exit 1; }
	@grep -q "memcpy( dst, w->vocabulary->ids" $(C_BE_NC_GEN)/examples/TablesTable.h || \
		{ echo "NEGATIVE CONTROL: the sabotage removed the little-endian memcpy arm"; exit 1; }
	$(CC) -std=c99 -Wall -Wextra -Werror -Wshadow -Wtype-limits $(C_TAUTOLOGICAL) \
		-O2 -ffp-contract=off $(C_BE_NC_INCLUDES) $(C_BE_NC_SOAK_SRCS) \
		-o $(C_BE_NC)/soak -lm
	@./$(C_BE_NC)/soak 0 > $(C_BE_NC)/host.log 2>&1 || \
		{ echo "NEGATIVE CONTROL FAILED: a host-order C trailer fallback is visible on a little-endian host — this host is not little-endian, or the sabotage is not the one described"; \
		  cat $(C_BE_NC)/host.log; exit 1; }
	@echo "negative control: a host-order C trailer fallback leaves the LITTLE-ENDIAN soak green"
	$(BE_CC) -std=c99 -Wall -Wextra -Werror -Wshadow -O2 -ffp-contract=off -static \
		-DSCHEMA_SOAK_NO_INTERPOSE $(C_BE_NC_INCLUDES) $(C_BE_NC_SOAK_SRCS) \
		-o $(C_BE_NC)/soak_be -lm
	@if $(BE_RUN) ./$(C_BE_NC)/soak_be 0 > $(C_BE_NC)/be.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a host-order C trailer fallback left the s390x golden gate green"; \
		cat $(C_BE_NC)/be.log; exit 1; \
	fi
	@grep -q "does not re-save to its own bytes" $(C_BE_NC)/be.log || \
		{ echo "NEGATIVE CONTROL FAILED: the s390x soak went red, but not on a wire golden"; \
		  cat $(C_BE_NC)/be.log; exit 1; }
	@echo "negative control: the same C fallback turns the s390x golden gate red, on the wire goldens"

# THE LITTLE-ENDIAN MEMCPY ARM of table_writer_finish. The control above
# sabotages the #else fallback and leaves this copy standing; on this host the
# #else is not compiled, so the ordinary soak runs the memcpy and nothing named
# was watching it. Reversing the interned id words here must turn the host soak
# red on the same "does not re-save to its own bytes" golden. Overlay, so no
# tracked file is written. Host-only: the negative-controls matrix runs it.
C_LE_NC := build/c-le-sabotage
C_LE_NC_GEN := $(C_LE_NC)/generated
C_LE_NC_INCLUDES := $(subst build/tables-generated-c,$(C_LE_NC_GEN),$(C_CONFORMANCE_INCLUDES))
C_LE_NC_SOAK_SRCS := test/c-tables/soak_main.c test/conformance/c/unit_tabledemo.c \
	test/conformance/c/unit_tblv1.c test/conformance/c/unit_tblv2.c \
	test/conformance/c/unit_tblp1.c test/conformance/c/unit_tblp3.c \
	$(C_LE_NC_GEN)/examples/TablesTable.c $(C_LE_NC_GEN)/examples/WideTable.c \
	$(C_LE_NC_GEN)/examples/NestedTable.c $(C_LE_NC_GEN)/examples/KeyedTable.c \
	$(C_LE_NC_GEN)/examples/PackTable.c $(C_LE_NC_GEN)/examples/GuardedTable.c \
	$(C_LE_NC_GEN)/examples/RangesTable.c \
	$(C_LE_NC_GEN)/v1/V1Table.c $(C_LE_NC_GEN)/v2/V2Table.c \
	$(C_LE_NC_GEN)/p1/P1Table.c $(C_LE_NC_GEN)/p3/P3Table.c
.PHONY: tables-c-little-endian-negative-control
tables-c-little-endian-negative-control: test/c-tables/soak_main.c
	@rm -rf $(C_LE_NC) && mkdir -p $(C_LE_NC)
	@sed 's|if ( count > 0 ) { memcpy( dst, w->vocabulary->ids, (size_t) ( count \* 8 ) ); }|if ( count > 0 ) { int64_t k; for ( k = 0; k < count; k++ ) { memcpy( dst + k * 8, w->vocabulary->ids + ( count - 1 - k ), 8 ); } } /* SABOTAGED: reversed id words */|' \
		internal/codegen/ctable/wire.go > $(C_LE_NC)/wire.go.txt
	@cmp -s internal/codegen/ctable/wire.go $(C_LE_NC)/wire.go.txt && \
		{ echo "NEGATIVE CONTROL: the C memcpy-arm sabotage patched nothing"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/codegen/ctable/wire.go":"%s/$(C_LE_NC)/wire.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > $(C_LE_NC)/overlay.json
	go build -overlay $(C_LE_NC)/overlay.json -o $(C_LE_NC)/schema ./cmd/schema
	$(C_LE_NC)/schema generate --lang c --out $(C_LE_NC_GEN)/examples tables/examples
	$(C_LE_NC)/schema generate --lang c --out $(C_LE_NC_GEN)/v1 test/tables/V1.schema
	$(C_LE_NC)/schema generate --lang c --out $(C_LE_NC_GEN)/v2 test/tables/V2.schema
	$(C_LE_NC)/schema generate --lang c --out $(C_LE_NC_GEN)/p1 test/tables/P1.schema
	$(C_LE_NC)/schema generate --lang c --out $(C_LE_NC_GEN)/p3 test/tables/P3.schema
	@grep -q "SABOTAGED: reversed id words" $(C_LE_NC_GEN)/examples/TablesTable.h || \
		{ echo "NEGATIVE CONTROL: the sabotaged emitter emitted an unsabotaged memcpy arm"; exit 1; }
	@grep -Fq "v >> ( j * 8 )" $(C_LE_NC_GEN)/examples/TablesTable.h || \
		{ echo "NEGATIVE CONTROL: the sabotage removed the big-endian fallback arm"; exit 1; }
	$(CC) -std=c99 -Wall -Wextra -Werror -Wshadow -Wtype-limits $(C_TAUTOLOGICAL) \
		-O2 -ffp-contract=off $(C_LE_NC_INCLUDES) $(C_LE_NC_SOAK_SRCS) \
		-o $(C_LE_NC)/soak -lm
	@if ./$(C_LE_NC)/soak 0 > $(C_LE_NC)/host.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: reversed id words in the little-endian memcpy arm left the host soak green"; \
		cat $(C_LE_NC)/host.log; exit 1; \
	fi
	@grep -q "does not re-save to its own bytes" $(C_LE_NC)/host.log || \
		{ echo "NEGATIVE CONTROL FAILED: the host soak went red, but not on a wire golden"; \
		  cat $(C_LE_NC)/host.log; exit 1; }
	@echo "negative control: reversed id words in the C memcpy arm turn the host soak red, on the wire goldens"

# THE KEYED None REFUSAL, C side (docs/SPEC-TABLES.md §2.4). C's accessor is a
# macro over table_keyed_slot rather than an operator[] — the one spelling that
# differs from the reference — and the refusal inside it is the same assert plus
# the same abort. -DNDEBUG is the configuration that removes the assert and the
# configuration a game ships; the child must still die.
.PHONY: tables-c-keyed-none-refusal-ndebug
tables-c-keyed-none-refusal-ndebug: build/tables-generated-c/.stamp test/c-tables/keyed_none_ndebug_main.c
	@mkdir -p build
	$(CC) $(TABLES_CFLAGS) -DNDEBUG -Ibuild/tables-generated-c/examples \
		test/c-tables/keyed_none_ndebug_main.c -o build/schema_test_c_keyed_none_ndebug -lm
	./build/schema_test_c_keyed_none_ndebug

# THE NEGATIVE CONTROL for it: a gate that has never seen the refusal go
# MISSING is watching nothing. The accessor's refusal is deleted from a COPY of
# the emitter, the corpus is regenerated from it, and the same child must then
# survive — which is the defect this gate exists to catch, demonstrated.
.PHONY: tables-c-keyed-none-refusal-negative-control
tables-c-keyed-none-refusal-negative-control: bin/schema test/c-tables/keyed_none_ndebug_main.c
	@rm -rf build/c-keyed-sabotage && mkdir -p build/c-keyed-sabotage
	@sed 's|        schema_fatal();|        /* SABOTAGED: the abort is gone */ (void) 0;|' \
		internal/codegen/ctable/ctable.go > build/c-keyed-sabotage/ctable.go.txt
	@cmp -s internal/codegen/ctable/ctable.go build/c-keyed-sabotage/ctable.go.txt && \
		{ echo "NEGATIVE CONTROL: the sabotage patched nothing"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/codegen/ctable/ctable.go":"%s/build/c-keyed-sabotage/ctable.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/c-keyed-sabotage/overlay.json
	go build -overlay build/c-keyed-sabotage/overlay.json -o build/c-keyed-sabotage/schema ./cmd/schema
	build/c-keyed-sabotage/schema generate --lang c --out build/c-keyed-sabotage/generated tables/examples
	@grep -q "SABOTAGED" build/c-keyed-sabotage/generated/KeyedTable.h || \
		{ echo "NEGATIVE CONTROL: the sabotaged emitter emitted an unsabotaged accessor"; exit 1; }
	$(CC) $(TABLES_CFLAGS) -Wno-error -DNDEBUG -Ibuild/c-keyed-sabotage/generated \
		test/c-tables/keyed_none_ndebug_main.c -o build/c-keyed-sabotage/probe -lm
	@if ./build/c-keyed-sabotage/probe > build/c-keyed-sabotage/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the gate stayed green with the refusal deleted"; \
		cat build/c-keyed-sabotage/log; exit 1; \
	fi
	@grep -q "did NOT end the program" build/c-keyed-sabotage/log || \
		{ echo "NEGATIVE CONTROL FAILED: the gate went red for some other reason"; \
		  cat build/c-keyed-sabotage/log; exit 1; }
	@echo "negative control: deleting the C accessor's abort turns the None-refusal gate RED"

# THE OTHER END OF THE SAME REFUSAL, C side (docs/SPEC-TABLES.md §2.4). C++ has
# tables-keyed-max-refusal-ndebug; C had only TestCTableKeyedBounds (longjmp).
# A shipped -DNDEBUG build must still die on a key past Max.
.PHONY: tables-c-keyed-max-refusal-ndebug
tables-c-keyed-max-refusal-ndebug: build/tables-generated-c/.stamp test/c-tables/keyed_max_ndebug_main.c
	@mkdir -p build
	$(CC) $(TABLES_CFLAGS) -DNDEBUG -Ibuild/tables-generated-c/examples \
		test/c-tables/keyed_max_ndebug_main.c -o build/schema_test_c_keyed_max_ndebug -lm
	./build/schema_test_c_keyed_max_ndebug

# and its NEGATIVE CONTROL: put the accessor back to the None-ONLY compare —
# what it refused before both ends stood — and the gate above must go RED.
.PHONY: tables-c-keyed-max-refusal-negative-control
tables-c-keyed-max-refusal-negative-control: bin/schema test/c-tables/keyed_max_ndebug_main.c
	@rm -rf build/c-keyed-max-sabotage && mkdir -p build/c-keyed-max-sabotage
	@sed 's|    if ( (uint32_t) key - 1u >= count )|    if ( key == 0 ) /* SABOTAGED: the None-only compare again */|' \
		internal/codegen/ctable/ctable.go > build/c-keyed-max-sabotage/ctable.go.txt
	@cmp -s internal/codegen/ctable/ctable.go build/c-keyed-max-sabotage/ctable.go.txt && \
		{ echo "NEGATIVE CONTROL: the sabotage patched nothing"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/codegen/ctable/ctable.go":"%s/build/c-keyed-max-sabotage/ctable.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/c-keyed-max-sabotage/overlay.json
	go build -overlay build/c-keyed-max-sabotage/overlay.json -o build/c-keyed-max-sabotage/schema ./cmd/schema
	build/c-keyed-max-sabotage/schema generate --lang c --out build/c-keyed-max-sabotage/generated tables/examples
	@grep -q "SABOTAGED" build/c-keyed-max-sabotage/generated/KeyedTable.h || \
		{ echo "NEGATIVE CONTROL: the sabotaged emitter emitted an unsabotaged accessor"; exit 1; }
	$(CC) $(TABLES_CFLAGS) -Wno-error -DNDEBUG -Ibuild/c-keyed-max-sabotage/generated \
		test/c-tables/keyed_max_ndebug_main.c -o build/c-keyed-max-sabotage/probe -lm
	@if ./build/c-keyed-max-sabotage/probe > build/c-keyed-max-sabotage/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a None-only compare left the past-Max gate GREEN"; \
		cat build/c-keyed-max-sabotage/log; exit 1; \
	fi
	@grep -q "the refusal was compiled out" build/c-keyed-max-sabotage/log || \
		{ echo "NEGATIVE CONTROL FAILED: the gate went red, but not on the refusal"; \
		  cat build/c-keyed-max-sabotage/log; exit 1; }
	@echo "negative control: the None-only compare turns the C past-Max refusal gate red"

# THE VARIABLE-LENGTH CLASS, end to end (docs/SPEC-TABLES.md §2, §6, §9). The
# conformance corpus reaches every FIXED surface and none of this one: its
# instances are all fixed, because the harness's wire goldens are. So the
# pointer class gets its own gate — build a graph through the arena, Lock it,
# save the mutable form and the locked region and prove they write the SAME
# BYTES, size a region from the wire's framing alone, load into it, and walk
# the graph back out through the region's own self-relative derefs.
#
# It runs twice, plain and under ASan + UBSan: the arena, the pack walk and the
# region sink are the three places in this backend that do pointer arithmetic
# on caller-owned memory, and a sanitizer is what says they stayed inside it.
build/schema_test_c_variable: build/tables-generated-c/.stamp test/c-tables/variable_main.c
	@mkdir -p build
	$(CC) $(TABLES_CFLAGS) -Ibuild/tables-generated-c/pointers \
		test/c-tables/variable_main.c build/tables-generated-c/pointers/GraphTable.c \
		build/tables-generated-c/pointers/MarksTable.c build/tables-generated-c/pointers/PartsTable.c -o $@ -lm

build/schema_test_c_variable_asan: build/tables-generated-c/.stamp test/c-tables/variable_main.c
	@mkdir -p build
	$(CC) -std=c99 -Wall -Wextra -Werror -Wshadow -Wtype-limits $(C_TAUTOLOGICAL) \
		-O1 -ffp-contract=off $(C_SANITIZE) -Ibuild/tables-generated-c/pointers \
		test/c-tables/variable_main.c build/tables-generated-c/pointers/GraphTable.c \
		build/tables-generated-c/pointers/MarksTable.c build/tables-generated-c/pointers/PartsTable.c -o $@ -lm

.PHONY: tables-c-variable
tables-c-variable: build/schema_test_c_variable build/schema_test_c_variable_asan
	./build/schema_test_c_variable
	./build/schema_test_c_variable_asan

# THE NEGATIVE CONTROL FOR THE FORGERY FUZZER, and it is the one that changed
# the fuzzer's design.
#
# A fuzzer that only OPENS a forged file proves the checks never crash and
# nothing at all about whether they are load-bearing — an Open validates and
# points, so a removed guard produces a wrong `open` and no symptom. This
# control is what said so: with the extent pair deleted from a COPY of the
# generated reader, the original fuzzer stayed green. It goes red now, because
# the fuzzer WALKS what it opened and the walk is where a guard that stopped
# guarding reads past the caller's buffer, into a redzone.
#
# The guards deleted are BOTH halves of the extent bound — the rows-inside-the-
# extent check and the padding check that catches the same forgery as a side
# effect. Deleting one alone leaves the reader correct, which is itself worth
# knowing and is why this control names two lines rather than one.
.PHONY: tables-c-fuzz-negative-control
tables-c-fuzz-negative-control: build/tables-generated-c/.stamp build/cook-open/.stamp
	@rm -rf build/c-fuzz-sabotage && mkdir -p build/c-fuzz-sabotage
	@cp -r build/tables-generated-c/block build/c-fuzz-sabotage/
	@sed -i.bak -e 's|if(rows>(uint64_t)bytes-offset) { return table_cook_refuse(reason,SCHEMA_TABLE_REFUSE_BAD_LAYOUT)!=NULL; }|/* SABOTAGED */|' \
	            -e 's|if(padding>bytes-used) { return table_cook_refuse(reason,SCHEMA_TABLE_REFUSE_TRUNCATED)!=NULL; }|/* SABOTAGED */|' \
		build/c-fuzz-sabotage/block/RenderBlock.c
	@grep -q SABOTAGED build/c-fuzz-sabotage/block/RenderBlock.c || \
		{ echo "NEGATIVE CONTROL: the sabotage patched nothing"; exit 1; }
	$(CC) -std=c99 -Wall -Wextra -Wno-error -O1 -ffp-contract=off $(C_SANITIZE) \
		-Itest/conformance/c -Ibuild/c-fuzz-sabotage/block -Ibuild/tables-generated-c/pointers \
		test/c-tables/fuzz_main.c test/conformance/c/unit_blockdemo.c test/conformance/c/unit_graphdemo.c \
		build/c-fuzz-sabotage/block/RenderBlock.c build/c-fuzz-sabotage/block/RenderTable.c \
		build/c-fuzz-sabotage/block/PaddedBlock.c build/c-fuzz-sabotage/block/PaddedTable.c \
		build/tables-generated-c/pointers/GraphTable.c build/tables-generated-c/pointers/MarksTable.c \
		build/tables-generated-c/pointers/PartsTable.c -o build/c-fuzz-sabotage/fuzz -lm
	@if SEED=1 N=50000 ./build/c-fuzz-sabotage/fuzz > build/c-fuzz-sabotage/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the extent guards are gone and the fuzzer stayed green — it is not reading what it opens"; \
		cat build/c-fuzz-sabotage/log; exit 1; \
	fi
	@grep -q "heap-buffer-overflow" build/c-fuzz-sabotage/log || \
		{ echo "NEGATIVE CONTROL FAILED: the fuzzer went red for some other reason"; \
		  cat build/c-fuzz-sabotage/log; exit 1; }
	@echo "negative control: deleting the block reader's extent guards turns the forgery fuzzer RED, as a heap over-read"

# THE NEGATIVE CONTROL FOR THE SOAK, and it is the one the blind read asked
# for. The soak's live-byte sample is a LEAK instrument: it reads a number
# after the first iteration and again at the end, so a malloc/free PAIR inside
# the loop is invisible to it — the number returns to where it was before it is
# ever read, and the run still prints "allocate nothing". The call COUNTER is
# what makes the claim, and a counter nobody has seen go red is a counter
# nobody can size.
#
# One matched malloc/free pair per iteration is planted in a COPY of the soak.
# The drift gate must stay silent — that is the half being demonstrated — and
# the call count must refuse.
.PHONY: tables-c-soak-negative-control
tables-c-soak-negative-control: build/tables-generated-c/.stamp
	@rm -rf build/c-soak-sabotage && mkdir -p build/c-soak-sabotage
	@sed 's|            codec->load( value, loaded\[i\].wire, (int64_t) loaded\[i\].bytes, \&report );|            { void * sabotage = malloc( 1 ); *(volatile char *) sabotage = 1; free( sabotage ); } /* SABOTAGED: one matched pair, invisible to a live-byte sample. The volatile store is what stops the optimiser deleting a dead allocation outright, which gcc does at -O2 — a control the compiler removed proves nothing. */\n            codec->load( value, loaded[i].wire, (int64_t) loaded[i].bytes, \&report );|' \
		test/c-tables/soak_main.c > build/c-soak-sabotage/soak_main.c
	@grep -q SABOTAGED build/c-soak-sabotage/soak_main.c || \
		{ echo "NEGATIVE CONTROL: the sabotage patched nothing"; exit 1; }
	$(CC) $(TABLES_CFLAGS) $(C_CONFORMANCE_INCLUDES) \
		build/c-soak-sabotage/soak_main.c test/conformance/c/unit_tabledemo.c test/conformance/c/unit_tblv1.c \
		test/conformance/c/unit_tblv2.c test/conformance/c/unit_tblp1.c test/conformance/c/unit_tblp3.c \
		build/tables-generated-c/examples/TablesTable.c build/tables-generated-c/examples/WideTable.c \
		build/tables-generated-c/examples/NestedTable.c build/tables-generated-c/examples/KeyedTable.c \
		build/tables-generated-c/examples/PackTable.c build/tables-generated-c/examples/GuardedTable.c \
		build/tables-generated-c/examples/RangesTable.c \
		build/tables-generated-c/v1/V1Table.c build/tables-generated-c/v2/V2Table.c \
		build/tables-generated-c/p1/P1Table.c build/tables-generated-c/p3/P3Table.c \
		-o build/c-soak-sabotage/soak -lm
	@if ./build/c-soak-sabotage/soak 2 > build/c-soak-sabotage/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a malloc/free pair per iteration left the soak green"; \
		cat build/c-soak-sabotage/log; exit 1; \
	fi
	@grep -q "allocator call" build/c-soak-sabotage/log || \
		{ echo "NEGATIVE CONTROL FAILED: the soak went red, but not on the call count"; \
		  cat build/c-soak-sabotage/log; exit 1; }
	@grep -q "live allocation" build/c-soak-sabotage/log || \
		{ echo "NEGATIVE CONTROL FAILED: the drift half did not run"; cat build/c-soak-sabotage/log; exit 1; }
	@grep -m1 "SOAK FAILED" build/c-soak-sabotage/log
	@echo "negative control: a matched malloc/free pair per iteration is INVISIBLE to the drift gate and turns the CALL COUNT red"

# THE ORDINAL SLOT CACHE'S OWN CONTROL, C LEG (docs/SPEC-TABLES.md §3). Every
# id a generated field or arm header names is a compile-time constant, so it
# carries an ORDINAL and table_writer_id_at answers a repeat out of
# slot[ordinal]. What keeps that honest is the REWIND: an ELIDED field interns
# its ids and gives them back, and a slot left standing over a POPPED entry
# hands out a reference to an entry the file no longer carries. Remove that one
# clause and compiler/ctablerefordinal_test.go's byte pin — taken from the C
# emitter that had no cache — must go RED. Through `go build -overlay`, so no
# tracked file moves.
.PHONY: tables-c-ref-ordinal-negative-control
tables-c-ref-ordinal-negative-control:
	@rm -rf build/c-ref-ordinal-nc && mkdir -p build/c-ref-ordinal-nc
	@sed -e 's|o=v->ordinal_of\[v->count\]; if(o>=0) { v->slot\[o\]=-1; }|o=v->ordinal_of[v->count]; (void)o; /* NEGATIVE CONTROL: the slot is not taken back */|' \
		internal/codegen/ctable/wire.go > build/c-ref-ordinal-nc/emitter.go.txt
	@cmp -s internal/codegen/ctable/wire.go build/c-ref-ordinal-nc/emitter.go.txt && \
		{ echo "NEGATIVE CONTROL: the rewind sabotage patched nothing"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/codegen/ctable/wire.go":"%s/build/c-ref-ordinal-nc/emitter.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/c-ref-ordinal-nc/overlay.json
	@if SCHEMA_SLOW=1 go test -v -overlay build/c-ref-ordinal-nc/overlay.json -count=1 ./compiler \
			-run TestCTableRefOrdinalBytes > build/c-ref-ordinal-nc/log 2>&1; then \
		grep -q -- '--- PASS' build/c-ref-ordinal-nc/log || \
			{ echo "NEGATIVE CONTROL FAILED: the byte pin did not run (skipped), so this control is watching nothing"; \
			  cat build/c-ref-ordinal-nc/log; exit 1; }; \
		echo "NEGATIVE CONTROL FAILED: the rewind leaves the ordinal slot standing and the byte pin stayed green"; \
		cat build/c-ref-ordinal-nc/log; exit 1; \
	fi
	@grep -qE "ROUND TRIP MOVED|THE SAVED BYTES MOVED" build/c-ref-ordinal-nc/log || \
		{ echo "NEGATIVE CONTROL FAILED: the byte pin went red, but not on the file the save wrote"; \
		  cat build/c-ref-ordinal-nc/log; exit 1; }
	@grep -m1 -E "ROUND TRIP MOVED|THE SAVED BYTES MOVED" build/c-ref-ordinal-nc/log
	@echo "negative control: a rewind that leaves the ordinal slot standing names a POPPED entry — the file stops round-tripping"

# THE SECOND HALF OF THE SAME RULE HAS NO C CONTROL, and the reason is a fact
# about this emitter rather than an omission. C++'s
# tables-ref-ordinal-shared-negative-control removes the ordinal recording from
# ref_at's HIT path and its shared-id driver goes red, because a C++ enum-keyed
# array interns its KEY before it measures the slot's element: the element's
# ref_at only hits, the entry is truncated when the slot elides, and the slot
# cache is left naming a popped entry.
#
# THE C EMITTER CANNOT REACH THAT STATE. Every rewind here that pops an entry
# is a FRAME PROBE immediately followed by an IDENTICAL REDO of the same region
# (internal/codegen/ctable/wire.go, wireFrame and the keyed-array element
# write), and the probes that are NOT redone — the `check_default` default
# probe and the keyed `slot_probe` that decide elision — intern NOTHING,
# because check_default returns at the first riding field BEFORE the header's
# id is written. A general-path append and the table_writer_id_at that hits it
# therefore always sit inside one rewound region, in that order, so the redo
# re-appends at the same index before any id_at can consult the cache. Held
# empirically as well as by reading: with the hit-path recording removed EXACTLY
# as C++'s control removes it, and a stale-slot detector compiled into
# table_writer_id_at, the whole C conformance corpus (18 surfaces, every one
# passing) and the wire fuzzer at 252,842 mutants stayed green and the detector
# never fired -- while the SAME detector aborts immediately when the rewind
# clause above is the one removed, so its silence is a measurement and not a
# dead check. compiler/ctablerefordinal_test.go's own drivers, both of them,
# also stay green under that sabotage.
#
# The recording stays in table_writer_id_at all the same: it is what makes the
# cache LOCALLY correct — slot[ordinal] always names a live entry whose
# ordinal_of points back — rather than correct by an argument about probe/redo
# pairing that a change to the measure path could quietly retire.

# The C half of `make update-goldens`: the committed generated table sources
# (testdata/golden/tables/*-c).
.PHONY: update-goldens-c
update-goldens-c: build/tables-generated-c/.stamp
	@for d in examples block pointers; do \
		mkdir -p testdata/golden/tables/$$d-c; \
		cp build/tables-generated-c/$$d/*Table.h build/tables-generated-c/$$d/*Table.c build/tables-generated-c/$$d/*View.h build/tables-generated-c/$$d/*View.c testdata/golden/tables/$$d-c/; \
	done

# The same registry listing oracle as the reference, through the C surface.
.PHONY: tables-c-view
tables-c-view: bin/schema test/c-tables/view_main.c
	@rm -rf build/c-view
	@mkdir -p build/c-view
	@set -e; for entry in $(VIEW_CORPUS); do \
		dir=$${entry%%:*}; pkg=$${entry##*:}; \
		cap=$$(printf '%s' "$$pkg" | cut -c1 | tr 'a-z' 'A-Z')$$(printf '%s' "$$pkg" | cut -c2-); \
		source=tables/$$dir; if [ "$$dir" = wide ]; then source=examples-wide; fi; \
		./bin/schema generate --lang c --out build/c-view/$$dir $$source; \
		$(CC) $(TABLES_CFLAGS) -Ibuild/c-view/$$dir -I$(SERIALIZE_C) \
			-DVIEW_HEADER="\"$${cap}View.h\"" test/c-tables/view_main.c \
			build/c-view/$$dir/*Table.c build/c-view/$$dir/*View.c -o build/c-view/prog-$$pkg -lm; \
		./build/c-view/prog-$$pkg > build/c-view/$$pkg.listing; \
	done
	SCHEMA_VIEW_LISTING_DIR="$(CURDIR)/build/c-view" go test ./internal/viewlisting -run TestUnitViewListingMatchesTheIR
	@echo "C unit registry: $(words $(VIEW_CORPUS)) units match the independent listing"

test-c tables-c: tables-c-view

# THE C LEG of `make test` (docs/SPEC-TABLES.md; test/conformance/README.md):
# the same corpus in C, with the two gates that hold the emitter honest, the
# forgery fuzzer under ASan and UBSan, and a short soak.
#
# THE SHORT FORMS RIDE HERE AND THE LONG ONES DO NOT, because `make test` runs
# on every push and had three minutes of headroom before a fifth leg existed.
# Every gate below FIRES here — the fuzzer's enumerated passes cover the
# boundaries whatever N is, and the soak's allocator-call gate reads the same
# at two seconds as at twenty — and what the long forms buy is more random
# mutants and more wall clock. `make tables-c` runs the leg whole at the full
# N, and the HOUR-long soak is a release act: `make tables-c-soak
# SOAK_SECONDS=3600`.

# THE FIXED FORM'S VERSIONING CONFORMANCE ON THE C LEG (docs/SPEC-TABLES.md
# §3.4). The invariant §3.4 states before anything else is that a fixed record
# is positional BY PLAN and the positions are the WRITER's block, never the
# reader's own layout — so the cases here are the C++ reference's cases
# (test/tables/fixedform_main.cpp), read through the same one plan-driven path.
#
# EACH GENERATION IS ITS OWN TRANSLATION UNIT. C has no namespace, so the
# reference's tblfx1::FxRoot beside tblfx2::FxRoot has no C spelling: two
# generations of one schema cannot share a TU (SPEC §6.1). Each side gets a .c,
# test/c-tables/fixedform.h is the whole surface between them and names no
# generated type at all, and the main links them — the shape the conformance
# driver already uses for two generations of one schema.
#
# FU1/FU2 is the TEXT-UNDER-AN-ARM pair whose compiled path is a TRAILING
# FIELD, not a slid ordinal: FU1's second arm carries a string(8), FU2 appends
# `extra` so a read of FU1's bytes is a compiled plan, and the two reads have
# to agree on the text (reference-fix 12). UT1/UT2 is the two-lane / slid-arm
# half of the same hole. C++ already generates the pair; this stamp did not.
build/tables-generated-c-fixed/.stamp: bin/schema test/tables/FX1.schema test/tables/FX2.schema test/tables/V1.schema test/tables/V2.schema test/tables/UT1.schema test/tables/UT2.schema test/tables/FU1.schema test/tables/FU2.schema
	@mkdir -p build/tables-generated-c-fixed
	./bin/schema generate --lang c --out build/tables-generated-c-fixed/fx1 test/tables/FX1.schema
	./bin/schema generate --lang c --out build/tables-generated-c-fixed/fx2 test/tables/FX2.schema
	./bin/schema generate --lang c --out build/tables-generated-c-fixed/v1 test/tables/V1.schema
	./bin/schema generate --lang c --out build/tables-generated-c-fixed/v2 test/tables/V2.schema
	./bin/schema generate --lang c --out build/tables-generated-c-fixed/ut1 test/tables/UT1.schema
	./bin/schema generate --lang c --out build/tables-generated-c-fixed/ut2 test/tables/UT2.schema
	./bin/schema generate --lang c --out build/tables-generated-c-fixed/fu1 test/tables/FU1.schema
	./bin/schema generate --lang c --out build/tables-generated-c-fixed/fu2 test/tables/FU2.schema
	@touch $@

C_FIXEDFORM_SOURCES := test/c-tables/fixedform_main.c test/c-tables/fixedform_fx1.c test/c-tables/fixedform_fx2.c \
	test/c-tables/fixedform_layout.c \
	test/c-tables/fixedform_v1.c test/c-tables/fixedform_v2.c \
	test/c-tables/fixedform_ut1.c test/c-tables/fixedform_ut2.c \
	test/c-tables/fixedform_fu1.c test/c-tables/fixedform_fu2.c
C_FIXEDFORM_INCLUDES := -Itest/c-tables -Ibuild/tables-generated-c-fixed/fx1 -Ibuild/tables-generated-c-fixed/fx2 \
	-Ibuild/tables-generated-c-fixed/v1 -Ibuild/tables-generated-c-fixed/v2 \
	-Ibuild/tables-generated-c-fixed/ut1 -Ibuild/tables-generated-c-fixed/ut2 \
	-Ibuild/tables-generated-c-fixed/fu1 -Ibuild/tables-generated-c-fixed/fu2 -I$(SERIALIZE_C)

build/schema_test_c_fixedform: build/tables-generated-c-fixed/.stamp $(C_FIXEDFORM_SOURCES) test/c-tables/fixedform.h
	@mkdir -p build
	$(CC) $(TABLES_CFLAGS) $(C_FIXEDFORM_INCLUDES) $(C_FIXEDFORM_SOURCES) -o $@ -lm

# THE SANITIZED TWIN, and it is the point of the pair. A fixed record carries
# no lengths and no terminators, so every offset the reader uses is arithmetic
# over sizes a STRANGER wrote down: "the reader never leaves the buffer" is a
# claim only a sanitizer can hold, and the C++ leg's own twin says the same.
build/schema_test_c_fixedform_asan: build/tables-generated-c-fixed/.stamp $(C_FIXEDFORM_SOURCES) test/c-tables/fixedform.h
	@mkdir -p build
	$(CC) $(TABLES_CFLAGS_CONTROL) $(C_SANITIZE) $(C_FIXEDFORM_INCLUDES) $(C_FIXEDFORM_SOURCES) -o $@ -lm

.PHONY: tables-c-fixedform
tables-c-fixedform: build/schema_test_c_fixedform build/schema_test_c_fixedform_asan
	./build/schema_test_c_fixedform
	./build/schema_test_c_fixedform_asan

# THE VERSIONING HALF OF THE FIXED FORM ON THE C LEG (§5 of
# docs/FIXED-FORM-ALGORITHM.md, the rows of docs/FIXED-FORM-VERSIONING-TESTS.md).
# internal/codegen/ctable/fixedversioning_test.go reads the C++ reference's byte
# oracle out of build/fixedform-corpus — `old_<row>.bin`, `new_<row>.bin` and
# the floor, hash and lineage-merge files — and holds each row's two read
# columns: NEW-READS-OLD lands every old value with §5.4's counters, and
# OLD-REFUSES-NEW answers `layout_newer` before any record, on the file's hash
# alone (§5.3).
#
# EACH SIDE IS ITS OWN TRANSLATION UNIT AND ITS OWN BINARY. C has no namespace,
# so two generations of one table name cannot share a TU (SPEC §6.1, the FX1/FX2
# precedent above) — so the harness GENERATES a probe unit per row per column,
# compiles it with $(CC) against the generated header, and runs it.
#
# THIS TARGET EXISTS BECAUSE `go test ./...` ASSERTS NOTHING HERE. The suite's
# harness SKIPS itself when build/fixedform-corpus is absent, which is right for
# a bare `go test ./...` on a tree that never built the oracle — and is exactly
# how a §5 regression would have ridden into CI green. So the target BUILDS THE
# ORACLE FIRST (`tables-fixedform-corpus`, the C++ reference's own dump) and then
# sets SCHEMA_REQUIRE_CORPUS=1, which turns that skip into a FAILURE: under this
# name a missing corpus can never pass silently.
.PHONY: tables-c-versioning
tables-c-versioning: tables-fixedform-corpus
	SCHEMA_REQUIRE_CORPUS=1 go test ./internal/codegen/ctable/ -count=1 -run 'TestFixedVersioning'
	@echo 'tables C versioning: §5 read both columns of every row against the C++ reference bytes'

.PHONY: test-c
test-c: build/schema_test_c build/schema_test_c_ludicrous build/schema_test_bench_c build/conformance-harness build/conformance-c build/conformance-c-asan build/schema_test_c_fuzz build/schema_test_c_soak build/schema_test_c_variable build/schema_test_c_variable_asan
	$(MAKE) tables-c-wire-fuzz SEED=1 N=20000
	$(MAKE) tables-c-zero-cost
	$(MAKE) tables-c-json-walk
	$(MAKE) tables-c-fuzz N=25000
	$(MAKE) tables-c-fuzz-negative-control
	$(MAKE) tables-c-variable
	$(MAKE) tables-c-keyed-none-refusal-ndebug
	$(MAKE) tables-c-keyed-none-refusal-negative-control
	$(MAKE) tables-c-keyed-max-refusal-ndebug
	$(MAKE) tables-c-keyed-max-refusal-negative-control
	$(MAKE) tables-c-soak SOAK_SECONDS=2
	$(MAKE) tables-c-soak-negative-control
	$(MAKE) tables-c-ref-ordinal-negative-control
	$(MAKE) tables-c-fixedform
	$(MAKE) tables-c-versioning
	# and the whole matrix again under ASan + UBSan: the sanitized run is the
	# strongest gate this leg has, and a gate that only fires under a target
	# nobody types is not in the chain.
	./build/conformance-harness run --drivers test/conformance/c/drivers-asan.txt --work build/conformance-c-asan-work
	$(MAKE) conformance-negative-control-c
	$(MAKE) conformance-negative-control-c-foreign
	cd test/c && ../../build/schema_test_c
	cd test/c-ludicrous && ../../build/schema_test_c_ludicrous
	./build/schema_test_bench_c

TEST_LEGS         += test-c
CONFORMANCE_LEGS  += build/conformance-c
BENCH_TABLES_LEGS += generated/bench/tables/c/.stamp
GOLDENS_LEGS      += update-goldens-c

# The file-wire differential: the compiler's independent engine owns every
# expected byte and report. Unsupported roster entries are named absent.
build/wire-fuzz-c: build/tables-generated-c/.stamp test/c-tables/wire_fuzz_main.c $(wildcard test/conformance/c/*.h) $(wildcard test/conformance/c/*.c)
	$(CC) $(TABLES_CFLAGS) $(C_CONFORMANCE_INCLUDES) test/c-tables/wire_fuzz_main.c $(filter-out test/conformance/c/main.c,$(C_CONFORMANCE_SOURCES)) -o $@ -lm

build/wire-fuzz-c-asan: build/tables-generated-c/.stamp test/c-tables/wire_fuzz_main.c $(wildcard test/conformance/c/*.h) $(wildcard test/conformance/c/*.c)
	$(CC) $(TABLES_CFLAGS_CONTROL) $(C_SANITIZE) $(C_CONFORMANCE_INCLUDES) test/c-tables/wire_fuzz_main.c $(filter-out test/conformance/c/main.c,$(C_CONFORMANCE_SOURCES)) -o $@ -lm

.PHONY: tables-c-wire-fuzz
tables-c-wire-fuzz: build/conformance-harness build/wire-fuzz-c build/wire-fuzz-c-asan
	./build/conformance-harness wire-fuzz --driver ./build/wire-fuzz-c --seed $(SEED) --n $(N)
	./build/conformance-harness wire-fuzz --driver ./build/wire-fuzz-c-asan --seed $(SEED) --n $(N) --failed build/wire-fuzz/failed-c-asan.bin

# Wide text on the packet wire, using the shared group's corpus.
build/packet-wide/c/.stamp: bin/schema build/packet-wide/source/WideText.schema
	./bin/schema generate --lang c --out build/packet-wide/c build/packet-wide/source/WideText.schema
	@touch $@

build/packet-wide/c-shapes/.stamp: bin/schema test/packet-wide/Shapes.schema
	./bin/schema generate --lang c --out build/packet-wide/c-shapes test/packet-wide/Shapes.schema
	@touch $@

.PHONY: packet-wide-c
packet-wide-c: build/packet-wide/c/.stamp build/packet-wide/c-shapes/.stamp build/packet-wide/cpp/driver build/packet-text/harness
	$(CC) -std=c99 -Wall -Wextra -Werror -I$(SERIALIZE_C) -Ibuild/packet-wide/c test/packet-wide/driver.c $(SERIALIZE_C)/serialize.c -lm -o build/packet-wide/c/debug
	./build/packet-text/harness -wide -corpus testdata/conformance/text/wstring.txt -oracle build/packet-wide/cpp/driver ./build/packet-wide/c/debug
	$(CC) -std=c99 -Wall -Wextra -Werror -O2 -DNDEBUG -I$(SERIALIZE_C) -Ibuild/packet-wide/c test/packet-wide/driver.c $(SERIALIZE_C)/serialize.c -lm -o build/packet-wide/c/release
	./build/packet-text/harness -wide -corpus testdata/conformance/text/wstring.txt -oracle build/packet-wide/cpp/driver ./build/packet-wide/c/release
	@for mode in '' '-DNDEBUG'; do \
		$(CC) -std=c99 -Wall -Wextra -Werror $$mode -fsanitize=address,undefined -I$(SERIALIZE_C) -Ibuild/packet-wide/c -Ibuild/packet-wide/c-shapes test/packet-wide/c_contract.c $(SERIALIZE_C)/serialize.c -lm -o build/packet-wide/c/contract || exit 1; \
		./build/packet-wide/c/contract || exit 1; \
	done

.PHONY: packet-wide-c-negative-control
packet-wide-c-negative-control: packet-wide-c
	@mkdir -p build/packet-wide/c-negative
	go run ./tools/sabotage -name packet-wide-c-pairing -out build/packet-wide/c-negative/wstring.gotext internal/codegen/c/wstring.go
	@printf '{"Replace":{"%s/internal/codegen/c/wstring.go":"%s/build/packet-wide/c-negative/wstring.gotext"}}\n' "$(CURDIR)" "$(CURDIR)" > build/packet-wide/c-negative/overlay.json
	go run -overlay=build/packet-wide/c-negative/overlay.json ./cmd/schema generate --lang c --out build/packet-wide/c-negative build/packet-wide/source/WideText.schema
	$(CC) -std=c99 -Wall -Wextra -Werror -O2 -DNDEBUG -I$(SERIALIZE_C) -Ibuild/packet-wide/c-negative test/packet-wide/driver.c $(SERIALIZE_C)/serialize.c -lm -o build/packet-wide/c-negative/driver
	@if ./build/packet-text/harness -wide -mutations-only -corpus testdata/conformance/text/wstring.txt -oracle build/packet-wide/cpp/driver ./build/packet-wide/c-negative/driver > build/packet-wide/c-negative/log 2>&1; then echo 'NEGATIVE CONTROL FAILED: C wide pairing removal passed'; exit 1; fi
	@grep -Fq 'FAILED: packet-text verdict on ' build/packet-wide/c-negative/log || { cat build/packet-wide/c-negative/log; exit 1; }
	@echo 'packet wide C negative control: removed pairing fails bit-flip agreement'

test-c: packet-wide-c packet-wide-c-negative-control

# Show that the independent differential detects a permissive LEB128 reader.
# Only generated scratch headers change; no working source is sabotaged.
.PHONY: tables-c-wire-fuzz-negative-control
tables-c-wire-fuzz-negative-control: build/conformance-harness build/tables-generated-c/.stamp
	@rm -rf build/c-wire-sabotage && mkdir -p build/c-wire-sabotage
	@cp -R build/tables-generated-c build/c-wire-sabotage/generated
	@for f in build/c-wire-sabotage/generated/*/*Table.h; do \
		sed 's/if ( i \&\& b == 0 )/if ( 0 ) \/* SABOTAGED canonical LEB128 *\//' "$$f" > "$$f.tmp" && mv "$$f.tmp" "$$f" || exit 1; \
	done
	@grep -q 'SABOTAGED canonical LEB128' build/c-wire-sabotage/generated/examples/TablesTable.h || { echo 'NEGATIVE CONTROL: canonical LEB128 sabotage did not apply'; exit 1; }
	$(CC) $(TABLES_CFLAGS_CONTROL) $(subst build/tables-generated-c,build/c-wire-sabotage/generated,$(C_CONFORMANCE_INCLUDES)) \
		test/c-tables/wire_fuzz_main.c $(subst build/tables-generated-c,build/c-wire-sabotage/generated,$(filter-out test/conformance/c/main.c,$(C_CONFORMANCE_SOURCES))) -o build/c-wire-sabotage/driver -lm
	@if ./build/conformance-harness wire-fuzz --driver ./build/c-wire-sabotage/driver --seed 1 --n 0 --failed build/c-wire-sabotage/failed.bin > build/c-wire-sabotage/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: noncanonical LEB128 was accepted and the differential stayed green'; exit 1; \
	fi
	@grep -q 'the report differs' build/c-wire-sabotage/log || { cat build/c-wire-sabotage/log; exit 1; }
	@echo 'C wire negative control: accepting noncanonical LEB128 changes the read report'

test-c: tables-c-wire-fuzz-negative-control

# Maps and lists use the C++ reference's pinned file bytes and exact region
# sizes. Both allocator-backed construction and caller-owned loads are checked
# through lock, save, and JSON under native execution and ASan/UBSan.
build/tables-generated-c/collections.stamp: bin/schema make/c.mk $(wildcard tables/maps/*.schema) $(wildcard tables/lists/*.schema) $(wildcard tables/arms/*.schema)
	./bin/schema generate --lang c --out build/tables-generated-c/maps tables/maps
	./bin/schema generate --lang c --out build/tables-generated-c/lists tables/lists
	./bin/schema generate --lang c --out build/tables-generated-c/arms tables/arms
	@touch $@

build/c-collections-maps: build/tables-generated-c/collections.stamp test/c-tables/collections_maps.c test/c-tables/collections.h
	$(CC) $(TABLES_CFLAGS) -Ibuild/tables-generated-c/maps test/c-tables/collections_maps.c build/tables-generated-c/maps/*Table.c -o $@ -lm

build/c-collections-lists: build/tables-generated-c/collections.stamp test/c-tables/collections_lists.c test/c-tables/collections.h
	$(CC) $(TABLES_CFLAGS) -Ibuild/tables-generated-c/lists test/c-tables/collections_lists.c build/tables-generated-c/lists/*Table.c -o $@ -lm

build/c-collections-maps-asan: build/tables-generated-c/collections.stamp test/c-tables/collections_maps.c test/c-tables/collections.h
	$(CC) $(TABLES_CFLAGS_CONTROL) $(C_SANITIZE) -Ibuild/tables-generated-c/maps test/c-tables/collections_maps.c build/tables-generated-c/maps/*Table.c -o $@ -lm

build/c-collections-lists-asan: build/tables-generated-c/collections.stamp test/c-tables/collections_lists.c test/c-tables/collections.h
	$(CC) $(TABLES_CFLAGS_CONTROL) $(C_SANITIZE) -Ibuild/tables-generated-c/lists test/c-tables/collections_lists.c build/tables-generated-c/lists/*Table.c -o $@ -lm

.PHONY: tables-c-collections
tables-c-collections: build/c-collections-maps build/c-collections-lists build/c-collections-maps-asan build/c-collections-lists-asan
	./build/c-collections-maps
	./build/c-collections-lists
	./build/c-collections-maps-asan
	./build/c-collections-lists-asan

test-c tables-c: tables-c-collections

build/collections-cpp/.stamp: bin/schema make/c.mk $(wildcard tables/maps/*.schema) $(wildcard tables/lists/*.schema) $(wildcard tables/arms/*.schema)
	./bin/schema generate --lang cpp --out build/collections-cpp/maps tables/maps
	./bin/schema generate --lang cpp --out build/collections-cpp/lists tables/lists
	./bin/schema generate --lang cpp --out build/collections-cpp/arms tables/arms
	@touch $@

build/c-collections-fuzz: build/tables-generated-c/collections.stamp test/c-tables/collections_fuzz_maps.c test/c-tables/collections_fuzz_lists.c test/c-tables/collections_fuzz_arms.c test/c-tables/wire_fuzz_main.c $(wildcard test/conformance/c/*.h)
	$(CC) $(TABLES_CFLAGS) -DSCHEMA_C_COLLECTIONS_FUZZ -Itest/conformance/c -Ibuild/tables-generated-c test/c-tables/wire_fuzz_main.c test/c-tables/collections_fuzz_maps.c test/c-tables/collections_fuzz_lists.c test/c-tables/collections_fuzz_arms.c build/tables-generated-c/maps/*Table.c build/tables-generated-c/lists/*Table.c build/tables-generated-c/arms/*Table.c -o $@ -lm

build/c-collections-fuzz-asan: build/tables-generated-c/collections.stamp test/c-tables/collections_fuzz_maps.c test/c-tables/collections_fuzz_lists.c test/c-tables/collections_fuzz_arms.c test/c-tables/wire_fuzz_main.c $(wildcard test/conformance/c/*.h)
	$(CC) $(TABLES_CFLAGS_CONTROL) $(C_SANITIZE) -DSCHEMA_C_COLLECTIONS_FUZZ -Itest/conformance/c -Ibuild/tables-generated-c test/c-tables/wire_fuzz_main.c test/c-tables/collections_fuzz_maps.c test/c-tables/collections_fuzz_lists.c test/c-tables/collections_fuzz_arms.c build/tables-generated-c/maps/*Table.c build/tables-generated-c/lists/*Table.c build/tables-generated-c/arms/*Table.c -o $@ -lm

build/cpp-collections-fuzz: build/collections-cpp/.stamp test/c-tables/collections_fuzz.cpp test/c-tables/wire_fuzz_main.c test/conformance/c/driver.h
	$(CXX) -std=c++17 -Wall -Wextra -Werror -O2 -Itest/conformance/c -Ibuild/collections-cpp test/c-tables/collections_fuzz.cpp -o $@

.PHONY: tables-c-collections-fuzz
tables-c-collections-fuzz: build/c-collections-fuzz build/c-collections-fuzz-asan build/cpp-collections-fuzz
	SCHEMA_C_COLLECTIONS_DRIVER=./build/c-collections-fuzz SCHEMA_CPP_COLLECTIONS_DRIVER=./build/cpp-collections-fuzz go test ./test/conformance/harness -run '^TestCCollection(Cook)?Differential$$' -count=1 -v
	SCHEMA_C_COLLECTIONS_DRIVER=./build/c-collections-fuzz-asan SCHEMA_CPP_COLLECTIONS_DRIVER=./build/cpp-collections-fuzz go test ./test/conformance/harness -run '^TestCCollection(Cook)?Differential$$' -count=1 -v

test-c tables-c: tables-c-collections-fuzz

# A wrong compile-time field slot must turn the independently encoded mixed
# message batch red. The generated writer is sabotaged only through an overlay.
.PHONY: tables-c-message-negative-control
tables-c-message-negative-control:
	@mkdir -p build/c-message-negative
	go run ./tools/sabotage -name message-c-wrong-slot -out build/c-message-negative/message_save.gotext internal/codegen/ctable/message_save.go
	@printf '{"Replace":{"%s/internal/codegen/ctable/message_save.go":"%s/build/c-message-negative/message_save.gotext"}}\n' "$(CURDIR)" "$(CURDIR)" > build/c-message-negative/overlay.json
	@if SCHEMA_SLOW=1 go test -v -count=1 -overlay=build/c-message-negative/overlay.json ./compiler -run '^TestCTableMessageSave$$' > build/c-message-negative/log 2>&1; then \
		grep -q -- '--- PASS' build/c-message-negative/log || \
			{ echo 'NEGATIVE CONTROL FAILED: the wire comparison did not run (skipped), so this control is watching nothing'; \
			  cat build/c-message-negative/log; exit 1; }; \
		echo 'NEGATIVE CONTROL FAILED: the message slot changed without failing the wire comparison'; exit 1; \
	fi
	@grep -q -- '--- FAIL: TestCTableMessageSave' build/c-message-negative/log
	@grep -q 'memcmp(output,expected,sizeof(expected))' build/c-message-negative/log
	@echo 'negative control: the wrong C message slot turns the independent wire comparison red'

test-c tables-c: tables-c-message-negative-control

.PHONY: tables-c-retain tables-c-retain-negative-control
tables-c-retain: build/conformance-harness build/wire-fuzz-c build/wire-fuzz-c-asan
	go test ./compiler -run '^TestCTableRetain' -count=1
	./build/conformance-harness wire-fuzz --driver ./build/wire-fuzz-c --retain --seed $(SEED) --n $(N) --failed build/wire-fuzz/failed-c-retain.bin
	./build/conformance-harness wire-fuzz --driver ./build/wire-fuzz-c-asan --retain --seed $(SEED) --n $(N) --failed build/wire-fuzz/failed-c-retain-asan.bin

tables-c-retain-negative-control:
	@mkdir -p build/c-retain-negative
	go run ./tools/sabotage -name retain-c-drop-field -out build/c-retain-negative/retain.gotext internal/codegen/ctable/retain.go
	@printf '{"Replace":{"%s/internal/codegen/ctable/retain.go":"%s/build/c-retain-negative/retain.gotext"}}\n' "$(CURDIR)" "$(CURDIR)" > build/c-retain-negative/overlay.json
	@if SCHEMA_SLOW=1 go test -v -count=1 -overlay=build/c-retain-negative/overlay.json ./compiler -run '^TestCTableRetainFile$$' > build/c-retain-negative/log 2>&1; then \
		grep -q -- '--- PASS' build/c-retain-negative/log || \
			{ echo 'NEGATIVE CONTROL FAILED: the public round-trip report did not run (skipped), so this control is watching nothing'; \
			  cat build/c-retain-negative/log; exit 1; }; \
		echo 'NEGATIVE CONTROL FAILED: silently dropped C retained field passed'; exit 1; fi
	@grep -q -- '--- FAIL: TestCTableRetainFile' build/c-retain-negative/log
	@grep -q -- 'report.retained==13' build/c-retain-negative/log
	@echo 'C retention negative control: dropped field fails the public round-trip report'

test-c tables-c: tables-c-retain tables-c-retain-negative-control

# Collection adapters belong to the common roster as well as their direct differential.
build/conformance-c build/conformance-c-asan build/wire-fuzz-c build/wire-fuzz-c-asan: test/c-tables/collections_fuzz_maps.c test/c-tables/collections_fuzz_lists.c test/c-tables/collections_fuzz_arms.c

# THE RUN COPY'S BOUND ON THE C LEG (docs/SPEC-TABLES.md §3.4,
# test/tables/fixedform_runcopy.c). The driver above is the versioning
# conformance set, and it checks the VALUES a read produces. The copy primitive
# under it has one invariant those cases cannot state — every byte a copy of n
# bytes reads or writes is inside [ start, start + n ) — because a move
# anchored outside the run still copies the run correctly and merely takes a
# neighbour with it. Exact-size heap blocks over every length from 0 to 96 make
# the sanitized twin the assertion, and the same file runs on the C++ leg.
#
# ITS OWN GENERATED DIRECTORY, for the reason every C table unit has one: two
# emitters write <Base>Table.h and one directory would have them overwrite each
# other. One schema is enough — the runtime is package-scoped and identical in
# every unit this emitter writes.
build/runcopy-c/.stamp: bin/schema tables/scalars/Scalars.schema
	@mkdir -p build/runcopy-c
	./bin/schema generate --lang c --out build/runcopy-c tables/scalars
	@touch $@

build/schema_test_c_runcopy: build/runcopy-c/.stamp test/tables/fixedform_runcopy.c
	@mkdir -p build
	$(CC) $(TABLES_CFLAGS) -Ibuild/runcopy-c -I$(SERIALIZE_C) \
		test/tables/fixedform_runcopy.c -o $@ -lm

build/schema_test_c_runcopy_asan: build/runcopy-c/.stamp test/tables/fixedform_runcopy.c
	@mkdir -p build
	$(CC) $(TABLES_CFLAGS_CONTROL) $(C_SANITIZE) -Ibuild/runcopy-c -I$(SERIALIZE_C) \
		test/tables/fixedform_runcopy.c -o $@ -lm

.PHONY: tables-c-runcopy
tables-c-runcopy: build/schema_test_c_runcopy build/schema_test_c_runcopy_asan
	./build/schema_test_c_runcopy
	./build/schema_test_c_runcopy_asan

# It rides the leg's own fixed-form target rather than a target of its own that
# somebody has to remember: one name for "check the C fixed form".
tables-c-fixedform: tables-c-runcopy
