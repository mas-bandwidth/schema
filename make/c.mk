# make/c.mk — the C leg (docs/CONTRIBUTING.md, "Adding a language"). Included
# by the Makefile's wildcard include; the Makefile names no language. The leg
# registers itself at the end of this file.

# the serialize.c runtime the generated C targets, a sibling checkout
SERIALIZE_C ?= ../serialize.c

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
build/tables-generated-c/.stamp: bin/schema $(SCHEMAS_TABLES) $(SCHEMAS_TABLES_POINTERS) $(SCHEMAS_TABLES_BLOCK) test/tables/V1.schema test/tables/V2.schema test/tables/P1.schema test/tables/P2.schema test/tables/P3.schema test/tables/JsonKeys.schema
	@mkdir -p build/tables-generated-c
	./bin/schema generate --lang c --out build/tables-generated-c/examples tables/examples
	./bin/schema generate --lang c --out build/tables-generated-c/pointers tables/pointers
	./bin/schema generate --lang c --out build/tables-generated-c/block tables/block
	./bin/schema generate --lang c --out build/tables-generated-c/blockhome tables/blockhome
	./bin/schema generate --lang c --out build/tables-generated-c/v1 test/tables/V1.schema
	./bin/schema generate --lang c --out build/tables-generated-c/v2 test/tables/V2.schema
	./bin/schema generate --lang c --out build/tables-generated-c/p1 test/tables/P1.schema
	./bin/schema generate --lang c --out build/tables-generated-c/p2 test/tables/P2.schema
	./bin/schema generate --lang c --out build/tables-generated-c/p3 test/tables/P3.schema
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

C_CONFORMANCE_SOURCES = test/conformance/c/main.c \
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
C_CONFORMANCE_INCLUDES := -Itest/conformance/c -Ibuild/tables-generated-c/examples \
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
.PHONY: tables-c-zero-cost
tables-c-zero-cost: build/tables-generated-c/.stamp
	@for f in build/tables-generated-c/examples/*Table.h build/tables-generated-c/v1/*Table.h \
	          build/tables-generated-c/v2/*Table.h build/tables-generated-c/p1/*Table.h \
	          build/tables-generated-c/p3/*Table.h; do \
		if grep -nE "TableArena|TableWorker|TableRef|TableSink|TableCtx|TableRegionSink|kTableSegment|kTableSlab|kTableMaxDepth|is_pointer|Builder|PackMeasure|LoadMeasure|stdatomic" $$f; then \
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

# THE SOAK IS DORMANT while this port writes the wire's previous form (schema
# #512), on the same rule as the big-endian leg below: the soak refuses to run
# at all until the codec re-saves every case in testdata/wire/tables to its own
# bytes, and those bytes are the id-table form. The binary above is what the
# leg wakes with. What is absent is the corpus it holds itself to.
.PHONY: tables-c-soak
tables-c-soak:
	@echo "tables-c-soak: dormant — the corpus it gates against is absent while this port writes the wire's previous form (docs/SPEC-TABLES.md §3, schema#512)"

.PHONY: tables-c-fuzz
tables-c-fuzz: build/schema_test_c_fuzz
	SEED=$(SEED) N=$(N) ./build/schema_test_c_fuzz

# THE C TABLES LEG, whole. Everything above, plus the conformance driver under
# the sanitizers over every surface it answers.
.PHONY: tables-c
tables-c: build/conformance-c build/conformance-c-asan tables-c-zero-cost tables-c-json-walk tables-c-fuzz tables-c-fuzz-negative-control tables-c-variable
	./build/conformance-harness run --drivers test/conformance/c/drivers-asan.txt --work build/conformance-c-asan-work
	$(MAKE) tables-js-leg
	$(MAKE) tables-js-accessor-negative-control
	$(MAKE) tables-js-fuzz
	$(MAKE) tables-c-keyed-none-refusal-ndebug
	$(MAKE) tables-c-keyed-none-refusal-negative-control
	$(MAKE) tables-c-soak SOAK_SECONDS=20
	$(MAKE) tables-c-soak-negative-control

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
.PHONY: conformance-negative-control-c
conformance-negative-control-c:
	@echo "conformance-negative-control-c: dormant — the surface it turns red is absent while this port writes the wire's previous form (docs/SPEC-TABLES.md §3, schema#512)"

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
	@printf '#!/bin/sh\nexec %s/driver-bin "$$@"\n' "$(CURDIR)/$(CONFORMANCE_NEGATIVE_C_FOREIGN)" > $(CONFORMANCE_NEGATIVE_C_FOREIGN)/driver
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
# THE LEG IS DORMANT while this port writes the wire's previous form (schema
# #512): the goldens under testdata/wire/tables are the id-table form, and a
# codec that cannot reproduce them on a LITTLE-endian host cannot be asked what
# it does on a big-endian one. The binary below is what the leg wakes with, and
# it still cross-compiles; what is absent is the corpus it would gate against.
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
tables-c-big-endian:
	@echo "tables-c-big-endian: dormant — the corpus it gates against is absent while this port writes the wire's previous form (docs/SPEC-TABLES.md §3, schema#512)"

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
	@sed 's|        abort();|        /* SABOTAGED: the abort is gone */ (void) 0;|' \
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
	@sed -i.bak -e 's|if ( rows > (uint64_t) bytes - offset_of ) { return 0; }|/* SABOTAGED */|' \
	            -e 's|if ( padding > bytes - used ) { return 0; }|/* SABOTAGED */|' \
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
tables-c-soak-negative-control:
	@echo "tables-c-soak-negative-control: dormant — the surface it turns red is absent while this port writes the wire's previous form (docs/SPEC-TABLES.md §3, schema#512)"

# The C half of `make update-goldens`: the committed generated table sources
# (testdata/golden/tables/*-c).
.PHONY: update-goldens-c
update-goldens-c: build/tables-generated-c/.stamp
	@for d in examples block pointers; do \
		mkdir -p testdata/golden/tables/$$d-c; \
		cp build/tables-generated-c/$$d/*Table.h build/tables-generated-c/$$d/*Table.c testdata/golden/tables/$$d-c/ 2>/dev/null || true; \
	done

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
.PHONY: test-c
test-c: build/schema_test_c build/schema_test_c_ludicrous build/schema_test_bench_c build/conformance-harness build/conformance-c build/conformance-c-asan build/schema_test_c_fuzz build/schema_test_c_soak build/schema_test_c_variable build/schema_test_c_variable_asan
	$(MAKE) tables-c-zero-cost
	$(MAKE) tables-c-json-walk
	$(MAKE) tables-c-fuzz N=25000
	$(MAKE) tables-c-fuzz-negative-control
	$(MAKE) tables-c-variable
	$(MAKE) tables-c-keyed-none-refusal-ndebug
	$(MAKE) tables-c-keyed-none-refusal-negative-control
	$(MAKE) tables-c-soak SOAK_SECONDS=2
	$(MAKE) tables-c-soak-negative-control
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
