# schema — `make` builds the compiler (bin/schema) and nothing else: no
# serialize checkouts, no language toolchains, no generation. The full
# nine-language conformance chain is `make test`, and it needs the sibling
# runtime checkouts and toolchains documented below.

CXX      ?= c++
CXXFLAGS ?= -std=c++17 -Wall -Wextra -Werror -ffp-contract=off

# the classic serialize runtime the generated C++ targets (header-only), a
# sibling checkout. Every other language's runtime checkout and toolchain pin
# is that language's own: make/<lang>.mk (docs/CONTRIBUTING.md, "Adding a
# language").
SERIALIZE ?= ../serialize
CXXFLAGS  += -I$(SERIALIZE)

# cmd, internal, AND the public API packages: ir/ and compiler/ are compiled
# into bin/schema like any other source, and leaving them out made an edit to
# the layout model or the build version silently NOT rebuild the compiler every
# gate below then measures.
GO_SOURCES   := $(shell find cmd internal ir compiler -name '*.go') go.mod
SCHEMAS      := $(wildcard examples/*.schema)
SCHEMAS128   := $(wildcard examples128/*.schema)
SCHEMAS_BENCH := $(wildcard bench/corpus/*.schema)
SCHEMAS_TABLES := $(wildcard tables/examples/*.schema)
SCHEMAS_TABLES_POINTERS := $(wildcard tables/pointers/*.schema)
SCHEMAS_TABLES_BLOCK := $(wildcard tables/block/*.schema) $(wildcard tables/blockhome/*.schema)
# the MESSAGE corpora (docs/SPEC-TABLES.md §2.6): a union whose arms are tables,
# fixed in tables/messages and with a variable arm in tables/stream
SCHEMAS_TABLES_MESSAGES := $(wildcard tables/messages/*.schema) $(wildcard tables/stream/*.schema)
# the BYTE BUFFER corpus (docs/SPEC-TABLES.md §2.5): a blob at its used size,
# pointed at — C++ and the tool carry it, every port refuses the unit by name
SCHEMAS_TABLES_BLOBS := $(wildcard tables/blobs/*.schema)
SCHEMAS_TABLES_SCALARS := $(wildcard tables/scalars/*.schema)
# the MAP corpus (docs/SPEC-TABLES.md §2.8)
SCHEMAS_TABLES_MAPS := $(wildcard tables/maps/*.schema)

# The soak length every leg's soak takes, in seconds. The hour is the release
# act — `make tables-<lang>-soak SOAK_SECONDS=3600` — and `make test` runs the
# short forms with an explicit value.
SOAK_SECONDS ?= 3600

all: bin/schema

# The version the built binary reports. `git describe` gives the exact tag on a
# release build and tag-commits-hash elsewhere; when this is not a git checkout
# (a source tarball, say) the stamp is left empty and internal/version falls
# back to the toolchain's own build info.
VERSION ?= $(shell git describe --tags --dirty 2>/dev/null)
LDFLAGS := $(if $(VERSION),-X github.com/mas-bandwidth/schema/v2/internal/version.version=$(VERSION),)

bin/schema: $(GO_SOURCES)
	go build -ldflags "$(LDFLAGS)" -o $@ ./cmd/schema

# one generate run emits every header; the stamp carries the dependency
generated/cpp/.stamp: bin/schema $(SCHEMAS)
	./bin/schema generate --lang cpp --out generated/cpp examples
	@touch $@

# the fixed-point + 128-bit unit (examples128/) — all eight targets since the
# serialize ports carry the phase-1 surface; each generated unit gets the same
# module/manifest wiring as its main-corpus twin
generated/cpp/ludicrous/.stamp: bin/schema $(SCHEMAS128)
	./bin/schema generate --lang cpp --out generated/cpp/ludicrous examples128
	@touch $@

build/schema_test: generated/cpp/.stamp test/main.cpp test/second.cpp
	@mkdir -p build
	$(CXX) $(CXXFLAGS) -Igenerated/cpp -Itest test/main.cpp test/second.cpp -o $@

# The two-unit guard regression (issue #189): two packages, both with
# string(N) fields, included into ONE translation unit. Its corpus is
# test-only — generated at build time into build/, never part of the
# committed generated/ tree (one package per generate call, SPEC §3.2).
build/guard-generated/.stamp: bin/schema test/guard/Alpha.schema test/guard/Beta.schema
	@mkdir -p build/guard-generated
	./bin/schema generate --lang cpp --out build/guard-generated test/guard/Alpha.schema
	./bin/schema generate --lang cpp --out build/guard-generated test/guard/Beta.schema
	@touch $@

build/schema_test_guard: build/guard-generated/.stamp test/guard/main.cpp
	@mkdir -p build
	$(CXX) $(CXXFLAGS) -Ibuild/guard-generated test/guard/main.cpp -o $@

# The tables corpus (docs/SPEC-TABLES.md): the tabledemo unit plus the
# two-generation evolution pair (tblv1/tblv2), generated at build time into
# build/ — test-only, never part of the committed generated/ tree.
#
# The generate step and the include path are parameterised by generator binary
# and output root, because the big-endian negative control below regenerates
# the WHOLE corpus from a sabotaged emitter: a second copy of these lists would
# be a second corpus, and the gate would stop covering what the leg covers.
define tables_generate
	$(1) generate --lang cpp --out $(2)/examples tables/examples
	$(1) generate --lang cpp --out $(2)/pointers tables/pointers
	$(1) generate --lang cpp --out $(2)/block tables/block
	$(1) generate --lang cpp --out $(2)/blockhome tables/blockhome
	$(1) generate --lang cpp --out $(2)/messages tables/messages
	$(1) generate --lang cpp --out $(2)/stream tables/stream
	$(1) generate --lang cpp --out $(2)/blobs tables/blobs
	$(1) generate --lang cpp --out $(2)/v1 test/tables/V1.schema
	$(1) generate --lang cpp --out $(2)/v2 test/tables/V2.schema
	$(1) generate --lang cpp --out $(2)/p1 test/tables/P1.schema
	$(1) generate --lang cpp --out $(2)/p2 test/tables/P2.schema
	$(1) generate --lang cpp --out $(2)/p3 test/tables/P3.schema
	$(1) generate --lang cpp --out $(2)/jsonkeys test/tables/JsonKeys.schema
	$(1) generate --lang cpp --out $(2)/m1 test/tables/M1.schema
	$(1) generate --lang cpp --out $(2)/m2 test/tables/M2.schema
	$(1) generate --lang cpp --out $(2)/a1 test/tables/A1.schema
	$(1) generate --lang cpp --out $(2)/g1 test/tables/G1.schema
	$(1) generate --lang cpp --out $(2)/k1 test/tables/K1.schema
	$(1) generate --lang cpp --out $(2)/k2 test/tables/K2.schema
	$(1) generate --lang cpp --out $(2)/a2 test/tables/A2.schema
	$(1) generate --lang cpp --out $(2)/scalars tables/scalars
	$(1) generate --lang cpp --out $(2)/maps tables/maps
	$(1) generate --lang cpp --out $(2)/scalars2 test/tables/Scalars2.schema
endef

tables_includes = -I$(1)/examples -I$(1)/pointers -I$(1)/block -I$(1)/blockhome -Itest/tables \
	-I$(1)/v1 -I$(1)/v2 -I$(1)/p1 -I$(1)/p2 -I$(1)/p3 -I$(1)/jsonkeys \
	-I$(1)/messages -I$(1)/stream -I$(1)/blobs -I$(1)/m1 -I$(1)/m2 -I$(1)/a1 -I$(1)/a2 -I$(1)/g1 -I$(1)/k1 -I$(1)/k2 -I$(1)/scalars -I$(1)/scalars2 -I$(1)/maps -I$(SERIALIZE)

build/tables-generated/.stamp: bin/schema $(SCHEMAS_TABLES) $(SCHEMAS_TABLES_POINTERS) $(SCHEMAS_TABLES_BLOCK) $(SCHEMAS_TABLES_MESSAGES) $(SCHEMAS_TABLES_BLOBS) $(SCHEMAS_TABLES_SCALARS) $(SCHEMAS_TABLES_MAPS) test/tables/V1.schema test/tables/V2.schema test/tables/P1.schema test/tables/P2.schema test/tables/P3.schema test/tables/JsonKeys.schema test/tables/M1.schema test/tables/M2.schema test/tables/A1.schema test/tables/A2.schema test/tables/G1.schema test/tables/K1.schema test/tables/K2.schema test/tables/Scalars2.schema
	@mkdir -p build/tables-generated
	$(call tables_generate,./bin/schema,build/tables-generated)
	@touch $@

# The ZERO-COST GATE (docs/SPEC-TABLES.md): a table with no pointer in its by-value
# closure must pay NOTHING for the pointer machinery — no builder, no arena, no
# handles, no lifecycle surface, no extra descriptor columns. The pointer-free
# corpus's generated headers must not contain one symbol of it. (The stronger
# one-time proof — byte-identical emission against the pre-pointer baseline —
# is recorded in the round log; this is the standing gate.)
#
# THE MAP MACHINERY TAKES THE SAME GATE (docs/SPEC-TABLES.md §2.2, §2.8): "not
# one symbol of the map machinery in a map-free unit's generated headers, held
# by the zero-cost gate's header scan — with the map symbols added to its
# list." A map makes its holder variable-length, so every unit scanned here is
# map-free by construction, and the map symbols below say so mechanically
# rather than by inspection.
#
# THE SCAN IS BY SYMBOL, not by line, and `TableNode` is matched with its whole
# spelling so a node symbol nobody has written yet is still refused. Exactly one
# of those spellings is ALLOWED in a pointer-free unit and it is named below.
TABLES_ZERO_COST_SYMBOLS := TableArena|TableSlot|TableWorker|TableRef|TableRegion|kTableSegment|kTableSlab|TablePack|[A-Za-z_]*TableNode[A-Za-z_]*|is_pointer|Builder|PackMeasure|LoadMeasure|TableBlob|TableBytesView|TableStringView|AllocBytes|AllocString|BytesEmplace|StringEmplace|TableMap|TableMapHead|TableMapSegment|TableMapOrder|TableMapCursor|TableEntryKey|TableKeyOrder|TableResetMapValue|TableEntrySetKey|kTableMapSegment

# THE ONE NODE SPELLING A POINTER-FREE UNIT CARRIES, and it is the FORM's and
# not the pointer machinery's (docs/SPEC-TABLES.md §3, §3.1): the reserved
# node-table id. Every reader of the id-table form owes §3.1's refusal of that
# id inside a NESTED body, whether or not its own closure has a pointer,
# because the body it is handed may have been written by a unit that does have
# one (§4). What it costs a pointer-free unit is one `static const uint64_t`
# and one comparison: no arena, no builder, no handle, no lifecycle surface and
# no extra descriptor column, which is the claim this gate holds. Every other
# TableNode spelling stays refused, and
# tables-zero-cost-negative-control shows that it does.
TABLES_ZERO_COST_ALLOWED := kTableNodeTableFieldId

TABLES_ZERO_COST_HEADERS := build/tables-generated/examples/*Table.h build/tables-generated/v1/*Table.h \
	build/tables-generated/v2/*Table.h build/tables-generated/p1/*Table.h \
	build/tables-generated/p3/*Table.h \
	build/tables-generated/messages/*Table.h build/tables-generated/m1/*Table.h \
	build/tables-generated/m2/*Table.h build/tables-generated/scalars/*Table.h

.PHONY: tables-zero-cost
tables-zero-cost: build/tables-generated/.stamp
	@for f in $(TABLES_ZERO_COST_HEADERS); do \
		if grep -ohE "$(TABLES_ZERO_COST_SYMBOLS)" $$f | grep -vxE "$(TABLES_ZERO_COST_ALLOWED)" | sort -u | grep -q .; then \
			grep -nE "$(TABLES_ZERO_COST_SYMBOLS)" $$f | grep -vE "$(TABLES_ZERO_COST_ALLOWED)"; \
			echo "ZERO-COST GATE FAILED: pointer or map machinery leaked into $$f"; exit 1; \
		fi; \
	done
	@echo "tables zero-cost gate: value-only tables carry no pointer or map machinery"

# THE NEGATIVE CONTROL. The gate above sanctions ONE node spelling, so it owes a
# demonstration that it still refuses the others: the nearest neighbour of the
# sanctioned constant is planted in a COPY of a scanned header, and the same
# scan must refuse it. Nothing tracked is written to.
.PHONY: tables-zero-cost-negative-control
tables-zero-cost-negative-control: build/tables-generated/.stamp
	@rm -rf build/zero-cost-control && mkdir -p build/zero-cost-control
	@cp build/tables-generated/examples/GuardedTable.h build/zero-cost-control/GuardedTable.h
	@printf 'static const TableNodeMap * kPlanted = NULL; /* PLANTED */\n' >> build/zero-cost-control/GuardedTable.h
	@if ! grep -ohE "$(TABLES_ZERO_COST_SYMBOLS)" build/zero-cost-control/GuardedTable.h \
			| grep -vxE "$(TABLES_ZERO_COST_ALLOWED)" | sort -u | grep -q .; then \
		echo "NEGATIVE CONTROL FAILED: a planted node symbol left the zero-cost scan green"; exit 1; \
	fi
	@grep -ohE "$(TABLES_ZERO_COST_SYMBOLS)" build/zero-cost-control/GuardedTable.h | grep -vxE "$(TABLES_ZERO_COST_ALLOWED)" | sort -u
	@echo "negative control: a planted node symbol turns the zero-cost scan RED, and the reserved id alone does not"

.PHONY: tables-json-walk
tables-json-walk: build/tables-generated/.stamp
	@rm -rf build/json-walk && mkdir -p build/json-walk
	@for f in build/tables-generated/*/*Table.cpp; do \
		out=build/json-walk/$$(echo $$f | tr / _); \
		awk '/---- json walk: begin ----/,/---- json walk: end ----/' $$f > $$out; \
		if [ ! -s $$out ]; then echo "GENERIC-WALK GATE FAILED: no walker in $$f"; exit 1; fi; \
	done
	@first=""; for f in build/json-walk/*; do \
		if [ -z "$$first" ]; then first=$$f; else \
			cmp -s $$first $$f || { echo "GENERIC-WALK GATE FAILED: the walker in $$f is not the walker in $$first"; exit 1; }; \
		fi; \
	done
	@echo "tables generic-walk gate: one walker, byte-identical in $$(ls build/json-walk | wc -l | tr -d ' ') generated .cpp files"

# THE GRAPH-WALK GATE (docs/SPEC-TABLES.md §16.7): the variable class's half of
# the text form is emitted only in a unit that declares a pointer, and it is ONE
# half too — the same bytes in every pointered .cpp of the corpus, on the walk's
# own terms — and none of it reaches a pointer-free unit, which is the zero-cost
# property (§2.2) holding for the text form.
.PHONY: tables-json-graph-walk
tables-json-graph-walk: build/tables-generated/.stamp
	@rm -rf build/json-graph-walk && mkdir -p build/json-graph-walk
	@for f in build/tables-generated/pointers/*Table.cpp build/tables-generated/p2/*Table.cpp build/tables-generated/blobs/*Table.cpp; do \
		out=build/json-graph-walk/$$(echo $$f | tr / _); \
		awk '/---- json graph walk: begin ----/,/---- json graph walk: end ----/' $$f > $$out; \
		if [ ! -s $$out ]; then echo "GRAPH-WALK GATE FAILED: no graph half in $$f"; exit 1; fi; \
	done
	@for f in build/tables-generated/examples/*Table.cpp build/tables-generated/v1/*Table.cpp build/tables-generated/p1/*Table.cpp; do \
		if grep -q -- '---- json graph walk: begin ----' $$f; then \
			echo "GRAPH-WALK GATE FAILED: the graph half leaked into the pointer-free $$f"; exit 1; fi; \
	done
	@first=""; for f in build/json-graph-walk/*; do \
		if [ -z "$$first" ]; then first=$$f; else \
			cmp -s $$first $$f || { echo "GRAPH-WALK GATE FAILED: the graph half in $$f is not the one in $$first"; exit 1; }; \
		fi; \
	done
	@echo "tables graph-walk gate: one graph half, byte-identical in $$(ls build/json-graph-walk | wc -l | tr -d ' ') pointered .cpp files"

# The NEGATIVE CONTROL for the walk (docs/SPEC-TABLES.md §16.5). A green round-trip
# suite proves nothing until the suite is shown capable of going red: the
# walker's field-offset arithmetic is sabotaged by one field width, and the
# round trip must FAIL on the first table with two fields. Attachment is that
# table — two four-byte fields at offsets 0 and 4 — so the sabotage swaps them
# and stays inside the struct.
.PHONY: tables-json-negative-control
tables-json-negative-control: bin/schema test/tables/json_negative_main.cpp
	@rm -rf build/json-sabotage && mkdir -p build/json-sabotage
	./bin/schema generate --lang cpp --out build/json-sabotage tables/examples
	@sed -i.bak 's|const uint8_t \* storage = (const uint8_t \*) base + f->offset;|const uint8_t * storage = (const uint8_t *) base + ( f->offset ^ 4 );|' build/json-sabotage/TablesTable.cpp
	@grep -q 'f->offset ^ 4' build/json-sabotage/TablesTable.cpp || { echo "NEGATIVE CONTROL: the sabotage did not apply"; exit 1; }
	@mkdir -p build
	$(CXX) -std=c++17 -Wall -Wextra -Werror -ffp-contract=off \
		-Ibuild/json-sabotage test/tables/json_negative_main.cpp build/json-sabotage/TablesTable.cpp -o build/schema_test_json_negative
	./build/schema_test_json_negative

# ---------------------------------------------------------------------------
# The BLOCK FORM (docs/SPEC-TABLES.md §19). Nothing declares it: every fixed table
# has one, emitted on the side into <Base>Block.h/.cpp and <Base>Block.cs, and
# a consumer compiles those only if it uses the form.
# ---------------------------------------------------------------------------

# -Wshadow, on all three tables legs (BLOCK here, TABLES and PACK below): the
# POSIX gate for a class the POSIX legs were blind to. A shadowed name in an
# emitted function is a warning nobody sees under -Wall -Wextra — neither gcc
# nor clang says a word without this flag — and cl refuses it outright at
# /W4 /WX, so the estate's Visual C++ requirement turned a silent POSIX green
# into a Windows red (#286).
#
# The two POSIX compilers cover DIFFERENT halves of what cl refuses, and the
# matrix runs both, so the pair is the gate rather than either one:
#   clang -Wshadow  -> a local hiding a local          (cl C4456)
#   gcc   -Wshadow  -> the same, PLUS a constructor parameter hiding a member
#                      (cl C4458), which clang files under -Wshadow-all
# macOS runs clang and ubuntu runs gcc; the big-endian leg is a second gcc.
BLOCK_CXXFLAGS := -std=c++17 -Wall -Wextra -Werror -Wshadow -ffp-contract=off -pthread
BLOCK_INCLUDES := -Ibuild/tables-generated/block
BLOCK_SOURCES = $$(ls build/tables-generated/block/*Block.cpp)

build/schema_test_block: build/tables-generated/.stamp test/tables/block_main.cpp
	@mkdir -p build
	$(CXX) $(BLOCK_CXXFLAGS) $(BLOCK_INCLUDES) test/tables/block_main.cpp $(BLOCK_SOURCES) -o $@

# The SANITIZED twin (#277's rule). The block form is where a multi-threaded
# fill over disjoint index ranges lives, and a race in that fill is precisely
# what a sanitizer leg exists to find — -Werror cannot see one.
# -fno-sanitize-recover=all is what makes it a GATE.
build/schema_test_block_asan: build/tables-generated/.stamp test/tables/block_main.cpp
	@mkdir -p build
	$(CXX) $(BLOCK_CXXFLAGS) -fsanitize=address,undefined -fno-sanitize-recover=all \
		-fno-omit-frame-pointer -g $(BLOCK_INCLUDES) test/tables/block_main.cpp $(BLOCK_SOURCES) -o $@

# The THREADSANITIZER twin. §19.5 says the multi-threaded fill runs "under the
# sanitizer leg, where a race in the fill is what the leg exists to find" — and
# ASan is not that leg: it finds no races, and byte-identity between a serial
# fill and a DETERMINISTIC four-worker fill finds none either. This is the leg
# that can. It costs under a second, so it rides beside the other two rather
# than waiting for a nightly.
build/schema_test_block_tsan: build/tables-generated/.stamp test/tables/block_main.cpp
	@mkdir -p build
	$(CXX) $(BLOCK_CXXFLAGS) -fsanitize=thread -fno-sanitize-recover=all -g \
		$(BLOCK_INCLUDES) test/tables/block_main.cpp $(BLOCK_SOURCES) -o $@

# Its NEGATIVE CONTROL, because a green TSan run otherwise proves only that the
# leg ran: --race has every worker write ONE row, after a start barrier, many
# times, and the leg must go red on the write-write race that creates.
#
# Overlapping RANGES would be a race too, and that is what this control did at
# first — but whether a sanitizer observes one then depends on how the machine
# happened to schedule four threads over seven thousand rows, and it passed on
# this bench and failed on CI's. Contending on one address from threads that
# start together is the same defect made certain, which is what a control has
# to be.
.PHONY: tables-block-race-negative-control
tables-block-race-negative-control: build/schema_test_block_tsan
	@if ./build/schema_test_block_tsan --race > build/block-race.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: ThreadSanitizer did not see overlapping workers writing one row"; exit 1; \
	fi
	@grep -q "ThreadSanitizer: data race" build/block-race.log || \
		{ echo "NEGATIVE CONTROL FAILED: the leg went red, but not on a data race"; tail -20 build/block-race.log; exit 1; }
	@echo "block race negative control: overlapping workers turn the ThreadSanitizer leg red"

# THE TWO-LANGUAGE GATE (docs/SPEC-TABLES.md §19.5, §12.1): a C++ producer writes a
# block and pins its bytes; a C# consumer opens those very bytes and compares
# every field of every row, twice — through the generated blittable struct and
# through the block descriptors. Sizes and offsets are asserted by GENERATED
# code on both sides, so the pair proves the two agree on the BYTES and not
# merely on the constants.
.PHONY: tables-block
tables-block: build/schema_test_block build/schema_test_block_asan build/schema_test_block_tsan build/tables-generated-cs/.stamp
	./build/schema_test_block
	./build/schema_test_block_asan
	./build/schema_test_block_tsan
	cd test/cs-block && dotnet run

# ---------------------------------------------------------------------------
# THE FORGERY FUZZER (docs/SPEC-TABLES.md §19.2, §19.5). The hand-written battery in
# block_main.cpp and Program.cs is eleven forgeries, one per fact BlockOpen
# checks plus the one this fuzzer found. This is the standing gate beside it:
# valid blocks from the generated builder, mutated, and one oracle over every
# mutant — REFUSE, or OPEN and be WHOLE.
#
# The mutators are seeded and reproducible: every mutant is a pure function of
# ( seed, unit, count vector, pass, index ), and a failing case prints the one
# command that re-runs it alone. Override for a long local run:
#
#   make tables-block-fuzz N=5000000 SEED=1234
#
# N is the RANDOM budget per unit, spread over its count vectors; the
# enumerated passes — every named slot x every width x every boundary value,
# every truncation length, every unaligned base — run whatever N is set to,
# because they are what actually cover the boundaries and they cost nothing.
# ---------------------------------------------------------------------------

# N is chosen against the measurement that matters, which is CI's and not this
# bench's: ubuntu-latest runs the sanitized twin about 3.4x slower than arm64
# clang here, and the sanitized twin is the whole cost. At N=200000 the target
# measured 41.6 s there against a 60 s budget, which meets the bar with too
# little left for a busy runner; at N=100000 it is about 25 s, half the budget.
#
#   leg (at N=100000)                    here (arm64)   ubuntu-latest
#   schema_test_block_fuzz                     1.4 s        ~ 1.8 s
#   schema_test_block_fuzz_asan                6.0 s        ~ 16 s
#   test/cs-block -- --fuzz                    2.2 s        ~ 3 s
#
# The ENUMERATED passes are what cover the boundaries and they run whatever N
# is; N buys random mutants on top, and a long run is what the override is for.
N ?= 100000
SEED ?= 24845619678

BLOCK_FUZZ_INCLUDES := -Ibuild/tables-generated/block -Ibuild/tables-generated/blockhome
BLOCK_FUZZ_SOURCES = $$(ls build/tables-generated/block/*Block.cpp build/tables-generated/blockhome/*Block.cpp)

build/schema_test_block_fuzz: build/tables-generated/.stamp test/tables/block_fuzz_main.cpp
	@mkdir -p build
	$(CXX) $(BLOCK_CXXFLAGS) $(BLOCK_FUZZ_INCLUDES) test/tables/block_fuzz_main.cpp $(BLOCK_FUZZ_SOURCES) -o $@

# The SANITIZED twin (#277's rule), and here it is the point rather than a
# companion: the oracle proves a mutant that OPENED stays inside the extent,
# and only the sanitizer can prove that a mutant BlockOpen refused read nothing
# outside it on the way to refusing. The region is allocated at exactly the
# bytes the caller claims, so the redzone begins at the extent's last byte.
build/schema_test_block_fuzz_asan: build/tables-generated/.stamp test/tables/block_fuzz_main.cpp
	@mkdir -p build
	$(CXX) $(BLOCK_CXXFLAGS) -fsanitize=address,undefined -fno-sanitize-recover=all \
		-fno-omit-frame-pointer -g $(BLOCK_FUZZ_INCLUDES) test/tables/block_fuzz_main.cpp \
		$(BLOCK_FUZZ_SOURCES) -o $@

# The seed blocks: one valid block per unit per count vector, written by the
# generated C++ BUILDER, because C# has only the read half of the form.
build/block-fuzz/.stamp: build/schema_test_block_fuzz
	@rm -rf build/block-fuzz && mkdir -p build/block-fuzz
	./build/schema_test_block_fuzz --dump build/block-fuzz
	@touch $@

.PHONY: tables-block-fuzz
tables-block-fuzz: build/schema_test_block_fuzz build/schema_test_block_fuzz_asan build/block-fuzz/.stamp build/tables-generated-cs/.stamp
	SEED=$(SEED) N=$(N) ./build/schema_test_block_fuzz
	SEED=$(SEED) N=$(N) ./build/schema_test_block_fuzz_asan
	cd test/cs-block && SEED=$(SEED) N=$(N) dotnet run -- --fuzz

# THE COOK FIXTURES the Rust fuzzer's cook half forges, written by test/cookgen
# with the same chains the conformance harness uses — the fixture generator's
# own facts, which the conformance data deliberately does not carry.
build/cook-fuzz/.stamp: $(wildcard test/cookgen/*.go) $(SCHEMAS_TABLES_POINTERS)
	@rm -rf build/cook-fuzz && mkdir -p build/cook-fuzz
	go build -o build/cookgen ./test/cookgen
	./build/cookgen --bytes 4096 --root Scene    --ref head --chain ListNode --next next --out build/cook-fuzz/Scene.cook
	./build/cookgen --bytes 4096 --root Depot    --ref head --chain ListNode --next next --out build/cook-fuzz/Depot.cook
	./build/cookgen --bytes 4096 --root Album    --ref head --chain ListNode --next next --out build/cook-fuzz/Album.cook
	./build/cookgen --bytes 4096 --root TreeNode --ref left --chain TreeNode --next left --out build/cook-fuzz/TreeNode.cook
	./build/cookgen --bytes 4096 --root ListNode --ref next --chain ListNode --next next --out build/cook-fuzz/ListNode.cook
	@touch $@

# The NEGATIVE CONTROLS. A fuzzer that has never gone red proves nothing about
# the checks it is watching, so each of these REMOVES one of BlockOpen's checks
# from the EMITTER — from both backends at once — and requires the fuzzer to
# find it. The emitter is replaced through `go build -overlay`, so no tracked
# file is edited and the sabotage cannot survive the target that made it.
#
# The gate they hold is the ORACLE's independence: it re-derives every bound
# from the descriptors and from the triples in the instance, so it can disagree
# with BlockOpen. An oracle that shared BlockOpen's arithmetic would stay green
# through both of these.

# The sed program that removes each check from each emitter. They live in
# variables rather than in the $(call) below because a sed address RANGE
# carries a comma, and make would read it as another argument.
BLOCK_FUZZ_SED_CPP_extent := /rows > (uint64_t) bytes - offset_of/d; /padding > bytes - used/d
BLOCK_FUZZ_SED_CS_extent := /rows > (ulong) bytes - offsetOf/d; /padding > bytes - used/d
BLOCK_FUZZ_SED_CPP_maximum := /count > (uint64_t) %sBlock/,/overflow on a count the maximum does not bound/d
BLOCK_FUZZ_SED_CS_maximum := /count > (ulong) %sMax/d
BLOCK_FUZZ_SED_RUST_extent := /rows > bytes as u64 - offset_of/d; /padding > bytes - used/d
BLOCK_FUZZ_SED_RUST_maximum := /count > Self::%s_MAX as u64/,/on a count the maximum does not bound/d

# how a sabotage is built: $(1) is its name, and the two sed programs above
# named by it are what come out of each emitter. The replacement files take a
# .go.txt suffix on purpose: `go build -overlay` does not care what a
# replacement is called, and `go test ./...` walks build/ and would otherwise
# find two packages sitting in one directory.
define block_fuzz_sabotage
	@rm -rf build/block-fuzz-$(1) && mkdir -p build/block-fuzz-$(1)
	@sed '$(BLOCK_FUZZ_SED_CPP_$(1))' internal/codegen/cpptable/block.go > build/block-fuzz-$(1)/cpptable-block.go.txt
	@sed '$(BLOCK_FUZZ_SED_CS_$(1))' internal/codegen/cstable/block.go > build/block-fuzz-$(1)/cstable-block.go.txt
	@sed '$(BLOCK_FUZZ_SED_RUST_$(1))' internal/codegen/rusttable/block.go > build/block-fuzz-$(1)/rusttable-block.go.txt
	@cmp -s internal/codegen/cpptable/block.go build/block-fuzz-$(1)/cpptable-block.go.txt && \
		{ echo "NEGATIVE CONTROL: the C++ emitter sabotage did not apply"; exit 1; } || true
	@cmp -s internal/codegen/cstable/block.go build/block-fuzz-$(1)/cstable-block.go.txt && \
		{ echo "NEGATIVE CONTROL: the C# emitter sabotage did not apply"; exit 1; } || true
	@cmp -s internal/codegen/rusttable/block.go build/block-fuzz-$(1)/rusttable-block.go.txt && \
		{ echo "NEGATIVE CONTROL: the Rust emitter sabotage did not apply"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/codegen/cpptable/block.go":"%s/build/block-fuzz-$(1)/cpptable-block.go.txt","%s/internal/codegen/cstable/block.go":"%s/build/block-fuzz-$(1)/cstable-block.go.txt","%s/internal/codegen/rusttable/block.go":"%s/build/block-fuzz-$(1)/rusttable-block.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" "$(CURDIR)" "$(CURDIR)" "$(CURDIR)" "$(CURDIR)" > build/block-fuzz-$(1)/overlay.json
	go build -overlay build/block-fuzz-$(1)/overlay.json -o build/block-fuzz-$(1)/schema ./cmd/schema
	@rm -rf build/block-fuzz-$(1)/generated
	./build/block-fuzz-$(1)/schema generate --lang cpp --out build/block-fuzz-$(1)/generated/block tables/block
	./build/block-fuzz-$(1)/schema generate --lang cpp --out build/block-fuzz-$(1)/generated/blockhome tables/blockhome
	./build/block-fuzz-$(1)/schema generate --lang cs --out build/block-fuzz-$(1)/generated/block-cs tables/block
	./build/block-fuzz-$(1)/schema generate --lang cs --out build/block-fuzz-$(1)/generated/blockhome-cs tables/blockhome
	$(CXX) $(BLOCK_CXXFLAGS) -Ibuild/block-fuzz-$(1)/generated/block -Ibuild/block-fuzz-$(1)/generated/blockhome \
		test/tables/block_fuzz_main.cpp \
		$$(ls build/block-fuzz-$(1)/generated/block/*Block.cpp build/block-fuzz-$(1)/generated/blockhome/*Block.cpp) \
		-o build/block-fuzz-$(1)/fuzz
	@if SEED=$(SEED) N=$(N) ./build/block-fuzz-$(1)/fuzz > build/block-fuzz-$(1)/cpp.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the C++ fuzzer stayed green with the $(1) check removed from the emitter"; \
		exit 1; \
	fi
	@grep -q "^FAILED: an opened block" build/block-fuzz-$(1)/cpp.log || \
		{ echo "NEGATIVE CONTROL FAILED: the C++ leg went red, but not on the oracle"; cat build/block-fuzz-$(1)/cpp.log; exit 1; }
	@if ( cd test/cs-block && SEED=$(SEED) N=$(N) dotnet run \
			-p:BlockGeneratedDir=../../build/block-fuzz-$(1)/generated/block-cs \
			-p:BlockHomeGeneratedDir=../../build/block-fuzz-$(1)/generated/blockhome-cs -- --fuzz ) \
			> build/block-fuzz-$(1)/cs.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the C# fuzzer stayed green with the $(1) check removed from the emitter"; \
		exit 1; \
	fi
	@grep -q "^FAILED: an opened block" build/block-fuzz-$(1)/cs.log || \
		{ echo "NEGATIVE CONTROL FAILED: the C# leg went red, but not on the oracle"; cat build/block-fuzz-$(1)/cs.log; exit 1; }
	@rm -f build/tables-generated-rust/.stamp
	./build/block-fuzz-$(1)/schema generate --lang rust --out build/tables-generated-rust/blockdemo/src tables/block
	./build/block-fuzz-$(1)/schema generate --lang rust --out build/tables-generated-rust/blockhome/src tables/blockhome
	cd test/rust-fuzz && PATH="$(RUSTUP_BIN):$$PATH" cargo build --quiet
	@if SEED=$(SEED) N=$(N) ./test/rust-fuzz/target/debug/rust-fuzz > build/block-fuzz-$(1)/rust.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the Rust fuzzer stayed green with the $(1) check removed from the emitter"; \
		exit 1; \
	fi
	@grep -q "^FAILED: an opened block" build/block-fuzz-$(1)/rust.log || \
		{ echo "NEGATIVE CONTROL FAILED: the Rust leg went red, but not on the oracle"; cat build/block-fuzz-$(1)/rust.log; exit 1; }
	@grep -m1 "^FAILED" build/block-fuzz-$(1)/cpp.log
	@grep -m1 "  mutation" build/block-fuzz-$(1)/cpp.log
	@echo "block fuzz $(1) negative control: removing that check from EVERY emitter turns the fuzzer red on every backend"
endef

# The EXTENT check: an array's rows must end inside the bytes the caller
# passed, and the used extent it reports must too. Both spellings go, because
# either one alone still refuses what the other would have.
.PHONY: tables-block-fuzz-extent-negative-control
tables-block-fuzz-extent-negative-control: build/block-fuzz/.stamp build/tables-generated-cs/.stamp build/tables-generated-rust/.stamp build/cook-fuzz/.stamp
	$(call block_fuzz_sabotage,extent)

# THE GO FUZZER, and its two negative controls on the same rule as the C++
# ones: it is the standing gate over the Go accelerators' Open, and a fuzzer
# that has never gone red proves nothing about the checks it is watching.
#
# It runs UNDER -race as well as plain, because a block and a cook are memory
# another language wrote and the Go readers point into it with `unsafe`: the
# race detector is what says the pointing itself is sound, and Go's bounds
# checks — which are on in every configuration — are what the oracle's own walk
# reads through.
# THE GO SOAK (docs/SPEC-TABLES.md, "What allocates, and what never does").
# Correctness tests are necessary and not sufficient: a read path that leaks one
# object per call passes every byte comparison in this repo and takes a server
# down in an afternoon. This reads and writes the WHOLE corpus in a loop and
# watches the ALLOCATION COUNTER, which must not move at all.
#
# Two seconds of it ride `go test ./...` so a regression is caught on the way
# past; SOAK names the real one. It found a real defect on its first run: the
# text walk rebuilt a union field's arms table on every call, ten objects per
# ToJson of an instance carrying five unions, which no byte comparison could
# ever have seen.
SOAK ?= 1h

# The DECLARED MAXIMUM check: a count past the maximum Begin refuses on the
# producer side. This is §19.2's tenth forgery, the one a reader found OPEN,
# and the control is what keeps it closed.
.PHONY: tables-block-fuzz-maximum-negative-control
tables-block-fuzz-maximum-negative-control: build/block-fuzz/.stamp build/tables-generated-cs/.stamp build/tables-generated-rust/.stamp build/cook-fuzz/.stamp
	$(call block_fuzz_sabotage,maximum)

# THE COOK'S NEGATIVE CONTROL (docs/SPEC-TABLES.md §7.5). The hostile battery in
# internal/tablecook is a Go test and rides `go test ./...`; what a battery
# cannot prove about itself is that it would go RED, so this removes PASS ONE
# — `cook-check`'s directory scan — and requires it to.
#
# The sabotage is one inserted `return nil` at the top of checkDirectory, which
# is unambiguous in a way a deleted range is not, and it reaches the build
# through `go test -overlay`, so no tracked file is edited and the sabotage
# cannot survive the target that made it.
.PHONY: tables-cook-fuzz-negative-control
tables-cook-fuzz-negative-control:
	@rm -rf build/cook-fuzz-control && mkdir -p build/cook-fuzz-control
	@sed 's|^func checkDirectory(m \*tabletext.Model, h Header, dir \[\]DirectoryEntry) error {$$|&\n\treturn nil // NEGATIVE CONTROL|' \
		internal/tablecook/check.go > build/cook-fuzz-control/check.go.txt
	@cmp -s internal/tablecook/check.go build/cook-fuzz-control/check.go.txt && \
		{ echo "NEGATIVE CONTROL: the cook-check sabotage did not apply"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/tablecook/check.go":"%s/build/cook-fuzz-control/check.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/cook-fuzz-control/overlay.json
	@if SEED=$(SEED) N=$(N) go test -count=1 -overlay=build/cook-fuzz-control/overlay.json \
			-run 'TestCookCheck' ./internal/tablecook/ > build/cook-fuzz-control/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the cook battery stayed green with the directory scan removed"; \
		exit 1; \
	fi
	@grep -q "FAILED" build/cook-fuzz-control/log || \
		{ echo "NEGATIVE CONTROL FAILED: the battery went red, but not on the bar"; cat build/cook-fuzz-control/log; exit 1; }
	@grep -m1 "FAILED" build/cook-fuzz-control/log

# THE SCALE FIXTURES (docs/SPEC-TABLES.md §7.5). `test/cookgen` writes a synthetic
# region streaming, in O(1) memory, so the OPEN-COST gate the emitter owes —
# open time flat across 1 MB, 100 MB and 1 GB — has inputs the C++ worker can
# regenerate rather than a gigabyte in the tree.
#
# CI runs the first two, under the two-minute rule. THE GIGABYTE IS RUN BY
# HAND (`make tables-cook-scale-1gb`): it writes a 1.6 GB file, which is not
# something a CI runner's disk should meet on every push. What runs here is
# the TOOL's own scan, which is O(R + P log N) and not the O(1) the runtime
# owes; this target's job is to produce the fixtures and prove they check.
COOKGEN_UNIT := tables/pointers
.PHONY: tables-cook-scale
tables-cook-scale: bin/schema
	@mkdir -p build/cook
	go build -o build/cookgen ./test/cookgen
	./build/cookgen --bytes 1048576   --out build/cook/1mb.cook
	./build/cookgen --bytes 1048576   --byte-order big --out build/cook/1mb-be.cook
	./build/cookgen --bytes 104857600 --out build/cook/100mb.cook
	./bin/schema cook-check --verbose build/cook/1mb.cook    $(COOKGEN_UNIT)
	./bin/schema cook-check --verbose build/cook/1mb-be.cook $(COOKGEN_UNIT)
	./bin/schema cook-check --verbose build/cook/100mb.cook  $(COOKGEN_UNIT)

.PHONY: tables-cook-scale-1gb
tables-cook-scale-1gb: bin/schema
	@mkdir -p build/cook
	go build -o build/cookgen ./test/cookgen
	./build/cookgen --bytes 1073741824 --out build/cook/1gb.cook
	./bin/schema cook-check --verbose build/cook/1gb.cook $(COOKGEN_UNIT)

# ---- the COOK's C++ READ SIDE (docs/SPEC-TABLES.md §7) --------------------------
#
# `schema cook` writes the file and the generated <Root>Open points at it, and
# the two were written from the page independently — the tool in Go, the C++
# side from §7 alone, neither reading the other. That is what makes the golden
# mode a CROSS-IMPLEMENTATION gate rather than one implementation agreeing with
# itself: every node the C++ side reaches through its own derefs must be a node
# the cook's ATTRIBUTION part names, at that offset, with that type id, and the
# two sets must be equal.
#
# THE FIXTURES come from test/cookgen, which streams a synthetic region in O(1)
# memory (§7.5), so the 100 MB arm of the O(1) gate is regenerated rather than
# committed. Five roots cover the shapes a region has: a pointer chain, a tree,
# a keyed array of variable tables beside an optional, a cross-file graph
# through a by-value variable table, and a bare chain node as its own root.
COOK_ROOTS := Scene Depot Album TreeNode ListNode
# The FIXED roots. A fixed table's cook is ONE REGION OF ONE NODE (§7) and the
# same header match, and it is where the VALUE crossing lives: a fixed table has
# no pointer, so it has no node table and no kind 17, so this backend's wire and
# the tool's are the same bytes — the C++ side writes a known instance, the tool
# cooks it, and the C++ side reads the values back out of the tool's layout.
# Settings is POINTED AT; Stamp is pointed at by nothing and is declared in a
# file with no variable table of its own, which is the shape a file-scoped
# emission rule forgets.
COOK_FIXED_ROOTS := Settings Stamp
# and the roots whose cook the C++ side WRITES (docs/SPEC-TABLES.md §7.6): the two
# fixed ones, and the pointered Scene graph test/tables/cook_main.cpp builds
COOK_WRITE_ROOTS := Settings Stamp Scene

COOK_INCLUDES := -Ibuild/tables-generated/pointers -Itest/tables
COOK_CXXFLAGS := -std=c++17 -Wall -Wextra -Werror -Wshadow -ffp-contract=off -pthread
# The SANITIZED twin is a gate and not a nicety here: Open is handed a hostile
# buffer by construction, the driver allocates each fixture at EXACTLY the
# length it claims so a redzone sits on the next byte, and
# -fno-sanitize-recover=all is what stops UBSan printing and continuing.
COOK_SANITIZE := -fsanitize=address,undefined -fno-sanitize-recover=all -fno-omit-frame-pointer -g

build/schema_test_cook: build/tables-generated/.stamp test/tables/cook_main.cpp
	@mkdir -p build
	$(CXX) $(COOK_CXXFLAGS) $(COOK_INCLUDES) test/tables/cook_main.cpp -o $@

build/schema_test_cook_asan: build/tables-generated/.stamp test/tables/cook_main.cpp
	@mkdir -p build
	$(CXX) $(COOK_CXXFLAGS) $(COOK_SANITIZE) $(COOK_INCLUDES) test/tables/cook_main.cpp -o $@

# The fixtures the C++ gate opens. Small ones per root for the golden lock, the
# battery and the fuzzer; a big-endian one for the byte-order leg; and the two
# scale arms for the O(1) gate. `schema cook-check` runs over every small one
# first, so a fixture the TOOL itself would refuse can never be what a green
# C++ run is standing on.
build/cook-open/.stamp: bin/schema $(SCHEMAS_TABLES_POINTERS)
	@rm -rf build/cook-open && mkdir -p build/cook-open
	go build -o build/cookgen ./test/cookgen
	./build/cookgen --bytes 4096 --root Scene    --ref head --chain ListNode --next next --out build/cook-open/Scene.cook
	./build/cookgen --bytes 3000 --root Depot    --ref head --chain ListNode --next next --out build/cook-open/Depot.cook
	./build/cookgen --bytes 3000 --root Album    --ref head --chain ListNode --next next --out build/cook-open/Album.cook
	./build/cookgen --bytes 3000 --root TreeNode --ref left --chain TreeNode --next left --out build/cook-open/TreeNode.cook
	./build/cookgen --bytes 3000 --root ListNode --ref next --chain ListNode --next next --out build/cook-open/ListNode.cook
	./build/cookgen --bytes 4096 --root Scene    --byte-order big --out build/cook-open/Scene-be.cook
	./build/cookgen --bytes 1048576   --root Scene --out build/cook-open/1mb.cook
	./build/cookgen --bytes 104857600 --root Scene --out build/cook-open/100mb.cook
	@for r in $(COOK_ROOTS); do \
		./bin/schema cook-check --root $$r build/cook-open/$$r.cook tables/pointers || exit 1; \
	done
	./bin/schema cook-check --root Scene build/cook-open/Scene-be.cook tables/pointers
	@touch $@

# The fixtures the C++ side WRITES: it saves a known instance to the tolerant
# wire — a fixed root's value, or the pointered Scene graph — the tool cooks that
# wire in both orders, the value gate reads a fixed instance back out of the
# cook, and the write gate holds the runtime's own cook to the tool's file. The
# tool's own read report must be SILENT over a wire this backend wrote — a
# crossing in the other direction, for free.
build/cook-open-fixed/.stamp: bin/schema build/schema_test_cook build/cook-open/.stamp
	@rm -rf build/cook-open-fixed && mkdir -p build/cook-open-fixed
	@for r in $(COOK_WRITE_ROOTS); do \
		./build/schema_test_cook write $$r build/cook-open-fixed/$$r.bin || exit 1; \
		./bin/schema cook --root $$r --in build/cook-open-fixed/$$r.bin \
			--out build/cook-open-fixed/$$r.cook --verbose tables/pointers || exit 1; \
		./bin/schema cook-check --root $$r build/cook-open-fixed/$$r.cook tables/pointers || exit 1; \
		./bin/schema cook --root $$r --in build/cook-open-fixed/$$r.bin --byte-order big \
			--out build/cook-open-fixed/$$r-be.cook tables/pointers || exit 1; \
	done
	@touch $@

.PHONY: tables-cook-open tables-cook-valued
tables-cook-open: build/schema_test_cook build/schema_test_cook_asan build/cook-open/.stamp build/cook-open-fixed/.stamp
	@for r in $(COOK_ROOTS); do \
		./build/schema_test_cook      golden $$r build/cook-open/$$r.cook || exit 1; \
		./build/schema_test_cook_asan golden $$r build/cook-open/$$r.cook || exit 1; \
	done
	@for r in $(COOK_FIXED_ROOTS); do \
		./build/schema_test_cook      golden      $$r build/cook-open-fixed/$$r.cook || exit 1; \
		./build/schema_test_cook_asan golden      $$r build/cook-open-fixed/$$r.cook || exit 1; \
		./build/schema_test_cook      fixedvalues $$r build/cook-open-fixed/$$r.cook || exit 1; \
		./build/schema_test_cook_asan fixedvalues $$r build/cook-open-fixed/$$r.cook || exit 1; \
		./build/schema_test_cook_asan forge       $$r build/cook-open-fixed/$$r.cook || exit 1; \
	done
	./build/schema_test_cook      usage Scene build/cook-open/Scene.cook
	./build/schema_test_cook      forge Scene build/cook-open/Scene.cook
	./build/schema_test_cook_asan forge Scene build/cook-open/Scene.cook
	./build/schema_test_cook_asan forge Depot build/cook-open/Depot.cook
	SEED=$(SEED) N=$(N) ./build/schema_test_cook_asan fuzz Scene build/cook-open/Scene.cook
	SEED=$(SEED) N=$(N) ./build/schema_test_cook_asan fuzz TreeNode build/cook-open/TreeNode.cook
	./build/schema_test_cook accept Scene build/cook-open/Scene.cook
	./build/schema_test_cook refuse Scene build/cook-open/Scene-be.cook
	./build/schema_test_cook time Scene build/cook-open/1mb.cook build/cook-open/100mb.cook

# THE WRITE SIDE, against the tool's own bytes (docs/SPEC-TABLES.md §7.6). The
# tool cooked the wire this backend wrote, in BOTH byte orders, and the
# generated <Root>Cook must land on those files exactly — a second cooker with
# its own bytes would be a second format, and a cook is content-addressed by
# (asset hash, build version). The same mode counts allocations across the
# measure and both writes — none for a fixed root; for the pointered Scene,
# the numbering's and only through the TableAllocator handed in, cooked from
# the unlocked builder, a loaded region and the locked region alike — requires
# a short capacity to write nothing, and opens what it wrote. The SANITIZED
# twin runs it too: the writer is handed a buffer of exactly its own measure,
# so a byte past it lands in a redzone.
.PHONY: tables-cook-write
tables-cook-write: build/schema_test_cook build/schema_test_cook_asan build/cook-open-fixed/.stamp
	@for r in $(COOK_WRITE_ROOTS); do \
		./build/schema_test_cook      cookwrite $$r build/cook-open-fixed/$$r.cook build/cook-open-fixed/$$r-be.cook || exit 1; \
		./build/schema_test_cook_asan cookwrite $$r build/cook-open-fixed/$$r.cook build/cook-open-fixed/$$r-be.cook || exit 1; \
	done

# ITS NEGATIVE CONTROL, and it is the ALLOCATION half — the half a byte
# comparison cannot fail on. One allocation inside the measured region, and the
# gate must go red: a zero-allocation claim whose instrument has never fired is
# a claim nobody has checked.
.PHONY: tables-cook-write-negative-control
tables-cook-write-negative-control: build/schema_test_cook build/cook-open-fixed/.stamp
	@mkdir -p build
	@if COOK_WRITE_SABOTAGE=1 ./build/schema_test_cook cookwrite Settings \
			build/cook-open-fixed/Settings.cook build/cook-open-fixed/Settings-be.cook \
			> build/cook-write-control.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: one allocation per write left the gate GREEN"; exit 1; \
	fi
	@grep -q "the write side allocated" build/cook-write-control.log || \
		{ echo "NEGATIVE CONTROL FAILED: the gate went red, but not on the allocation"; \
		  cat build/cook-write-control.log; exit 1; }
	@echo "negative control: one allocation inside the write turns the cook-write gate red"

# AND THE POINTERED HALF's control, which is the HOOKS test's to hold
# (docs/SPEC-TABLES.md §7.6, §13.9): a pointered writer allocates its numbering,
# and every byte of it must go through the pair it was handed. The sabotage
# makes the builder overload of <Root>Cook ignore the builder's pair and take
# the DEFAULT one — the bypass a writer would most plausibly commit — and the
# hooks unit, whose default pair counts and must read zero, has to go red.
COOK_WRITE_HOOKS_SABOTAGE := build/cook-write-hooks-control
.PHONY: tables-cook-write-hooks-negative-control
tables-cook-write-hooks-negative-control: bin/schema test/tables/hooks_main.cpp
	@rm -rf $(COOK_WRITE_HOOKS_SABOTAGE) && mkdir -p $(COOK_WRITE_HOOKS_SABOTAGE)
	@sed -e 's|order, builder.arena.allocator );|order, TableDefaultAllocator() /* SABOTAGED: the builder pair is ignored */ );|' \
		internal/codegen/cpptable/cookwrite.go > $(COOK_WRITE_HOOKS_SABOTAGE)/cookwrite.go.txt
	@cmp -s internal/codegen/cpptable/cookwrite.go $(COOK_WRITE_HOOKS_SABOTAGE)/cookwrite.go.txt && \
		{ echo "NEGATIVE CONTROL FAILED: the cook-write sabotage did not apply"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/codegen/cpptable/cookwrite.go":"%s/$(COOK_WRITE_HOOKS_SABOTAGE)/cookwrite.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > $(COOK_WRITE_HOOKS_SABOTAGE)/overlay.json
	@go build -overlay=$(COOK_WRITE_HOOKS_SABOTAGE)/overlay.json -o $(COOK_WRITE_HOOKS_SABOTAGE)/schema ./cmd/schema
	@$(COOK_WRITE_HOOKS_SABOTAGE)/schema generate --lang cpp --out $(COOK_WRITE_HOOKS_SABOTAGE)/pointers tables/pointers > /dev/null
	@$(COOK_WRITE_HOOKS_SABOTAGE)/schema generate --lang cpp --out $(COOK_WRITE_HOOKS_SABOTAGE)/blobs tables/blobs > /dev/null
	@$(CXX) $(TABLES_CXXFLAGS) -I$(COOK_WRITE_HOOKS_SABOTAGE)/pointers -I$(COOK_WRITE_HOOKS_SABOTAGE)/blobs -Itest/tables test/tables/hooks_main.cpp \
		$$(ls $(COOK_WRITE_HOOKS_SABOTAGE)/pointers/*Table.cpp $(COOK_WRITE_HOOKS_SABOTAGE)/blobs/*Table.cpp) -o $(COOK_WRITE_HOOKS_SABOTAGE)/hooks
	@if $(COOK_WRITE_HOOKS_SABOTAGE)/hooks > $(COOK_WRITE_HOOKS_SABOTAGE)/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a cook through the default pair left the hooks test GREEN"; exit 1; \
	fi
	@grep -q "g_fallback_allocs == 0" $(COOK_WRITE_HOOKS_SABOTAGE)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the hooks test went red, but not on the default pair's count"; \
		  cat $(COOK_WRITE_HOOKS_SABOTAGE)/log; exit 1; }
	@echo "negative control: a pointered cook that ignores the builder's pair turns the hooks test red"

# The BLOB READ PATH's NEGATIVE CONTROL: plant one allocation into the view —
# the read path's only new code — through the same overlay mechanism, and the
# hooks test's frozen counters must go red. The claim "the read path allocates
# nothing" is held by a RUN, so its control must be a run that fails.
BLOB_READ_SABOTAGE := build/blob-read-control
.PHONY: tables-blob-read-hooks-negative-control
tables-blob-read-hooks-negative-control: bin/schema test/tables/hooks_main.cpp
	@rm -rf $(BLOB_READ_SABOTAGE) && mkdir -p $(BLOB_READ_SABOTAGE)
	@sed -e 's|TableBytesView view = { NULL, 0 };|TableBytesView view = { NULL, 0 }; schema_release( schema_allocate( 1 ) ); /* SABOTAGED: an allocation on the read path */|' \
		internal/codegen/cpptable/arena.go > $(BLOB_READ_SABOTAGE)/arena.go.txt
	@cmp -s internal/codegen/cpptable/arena.go $(BLOB_READ_SABOTAGE)/arena.go.txt && \
		{ echo "NEGATIVE CONTROL FAILED: the read-path sabotage did not apply"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/codegen/cpptable/arena.go":"%s/$(BLOB_READ_SABOTAGE)/arena.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > $(BLOB_READ_SABOTAGE)/overlay.json
	@go build -overlay=$(BLOB_READ_SABOTAGE)/overlay.json -o $(BLOB_READ_SABOTAGE)/schema ./cmd/schema
	@$(BLOB_READ_SABOTAGE)/schema generate --lang cpp --out $(BLOB_READ_SABOTAGE)/pointers tables/pointers > /dev/null
	@$(BLOB_READ_SABOTAGE)/schema generate --lang cpp --out $(BLOB_READ_SABOTAGE)/blobs tables/blobs > /dev/null
	@$(CXX) $(TABLES_CXXFLAGS) -I$(BLOB_READ_SABOTAGE)/pointers -I$(BLOB_READ_SABOTAGE)/blobs -Itest/tables test/tables/hooks_main.cpp \
		$$(ls $(BLOB_READ_SABOTAGE)/pointers/*Table.cpp $(BLOB_READ_SABOTAGE)/blobs/*Table.cpp) -o $(BLOB_READ_SABOTAGE)/hooks
	@if $(BLOB_READ_SABOTAGE)/hooks > $(BLOB_READ_SABOTAGE)/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: an allocating view left the hooks test GREEN"; exit 1; \
	fi
	@grep -q "fallback_before" $(BLOB_READ_SABOTAGE)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the hooks test went red, but not on the frozen read-path counter"; \
		  cat $(BLOB_READ_SABOTAGE)/log; exit 1; }
	@echo "negative control: one allocation inside a blob view turns the hooks test red"

# THE SPAN's NEGATIVE CONTROL (docs/SPEC-TABLES.md §2.5, §6.5). A byte buffer
# larger than one 64 KiB slab cannot be bump-allocated in a slab: it takes a
# SPAN of the arena's address space. The sabotage removes exactly that choice —
# the size test that sends a large blob to TableArenaGrabSpan is made never to
# fire, and every other line of the allocator is the tree's — so the blob is
# bump-allocated in a 64 KiB slab and runs off the end of it, over memory the
# arena goes on to hand to the nodes allocated after it. The driver reads the
# blob back after those allocations and the pattern does not survive. It builds
# under the sanitizers, so an overrun that lands outside the slab's own block
# is named at the byte rather than read back as a wrong value.
#
# Both halves run: the tree's own emitter through the same driver must be
# GREEN, so a red below is the sabotage and not the driver.
BLOB_SPAN_SABOTAGE := build/blob-span-control
BLOB_SPAN_SANITIZE := -fsanitize=address,undefined -fno-sanitize-recover=all -fno-omit-frame-pointer -g
.PHONY: tables-blob-span-negative-control
tables-blob-span-negative-control: bin/schema test/tables/blob_span_main.cpp
	@rm -rf $(BLOB_SPAN_SABOTAGE) && mkdir -p $(BLOB_SPAN_SABOTAGE)
	@sed -e 's|if ( bytes > (int64_t) kTableSlabBytes )|if ( false ) /* SABOTAGED: no blob takes a span */|' \
		internal/codegen/cpptable/arena.go > $(BLOB_SPAN_SABOTAGE)/arena.go.txt
	@cmp -s internal/codegen/cpptable/arena.go $(BLOB_SPAN_SABOTAGE)/arena.go.txt && \
		{ echo "NEGATIVE CONTROL FAILED: the span sabotage did not apply"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/codegen/cpptable/arena.go":"%s/$(BLOB_SPAN_SABOTAGE)/arena.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > $(BLOB_SPAN_SABOTAGE)/overlay.json
	@go build -overlay=$(BLOB_SPAN_SABOTAGE)/overlay.json -o $(BLOB_SPAN_SABOTAGE)/schema ./cmd/schema
	@$(BLOB_SPAN_SABOTAGE)/schema generate --lang cpp --out $(BLOB_SPAN_SABOTAGE)/blobs tables/blobs > /dev/null
	@./bin/schema generate --lang cpp --out $(BLOB_SPAN_SABOTAGE)/blobs-good tables/blobs > /dev/null
	$(CXX) $(TABLES_CXXFLAGS) $(BLOB_SPAN_SANITIZE) -I$(BLOB_SPAN_SABOTAGE)/blobs-good -Itest/tables \
		test/tables/blob_span_main.cpp $$(ls $(BLOB_SPAN_SABOTAGE)/blobs-good/*Table.cpp) -o $(BLOB_SPAN_SABOTAGE)/span-good
	@$(BLOB_SPAN_SABOTAGE)/span-good > $(BLOB_SPAN_SABOTAGE)/good.log 2>&1 || \
		{ echo "NEGATIVE CONTROL FAILED: the driver is red on the tree's own emitter"; \
		  cat $(BLOB_SPAN_SABOTAGE)/good.log; exit 1; }
	$(CXX) $(TABLES_CXXFLAGS) $(BLOB_SPAN_SANITIZE) -I$(BLOB_SPAN_SABOTAGE)/blobs -Itest/tables \
		test/tables/blob_span_main.cpp $$(ls $(BLOB_SPAN_SABOTAGE)/blobs/*Table.cpp) -o $(BLOB_SPAN_SABOTAGE)/span
	@if $(BLOB_SPAN_SABOTAGE)/span > $(BLOB_SPAN_SABOTAGE)/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a blob bump-allocated in a slab it does not fit left the driver GREEN"; \
		cat $(BLOB_SPAN_SABOTAGE)/log; exit 1; \
	fi
	@grep -q "heap-buffer-overflow\|blob past the slab" $(BLOB_SPAN_SABOTAGE)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the driver went red, but not on the blob"; \
		  cat $(BLOB_SPAN_SABOTAGE)/log; exit 1; }
	@grep -m1 "heap-buffer-overflow\|blob past the slab" $(BLOB_SPAN_SABOTAGE)/log
	@echo "negative control: a blob larger than a slab, denied its span, overruns the slab"

# THE VALUED FIXTURE, cross-checked by the ENGINE's own uncook (§7.5, §7.4).
#
# `test/cookgen --values` fills every non-pointer leaf, so the conformance
# harness's `SceneValued` dump locks what a reader READS out of a node and not
# only where the node is. That dump is pinned from the C++ leg and byte-compared
# by the C# one; this is the THIRD reader, and it is the Go engine's:
#
#   cookgen --values  ->  uncook  ->  cook   must land on the same bytes
#
# `uncook` walks the region and recovers the root's wire, `cook` lays a region
# out from that wire, and the two regions being byte-identical is every value
# surviving a round trip through an implementation that did not write the
# fixture. A value dropped, widened or misplaced by either half moves a byte
# here — and `cook-check` first, so a fixture the tool itself would refuse can
# never be what a green run is standing on.
.PHONY: tables-cook-valued
tables-cook-valued: bin/schema
	@rm -rf build/cook-valued && mkdir -p build/cook-valued
	go build -o build/cookgen ./test/cookgen
	./build/cookgen --bytes 4096 --root Scene --ref head --chain ListNode --next next --values \
		--out build/cook-valued/SceneValued.cook
	./bin/schema cook-check --root Scene --verbose build/cook-valued/SceneValued.cook tables/pointers
	./bin/schema uncook --root Scene --in build/cook-valued/SceneValued.cook \
		--out build/cook-valued/Scene.wire --verbose tables/pointers
	./bin/schema cook --root Scene --in build/cook-valued/Scene.wire \
		--out build/cook-valued/SceneAgain.cook --verbose tables/pointers
	@cmp build/cook-valued/SceneValued.cook build/cook-valued/SceneAgain.cook || \
		{ echo "the engine's uncook did not recover every value the fixture carries"; exit 1; }
	@echo "valued cook: cookgen --values -> uncook -> cook is byte-identical, so every value survives the engine's own read"

# THE GIGABYTE ARM, by hand (§7.5): a 1.5 GB fixture is not something a CI
# runner's disk should meet on every push, and the two arms above already span
# two orders of magnitude.
.PHONY: tables-cook-open-1gb
tables-cook-open-1gb: build/schema_test_cook build/cook-open/.stamp
	go build -o build/cookgen ./test/cookgen
	./build/cookgen --bytes 1073741824 --root Scene --out build/cook-open/1gb.cook
	./build/schema_test_cook time Scene build/cook-open/1mb.cook build/cook-open/1gb.cook

# THE NEGATIVE CONTROLS, and a battery whose battery has never gone red is
# watching nothing. Each removes ONE clause of TableCookOpen through a
# `go build -overlay` on the emitter — so no tracked file is ever written to,
# an interrupt cannot leave a sabotaged working tree, and a parallel `make -j`
# cannot compile the sabotage into something else — regenerates the corpus from
# the sabotaged emitter, and requires the C++ gate to go RED.
#
# The two clauses chosen are the two that decide whether Open can hand back
# storage the caller never gave it: the part-length equation, which is what
# refuses a truncated file, and the root-fits check, which is what refuses a
# data part too short to hold the root.
define cook_open_sabotage
	@mkdir -p build
	@rm -rf build/cook-open-$(1) && mkdir -p build/cook-open-$(1)
	@sed 's|^.*$(2).*$$|    $(3) // NEGATIVE CONTROL|' \
		internal/codegen/cpptable/cook.go > build/cook-open-$(1)/cook.gotext
	@cmp -s build/cook-open-$(1)/cook.gotext internal/codegen/cpptable/cook.go && \
		{ echo "NEGATIVE CONTROL FAILED: the $(1) sabotage patched nothing"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/codegen/cpptable/cook.go":"%s/build/cook-open-$(1)/cook.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/cook-open-$(1)/overlay.json
	@go build -overlay=build/cook-open-$(1)/overlay.json -o build/cook-open-$(1)/schema ./cmd/schema
	./build/cook-open-$(1)/schema generate --lang cpp --out build/cook-open-$(1)/gen tables/pointers
	$(CXX) $(COOK_CXXFLAGS) $(COOK_SANITIZE) -Ibuild/cook-open-$(1)/gen -Itest/tables \
		test/tables/cook_main.cpp -o build/cook-open-$(1)/test
	@if ./build/cook-open-$(1)/test forge Scene build/cook-open/Scene.cook > build/cook-open-$(1)/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the battery stayed green with the $(1) check removed"; exit 1; \
	fi
	@grep -q "FAILED" build/cook-open-$(1)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the battery went red, but not on a forgery"; cat build/cook-open-$(1)/log; exit 1; }
	@grep -m1 -A3 "FAILED" build/cook-open-$(1)/log
	@echo "negative control: removing the $(1) check turns the cook forgery battery RED"
endef

# The sabotage keeps the local it defeats IN USE, because a generated C++ file
# with an unused local does not compile under -Werror and a control that fails
# to build is not a control that went red.
.PHONY: tables-cook-open-lengths-negative-control
tables-cook-open-lengths-negative-control: build/cook-open/.stamp
	$(call cook_open_sabotage,lengths,attribution_length != length - data_offset - data_length,if ( attribution_length == UINT64_MAX ) { return NULL; })

.PHONY: tables-cook-open-root-negative-control
tables-cook-open-root-negative-control: build/cook-open/.stamp
	$(call cook_open_sabotage,root,data_length < root_size,if ( root_size == UINT64_MAX ) { return NULL; })

# THE COOK ROUND TRIP THROUGH THE CLI (docs/SPEC-TABLES.md §7.5). The Go tests hold
# the engine; this holds the three COMMANDS and their flags, over a pinned pack
# tree, in both byte orders and with the attribution written both ways.
.PHONY: tables-cook-cli
tables-cook-cli: bin/schema
	@rm -rf build/cook-cli && mkdir -p build/cook-cli
	./bin/schema pack --root PackConfig --out build/cook-cli/orig.bin tables/pack/pinned/PackConfig tables/examples
	./bin/schema cook --root PackConfig --in tables/pack/pinned/PackConfig --out build/cook-cli/tree.cook --verbose tables/examples
	./bin/schema cook-check --root PackConfig --verbose build/cook-cli/tree.cook tables/examples
	./bin/schema uncook --root PackConfig --in build/cook-cli/tree.cook --out build/cook-cli/tree.bin tables/examples
	cmp build/cook-cli/orig.bin build/cook-cli/tree.bin
	./bin/schema cook --root PackConfig --in build/cook-cli/orig.bin --out build/cook-cli/be.cook \
		--byte-order big --attribution build/cook-cli/be.attrib --verbose tables/examples
	@if ./bin/schema cook-check build/cook-cli/be.cook tables/examples 2>/dev/null; then \
		echo "FAILED: a cook carrying data alone was checked anyway"; exit 1; \
	fi
	./bin/schema cook-check --root PackConfig --attribution build/cook-cli/be.attrib --verbose build/cook-cli/be.cook tables/examples
	./bin/schema uncook --root PackConfig --in build/cook-cli/be.cook --attribution build/cook-cli/be.attrib \
		--out build/cook-cli/be.bin tables/examples
	cmp build/cook-cli/orig.bin build/cook-cli/be.bin

# THE BLOCK ZERO-COST GATE (docs/SPEC-TABLES.md §2.2, §19), in its two halves.
#
# The first asks "did any block symbol leak into a Table source?" — a grep.
# The second is the property §19 actually states: **the Table sources are
# BYTE-IDENTICAL with or without the Block files existing**, held by comparing
# 68 of them against frozen pins under testdata/golden/tables/.
#
# They are ordinary goldens: `make update-goldens` re-pins them when a TABLE
# emitter legitimately changes, and a move under an unchanged emitter is
# stop-the-line, exactly as it is for every other golden. What a frozen pin
# cannot do is survive a legitimate Table-emitter change without being
# re-pinned; schema#331 carries the follow-on that would replace it with a
# mechanical comparison against a block-less emitter, the way the negative
# controls below already build sabotaged ones.
.PHONY: tables-block-zero-cost
tables-block-zero-cost: build/tables-generated/.stamp build/tables-generated-cs/.stamp build/tables-generated-c/.stamp
	@for f in build/tables-generated/*/*Table.h build/tables-generated/*/*Table.cpp \
	          build/tables-generated-cs/*/*Table.cs; do \
		if sed -E 's://.*$$::' $$f | grep -nE "TableBlock|[A-Za-z0-9_]Block"; then \
			echo "BLOCK ZERO-COST GATE FAILED: the block form leaked into $$f"; exit 1; \
		fi; \
	done
	@echo "block zero-cost gate: no Table source carries one symbol of the block form"
# The grep runs over the CODE, with every // comment stripped first: what the
# gate holds is that no block CONSTRUCT is in a Table source, and a comment
# naming one (the arena runtime describes its allocator as the shape
# TableBlockAllocator has, §19.1) is prose, not a symbol.
# BuildVersion is NOT in that grep: it is not a block symbol — a build version
# answers "which build?" and not "which form?", both accelerators carry it
# (docs/SPEC-TABLES.md §20.6), and every C++ Table header carries it because every
# table cooks (§7). What holds the block form to zero cost is the line above —
# no Table source carries one BLOCK symbol — and the byte comparison below.
	@for f in build/tables-generated-cs/*/*Table.cs; do \
		if grep -n "BuildVersion" $$f; then \
			echo "BLOCK ZERO-COST GATE FAILED: the C# Table sources carry BuildVersion, which is the BLOCK file's there: $$f"; exit 1; \
		fi; \
	done
	@echo "block zero-cost gate: the C# Table sources still carry no build version — it is their Block file's"
	@n=0; d=0; \
	for f in testdata/golden/tables/examples/*Table.* testdata/golden/tables/pointers/*Table.* \
	         testdata/golden/tables/block/*Table.* testdata/golden/tables/blockhome/*Table.* \
	         testdata/golden/tables/messages/*Table.* testdata/golden/tables/stream/*Table.* \
	         testdata/golden/tables/blobs/*Table.* testdata/golden/tables/scalars/*Table.* \
	         testdata/golden/tables/maps/*Table.* ; do \
		dir=$$(basename $$(dirname $$f)); \
		n=$$(( n + 1 )); \
		cmp -s $$f build/tables-generated/$$dir/$$(basename $$f) || \
			{ echo "ZERO-COST GATE FAILED: $$f moved"; d=$$(( d + 1 )); }; \
	done; \
	for f in testdata/golden/tables/examples-cs/*Table.cs; do \
		n=$$(( n + 1 )); \
		cmp -s $$f build/tables-generated-cs/examples/$$(basename $$f) || \
			{ echo "ZERO-COST GATE FAILED: $$f moved"; d=$$(( d + 1 )); }; \
	done; \
	for f in testdata/golden/tables/block-cs/*Table.cs; do \
		n=$$(( n + 1 )); \
		cmp -s $$f build/tables-generated-cs/block/$$(basename $$f) || \
			{ echo "ZERO-COST GATE FAILED: $$f moved"; d=$$(( d + 1 )); }; \
	done; \
	for f in testdata/golden/tables/blockhome-cs/*Table.cs; do \
		n=$$(( n + 1 )); \
		cmp -s $$f build/tables-generated-cs/blockhome/$$(basename $$f) || \
			{ echo "ZERO-COST GATE FAILED: $$f moved"; d=$$(( d + 1 )); }; \
	done; \
	for f in testdata/golden/tables/examples-c/*Table.* testdata/golden/tables/block-c/*Table.* \
	         testdata/golden/tables/pointers-c/*Table.*; do \
		dir=$$(basename $$(dirname $$f)); \
		dir=$${dir%-c}; \
		n=$$(( n + 1 )); \
		cmp -s $$f build/tables-generated-c/$$dir/$$(basename $$f) || \
			{ echo "ZERO-COST GATE FAILED: $$f moved"; d=$$(( d + 1 )); }; \
	done; \
	if [ "$$d" != "0" ]; then exit 1; fi; \
	if [ "$$n" -lt 72 ]; then echo "ZERO-COST GATE FAILED: compared $$n Table files, expected at least 72 — the glob, not the property, is what broke"; exit 1; fi; \
	echo "block zero-cost gate: $$n Table sources byte-identical to their pins"

# THE BUILD VERSION IS ONE NUMBER (docs/SPEC-TABLES.md §20.7): the constant each
# backend emits, and the number `schema build-version` prints, are the same
# number or the tuple a store is indexed by means two different things. The
# projection it hashes is pinned beside it as a golden, so a change to how it
# is computed breaks every pinned value loudly.
.PHONY: tables-block-build-version
tables-block-build-version: bin/schema build/tables-generated/.stamp build/tables-generated-cs/.stamp
	@v=$$(./bin/schema build-version tables/block); \
		grep -q "static const uint64_t BuildVersion = $${v}ull;" build/tables-generated/block/RenderBlock.h || \
			{ echo "BUILD VERSION GATE FAILED: the C++ constant is not $$v"; exit 1; }; \
		grep -q "public const ulong BuildVersion = $${v}UL;" build/tables-generated-cs/block/*Block.cs || \
			{ echo "BUILD VERSION GATE FAILED: the C# constant is not $$v"; exit 1; }; \
		echo "block build-version gate: schema build-version, the C++ constant and the C# constant are all $$v"

# THE FILL REFUSER (docs/SPEC-TABLES.md §19.1, §19.5). The multi-threaded fill is an
# OBLIGATION on the implementation, not a permission to the caller: the
# generated fill path — Begin, the array accessors and the row storage they
# hand back — contains no allocation, no lock and no atomic, and the BUILD
# FAILS if one appears. This reads the generated SOURCE between the fill path's
# own markers; test/tables/block_main.cpp watches the RUNNING program with a
# global operator-new counter, and the two together are what the obligation
# stands on.
BLOCK_FORBIDDEN := malloc|calloc|realloc|operator new|[^_a-zA-Z]new |std::mutex|std::lock_guard|std::unique_lock|std::atomic|std::thread|pthread_|__atomic

.PHONY: tables-block-fill-refuser
tables-block-fill-refuser: build/tables-generated/.stamp
	@rm -rf build/block-fill && mkdir -p build/block-fill
	@for f in build/tables-generated/*/*Block.h; do \
		out=build/block-fill/$$(echo $$f | tr / _); \
		awk '/---- block fill path: begin ----/,/---- block fill path: end ----/' $$f > $$out; \
		regions=$$(grep -c "block fill path: begin ----" $$f || true); \
		ends=$$(grep -c "block fill path: end ----" $$f || true); \
		begins=$$(grep -c "^inline bool .*BlockBegin(" $$f || true); \
		lines=$$(wc -l < $$out | tr -d ' '); \
		if [ "$$regions" != "$$ends" ] || [ "$$regions" != "$$begins" ]; then \
			echo "FILL REFUSER FAILED: $$f has $$regions begin markers, $$ends end markers and $$begins Begin functions — the markers, not the property, are what broke"; exit 1; \
		fi; \
		if [ "$$lines" -lt $$(( 40 * $$regions )) ]; then \
			echo "FILL REFUSER FAILED: $$f extracted $$lines lines for $$regions fill paths — the markers, not the property, are what broke"; exit 1; \
		fi; \
	done
	@if grep -nE "$(BLOCK_FORBIDDEN)" build/block-fill/*; then \
		echo "FILL REFUSER FAILED: the generated fill path allocates, locks or takes an atomic (docs/SPEC-TABLES.md §19.1)"; exit 1; \
	fi
	@echo "block fill refuser: the generated fill path allocates nothing, locks nothing, takes no atomic"

# NEGATIVE CONTROL ONE (the brief's (a)): the fill refuser above must be
# CAPABLE of going red. An allocation and a lock are injected into a generated
# fill path, and the gate must refuse. A green gate proves nothing until it has
# been shown able to fail.
.PHONY: tables-block-fill-refuser-negative-control
tables-block-fill-refuser-negative-control: build/tables-generated/.stamp
	@rm -rf build/block-fill-sabotage && mkdir -p build/block-fill-sabotage
	@cp build/tables-generated/block/RenderBlock.h build/block-fill-sabotage/
	@sed -i.bak 's|// ---- block fill path: begin ----|// ---- block fill path: begin ----\n// sabotaged: static std::mutex lock; void * p = malloc( 1 );|' \
		build/block-fill-sabotage/RenderBlock.h
	@grep -q "sabotaged: static std::mutex" build/block-fill-sabotage/RenderBlock.h || \
		{ echo "NEGATIVE CONTROL: the sabotage did not apply"; exit 1; }
	@awk '/---- block fill path: begin ----/,/---- block fill path: end ----/' \
		build/block-fill-sabotage/RenderBlock.h > build/block-fill-sabotage/region
	@if grep -qE "$(BLOCK_FORBIDDEN)" build/block-fill-sabotage/region; then \
		echo "block fill refuser negative control: the gate goes red on an injected lock and allocation"; \
	else \
		echo "NEGATIVE CONTROL FAILED: the fill refuser did not see an injected lock and allocation"; exit 1; \
	fi

# NEGATIVE CONTROL THREE (the brief's (c)): the C# side's GENERATED PADDING
# FIELDS are load-bearing, not decoration. Delete them from one padded record
# and the generated layout check must go red — every field after the hole lands
# at the wrong offset, which is exactly what §19.3 says Size alone cannot pin.
.PHONY: tables-block-padding-negative-control
tables-block-padding-negative-control: build/tables-generated-cs/.stamp
	@rm -rf build/block-padding-sabotage && cp -R build/tables-generated-cs/block build/block-padding-sabotage
	@sed -i.bak '/private byte _pad/d' build/block-padding-sabotage/PaddedBlock.cs
	@grep -q "private byte _pad" build/block-padding-sabotage/PaddedBlock.cs && \
		{ echo "NEGATIVE CONTROL: the sabotage did not apply"; exit 1; } || true
	@rm -f build/block-padding-sabotage/*.bak
	@if ( cd test/cs-block && dotnet run -p:BlockGeneratedDir=../../build/block-padding-sabotage ) \
		> build/block-padding-sabotage.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the C# layout check passed with the padding fields deleted"; \
		cat build/block-padding-sabotage.log; exit 1; \
	fi
	@grep -q "schema block layout" build/block-padding-sabotage.log || \
		{ echo "NEGATIVE CONTROL FAILED: the failure was not the layout check"; cat build/block-padding-sabotage.log; exit 1; }
	@echo "block padding negative control: deleting the generated padding fields turns the layout check red"

# §19.5's FIRST named negative control: "perturb one row type's pitch constant
# on one side only and the two-language test goes red". The constant is the C#
# side's ShipsStride, which the generated layout check asserts against this
# runtime's own sizeof — so a constant that drifted from the row it describes
# is caught where the drift would matter, and the control proves the assert is
# not inert.
.PHONY: tables-block-pitch-negative-control
tables-block-pitch-negative-control: build/tables-generated-cs/.stamp
	@rm -rf build/block-pitch-sabotage && cp -R build/tables-generated-cs/block build/block-pitch-sabotage
	@sed -i.bak 's|public const long ShipsStride = 88;|public const long ShipsStride = 96; // SABOTAGED|' \
		build/block-pitch-sabotage/RenderBlock.cs
	@grep -q "SABOTAGED" build/block-pitch-sabotage/RenderBlock.cs || \
		{ echo "NEGATIVE CONTROL: the sabotage did not apply"; exit 1; }
	@rm -f build/block-pitch-sabotage/*.bak
	@if ( cd test/cs-block && dotnet run -p:BlockGeneratedDir=../../build/block-pitch-sabotage ) \
		> build/block-pitch-sabotage.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the C# side accepted a pitch constant that is not its row's sizeof"; \
		cat build/block-pitch-sabotage.log; exit 1; \
	fi
	@grep -q "ShipsStride" build/block-pitch-sabotage.log || \
		{ echo "NEGATIVE CONTROL FAILED: it went red, but not on the pitch constant"; cat build/block-pitch-sabotage.log; exit 1; }
	@echo "block pitch negative control: a pitch constant perturbed on ONE side turns the C# layout check red"

# §19.5's SECOND named negative control: "perturb one field's offset in the
# compiler's layout model and the generated asserts go red on both backends".
# This one reaches past the emitters into ir.layoutRecord, so it proves the
# thing the other controls cannot: that the asserts check the COMPILER's model
# rather than restating whatever the target compiler already said.
#
# The sabotaged compiler reaches the build through `go build -overlay`, so no
# tracked file is ever written to — the same mechanism the big-endian negative
# control uses, and for the same reason.
.PHONY: tables-block-layout-model-negative-control
tables-block-layout-model-negative-control: bin/schema
	@mkdir -p build
	@sed 's|ml.Fields = append(ml.Fields, FieldLayout{Field: f, Offset: start, Size: offset - start, Align: fieldAlign})|ml.Fields = append(ml.Fields, FieldLayout{Field: f, Offset: start + 8, Size: offset - start, Align: fieldAlign}) // SABOTAGED: one field, moved|' \
		ir/blocklayout.go > build/blocklayout-moved.gotext
	@cmp -s build/blocklayout-moved.gotext ir/blocklayout.go && \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; } || true
	@printf '{"Replace":{"%s/ir/blocklayout.go":"%s/build/blocklayout-moved.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/blocklayout-overlay.json
	@go build -overlay=build/blocklayout-overlay.json -o build/schema-layout-moved ./cmd/schema
	@rm -rf build/block-model-sabotage && mkdir -p build/block-model-sabotage/cpp build/block-model-sabotage/cs
	@./build/schema-layout-moved generate --lang cpp --out build/block-model-sabotage/cpp tables/block
	@./build/schema-layout-moved generate --lang cs --out build/block-model-sabotage/cs tables/block
	@if $(CXX) $(BLOCK_CXXFLAGS) -fsyntax-only -Ibuild/block-model-sabotage/cpp \
		build/block-model-sabotage/cpp/RenderBlock.cpp > build/block-model-cpp.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: C++ accepted a moved offset from the compiler's model"; exit 1; \
	fi
	@grep -q "static_assert" build/block-model-cpp.log || \
		{ echo "NEGATIVE CONTROL FAILED: C++ went red, but not on a layout static_assert"; cat build/block-model-cpp.log; exit 1; }
	@echo "block layout-model negative control (C++): a moved offset in the compiler's model turns the static_asserts red"
	@if ( cd test/cs-block && dotnet run -p:BlockGeneratedDir=../../build/block-model-sabotage/cs ) \
		> build/block-model-cs.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: C# accepted a moved offset from the compiler's model"; \
		cat build/block-model-cs.log; exit 1; \
	fi
	@grep -q "schema block layout" build/block-model-cs.log || \
		{ echo "NEGATIVE CONTROL FAILED: C# went red, but not on the layout check"; cat build/block-model-cs.log; exit 1; }
	@echo "block layout-model negative control (C#): the same moved offset turns the once-run layout check red"

# THE BLOCK HOME's NEGATIVE CONTROL (docs/SPEC-TABLES.md §19.2's C# surface). The
# dogfood found two defects with one root cause — a C# backend that emitted per
# DECLARING FILE and skipped a file with no `table` in it: the unit's shared
# runtime went to the PROTOCOL ID's home, which declares no table in any real
# unit, and a blittable record went to its declaring file's Block.cs, which is
# never written when that file declares only `type`s. Neither produced a
# diagnostic; both produced a unit that does not compile.
#
# tables/blockhome has exactly that shape, and test/cs-block compiles it.
#
# THE HOME IS <Package>Block.cs NOW (docs/SPEC-TABLES.md §19.2), emitted FOR THE UNIT
# when no file of the unit is named for the package — which is what makes the
# home unconditional and this class of defect structural rather than a rule to
# keep. The sabotage is therefore the one line that guarantees it: take away
# the emitted-for-the-unit home, and the runtime and every blittable record
# have nowhere to land. The compile must FAIL.
.PHONY: tables-block-home-negative-control
tables-block-home-negative-control: bin/schema
	@mkdir -p build
	@sed -e 's|if home != "" \&\& !runtimeWritten {|if false \&\& !runtimeWritten { // SABOTAGED: no home is emitted for the unit|' \
		internal/codegen/cstable/block.go > build/csblock-declaring-file.gotext
	@cmp -s build/csblock-declaring-file.gotext internal/codegen/cstable/block.go && \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/codegen/cstable/block.go":"%s/build/csblock-declaring-file.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/csblock-overlay.json
	@go build -overlay=build/csblock-overlay.json -o build/schema-csblock-sabotaged ./cmd/schema
	@rm -rf build/blockhome-sabotage && mkdir -p build/blockhome-sabotage
	@./build/schema-csblock-sabotaged generate --lang cs --out build/blockhome-sabotage tables/blockhome
	@if ( cd test/cs-block && dotnet build -v q --nologo -p:BlockHomeGeneratedDir=../../build/blockhome-sabotage ) \
		> build/blockhome-sabotage.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the unit compiled with its block runtime emitted nowhere"; \
		cat build/blockhome-sabotage.log; exit 1; \
	fi
	@grep -qE "CS0103|CS0234|CS0246" build/blockhome-sabotage.log || \
		{ echo "NEGATIVE CONTROL FAILED: it went red, but not on an undefined name"; tail -20 build/blockhome-sabotage.log; exit 1; }
	@echo "block home negative control: a unit with no home for its block runtime does not compile"

# THE RUNTIME HOME IS THE PACKAGE (docs/SPEC-TABLES.md §19.2). A unit's shared C#
# runtime — the table runtime and the text form's walk in <Package>Table.cs, the
# block runtime and its blittable records in <Package>Block.cs, the cook runtime
# in <Package>Cook.cs — is emitted ONCE, and every earlier rule picked WHERE off
# the file order. Adding a file that sorts earlier then relocated ~2,000 lines:
# correct output, and a diff nobody can read (issue #347).
#
# The gate adds exactly such a file to a COPY of tables/examples — Aaa.schema,
# ahead of Guarded.schema — and requires the homes not to move. The table
# runtime is byte-identical across the two trees as well as same-named: it
# carries no build version (the zero-cost gate above), so a unit gaining a table
# cannot move it. The block and cook runtimes DO carry the build version, so
# their names are what is checked.
.PHONY: tables-runtime-home
tables-runtime-home: bin/schema
	@rm -rf build/runtime-home && mkdir -p build/runtime-home/src
	@cp tables/examples/*.schema build/runtime-home/src/
	@printf 'package tabledemo\n\ntable AaaRow\n{\n    tag uint8\n}\n' > build/runtime-home/src/Aaa.schema
	@./bin/schema generate --lang cs --out build/runtime-home/base tables/examples
	@./bin/schema generate --lang cs --out build/runtime-home/added build/runtime-home/src
	@for surface in Table Block Cook; do \
		base=$$(cd build/runtime-home/base && grep -l "the unit's shared runtime lives here" *$$surface.cs); \
		added=$$(cd build/runtime-home/added && grep -l "the unit's shared runtime lives here" *$$surface.cs); \
		if [ "$$base" != "Tabledemo$$surface.cs" ] || [ "$$added" != "Tabledemo$$surface.cs" ]; then \
			echo "RUNTIME HOME GATE FAILED: the $$surface runtime is in $$base before the added file and $$added after — expected Tabledemo$$surface.cs both times"; exit 1; \
		fi; \
	done
	@cmp -s build/runtime-home/base/TabledemoTable.cs build/runtime-home/added/TabledemoTable.cs || \
		{ echo "RUNTIME HOME GATE FAILED: the table runtime's bytes moved when the unit gained a file"; exit 1; }
	@echo "runtime home gate: the table, block and cook runtimes stay in <Package><Surface>.cs when an earlier-sorting file joins the unit"

.PHONY: tables-runtime-home-negative-control
tables-runtime-home-negative-control: bin/schema tables-runtime-home
	@sed 's|home := capitalize(u.Package)|home := ir.ProtocolIdHome(u) // SABOTAGED: back to the file order|' \
		internal/codegen/cstable/cstable.go > build/csruntime-fileorder.gotext
	@grep -q SABOTAGED build/csruntime-fileorder.gotext || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; }
	@printf '{"Replace":{"%s/internal/codegen/cstable/cstable.go":"%s/build/csruntime-fileorder.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/csruntime-overlay.json
	@go build -overlay=build/csruntime-overlay.json -o build/schema-csruntime-sabotaged ./cmd/schema
	@rm -rf build/runtime-home/base-sabotage build/runtime-home/added-sabotage
	@./build/schema-csruntime-sabotaged generate --lang cs --out build/runtime-home/base-sabotage tables/examples
	@./build/schema-csruntime-sabotaged generate --lang cs --out build/runtime-home/added-sabotage build/runtime-home/src
	@base=$$(cd build/runtime-home/base-sabotage && grep -l "the unit's shared runtime lives here" *Table.cs); \
	 added=$$(cd build/runtime-home/added-sabotage && grep -l "the unit's shared runtime lives here" *Table.cs); \
	 if [ "$$base" = "$$added" ]; then \
		echo "NEGATIVE CONTROL FAILED: the file-order rule kept the runtime in $$base — the gate is watching nothing"; exit 1; \
	 fi; \
	 echo "runtime home negative control: the file-order rule moves the runtime from $$base to $$added"

# DEFECT B's NEGATIVE CONTROL (docs/SPEC-TABLES.md §2.7's DEPTH ONE, BOUNDED ONLY).
# The dogfood found the C# blittable emitter projecting a bounded array INSIDE
# a nested record out of line — a sixteen-byte triple where C++ put the whole
# array, and every field after it somewhere else. Nothing said so until
# Verify() threw on the first Open, in the game.
#
# This puts the defect back — through `go build -overlay`, so no tracked file
# is written — and requires the gate to go red. The gate has TWO halves and
# either firing is it working: COMPILING every corpus unit's generated C# is
# half (a projected array is not the struct the rest of the unit indexes), and
# running each unit's Verify() is the other (a projected array moves the sizes
# and offsets). The control reports which one fired.
.PHONY: tables-block-inline-array-negative-control
tables-block-inline-array-negative-control: bin/schema
	@mkdir -p build
	@sed -e 's|if projection \&\& ir.BlockOutOfLine(f) {|if ir.BlockOutOfLine(f) { // SABOTAGED: project at every depth|' \
	     -e 's|inline := !projection \|\| !ir.BlockOutOfLine(f)|inline := !ir.BlockOutOfLine(f)|' \
		internal/codegen/cstable/block.go > build/csblock-depth.gotext
	@cmp -s build/csblock-depth.gotext internal/codegen/cstable/block.go && \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/codegen/cstable/block.go":"%s/build/csblock-depth.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/csblock-depth-overlay.json
	@go build -overlay=build/csblock-depth-overlay.json -o build/schema-depth-sabotaged ./cmd/schema
	@rm -rf build/blockhome-depth && mkdir -p build/blockhome-depth
	@./build/schema-depth-sabotaged generate --lang cs --out build/blockhome-depth tables/blockhome
	@if ( cd test/cs-block && dotnet run -p:BlockHomeGeneratedDir=../../build/blockhome-depth ) \
		> build/blockhome-depth.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a bounded array projected inside a nested record passed the layout gate"; \
		cat build/blockhome-depth.log; exit 1; \
	fi
	@if grep -q "schema block layout" build/blockhome-depth.log; then \
		echo "block inline-array negative control: the LAYOUT CHECK went red — a projected array inside a nested record moves the offsets"; \
	elif grep -qE "CS1061|CS0246|CS0117" build/blockhome-depth.log; then \
		echo "block inline-array negative control: the COMPILE went red — a projected array is not the struct the rest of the generated unit indexes"; \
	else \
		echo "NEGATIVE CONTROL FAILED: it went red, but not on either half of the gate"; tail -20 build/blockhome-depth.log; exit 1; \
	fi

# GATE 2 (docs/SPEC-TABLES.md §12.1): the MEASURED gate, two numbers, and it is not
# part of `make test` on purpose — a correctness suite whose verdict depends on
# the machine's mood is not a correctness suite, and the estate's bench rules
# want one bench at a time per machine and a quiet window.
#
# The C++ half writes the representative frame it measured into
# build/block_gate2.bin and the C# half reads THE SAME BYTES, so the two
# numbers are taken over one frame rather than over two descriptions of one.
# Each half runs its own golden gate first and REFUSES to bench on a mismatch.
build/schema_test_block_gate2: build/tables-generated/.stamp test/tables/block_gate2_main.cpp
	@mkdir -p build
	$(CXX) -O2 $(BLOCK_CXXFLAGS) $(BLOCK_INCLUDES) test/tables/block_gate2_main.cpp $(BLOCK_SOURCES) -o $@

.PHONY: tables-block-gate2
tables-block-gate2: build/schema_test_block_gate2 build/tables-generated-cs/.stamp
	./build/schema_test_block_gate2
	cd test/cs-block && dotnet run -c Release -- --gate2

# THE SMOKE, which is what CI runs (certification legs only, never a PR).
# The gate above is a MEASUREMENT and its verdict belongs on a quiet box; a
# gate nothing runs anywhere is a gate that rots, which is the other failure.
# So the correctness half — the two arms agreeing byte for byte across all
# nine sections, and the frame the C# half reads — runs on every push to main
# and nightly, at a small N, with the 5% band NOT enforced and both halves
# saying so. A drift in what the generated form WRITES goes red here; a slow
# runner does not.
.PHONY: tables-block-gate2-smoke
tables-block-gate2-smoke: build/schema_test_block_gate2 build/tables-generated-cs/.stamp
	./build/schema_test_block_gate2 --smoke
	cd test/cs-block && dotnet run -c Release -- --gate2-smoke
# The NEGATIVE CONTROL for a KEYED object's duplicate counting
# (docs/SPEC-TABLES.md §16.2). Last-wins inside a keyed object was already true, so
# the missing count was invisible to every round-trip test — the value was
# right and only the ledger was wrong. This sabotage removes the increment and
# the fixture must go red; without it, nothing in the suite could see the
# defect the pack engine's two-sided gate found.
.PHONY: tables-json-keyed-dup-negative-control
tables-json-keyed-dup-negative-control: bin/schema test/tables/json_keyed_dup_negative_main.cpp
	@rm -rf build/json-dup-sabotage && mkdir -p build/json-dup-sabotage
	./bin/schema generate --lang cpp --out build/json-dup-sabotage tables/examples
	@sed -i.bak 's|if ( ( seen\[slot >> 6\] \& bit ) != 0 ) { in.report->duplicate++; }|if ( ( seen[slot >> 6] \& bit ) != 0 ) { /* sabotaged: no count */ }|' build/json-dup-sabotage/KeyedTable.cpp
	@grep -q 'sabotaged: no count' build/json-dup-sabotage/KeyedTable.cpp || { echo "NEGATIVE CONTROL: the sabotage did not apply"; exit 1; }
	@mkdir -p build
	$(CXX) -std=c++17 -Wall -Wextra -Werror -ffp-contract=off \
		-Ibuild/json-dup-sabotage test/tables/json_keyed_dup_negative_main.cpp build/json-dup-sabotage/KeyedTable.cpp -o build/schema_test_json_keyed_dup_negative
	./build/schema_test_json_keyed_dup_negative

# THE NEGATIVE CONTROL FOR PER-CASE ABSENCE (test/conformance/README.md).
#
# An absence is a driver's own claim, so the mechanism could hide a real hole:
# a leg that went absent on a case it should answer would print a smaller pass
# and no failure at all. What stops that is the rule beside it — THE REFERENCE
# LEG MAY NOT ANSWER ABSENT — and this is the control that shows the rule is
# load-bearing rather than decorative.
#
# It sabotages the C++ driver into claiming absence for every instance whose
# unit is pointered, which is exactly what a port legitimately does, and
# requires the harness to go RED on the reference leg.
.PHONY: conformance-negative-control-absent
conformance-negative-control-absent: build/conformance-harness build/tables-generated/.stamp
	@mkdir -p build
	@sed -e 's|if ( variable != NULL )$$|if ( variable != NULL \&\& !spill( out, f[1] + ".absent", "", 0 ) ) { return 1; } /* SABOTAGED */\n        if ( variable != NULL ) { continue; }\n        if ( false )|' \
		test/conformance/cpp/main.cpp > build/conformance-cpp-absent.cpp
	@cmp -s build/conformance-cpp-absent.cpp test/conformance/cpp/main.cpp && \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; } || true
	$(CXX) $(TABLES_CXXFLAGS) $(CONFORMANCE_INCLUDES) build/conformance-cpp-absent.cpp \
		$(CONFORMANCE_SOURCES) -o build/conformance-cpp
	@if ./build/conformance-harness run --only cpp > build/conformance-absent.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the REFERENCE leg answered ABSENT and the harness stayed green"; \
		$(MAKE) build/conformance-cpp; exit 1; \
	fi
	@grep -q "REFERENCE leg answered ABSENT" build/conformance-absent.log || \
		{ echo "NEGATIVE CONTROL FAILED: it went red, but not on the reference rule"; \
		  cat build/conformance-absent.log; $(MAKE) build/conformance-cpp; exit 1; }
	@rm -f build/conformance-cpp
	@$(MAKE) build/conformance-cpp
	@echo "negative control: an ABSENCE from the reference leg turns the harness red"
# AND THE OTHER DIRECTION, which is the half a one-sided control would have
# missed and did: the rule belongs to the COMMITTED registry alone. A run handed
# a substituted one — the big-endian leg writes a single-driver file for the Go
# port — is one leg of a port, so its first line is not the reference and its
# absences are ordinary. Without this, the rule fires on every cross-endian run.
	@printf 'go test/conformance/go/driver\n' > build/conformance-solo-drivers.txt
	./build/conformance-harness run --drivers build/conformance-solo-drivers.txt --work build/conformance-solo-work
	@echo "negative control: the same absences under a SUBSTITUTED registry are ordinary, and the leg passes"

# THE SAME RULE AT THE COARSER GRAIN (schema#467): a WHOLE SURFACE the reference
# leg never registers.
#
# A reference leg that drops a surface from `list` — or exits 2 on it — leaves
# every other leg comparing against a surface nothing pins, which is the same
# hole a per-case absence there would leave and is red for the same reason. The
# success footer may not print beside it, and this is the control that says so.
#
# The sabotage is on the DRIVER rather than on an emitter (docs/PORTING.md I2)
# because `list` is the driver's own answer and nothing generates it: a WRAPPER
# around the committed reference driver, filtering one surface out of its list,
# planted as the reference of a scratch DISCOVERED registry — the rule belongs
# to that registry alone, so a substituted one would not exercise it. The
# wrapper refuses rather than filtering nothing, so a control that removed no
# surface says so instead of reading as a pass. No tracked file is written.
CONFORMANCE_REFERENCE_MISSING := build/conformance-reference-missing
CONFORMANCE_REFERENCE_SURFACE := block-dump
.PHONY: conformance-negative-control-reference-surface
conformance-negative-control-reference-surface: build/conformance-harness build/conformance-cpp build/schema_test_cook
	@rm -rf $(CONFORMANCE_REFERENCE_MISSING)
	@mkdir -p $(CONFORMANCE_REFERENCE_MISSING)/registry/cpp
	@printf '#!/bin/sh\nif [ "$$2" != list ]; then exec test/conformance/cpp/driver "$$@"; fi\ntest/conformance/cpp/driver "$$1" list > %s/listed.txt\ngrep -q "^$(CONFORMANCE_REFERENCE_SURFACE)$$" %s/listed.txt || { echo "the reference leg registers no $(CONFORMANCE_REFERENCE_SURFACE) surface: this control removes nothing" >&2; exit 1; }\ngrep -v "^$(CONFORMANCE_REFERENCE_SURFACE)$$" %s/listed.txt\n' \
		"$(CONFORMANCE_REFERENCE_MISSING)" "$(CONFORMANCE_REFERENCE_MISSING)" "$(CONFORMANCE_REFERENCE_MISSING)" \
		> $(CONFORMANCE_REFERENCE_MISSING)/registry/cpp/driver
	@chmod +x $(CONFORMANCE_REFERENCE_MISSING)/registry/cpp/driver
	@if ./build/conformance-harness run --drivers $(CONFORMANCE_REFERENCE_MISSING)/registry \
			--work $(CONFORMANCE_REFERENCE_MISSING)/work > $(CONFORMANCE_REFERENCE_MISSING)/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the REFERENCE leg registered no $(CONFORMANCE_REFERENCE_SURFACE) surface and the harness stayed green"; \
		cat $(CONFORMANCE_REFERENCE_MISSING)/log; exit 1; \
	fi
	@grep -q "REFERENCE leg is ABSENT on the whole $(CONFORMANCE_REFERENCE_SURFACE) surface" $(CONFORMANCE_REFERENCE_MISSING)/log || \
		{ echo "NEGATIVE CONTROL FAILED: it went red, but not on the reference rule"; \
		  cat $(CONFORMANCE_REFERENCE_MISSING)/log; exit 1; }
	@if grep -q "every registered surface passes" $(CONFORMANCE_REFERENCE_MISSING)/log; then \
		echo "NEGATIVE CONTROL FAILED: the success footer printed beside a reference ABSENT"; \
		cat $(CONFORMANCE_REFERENCE_MISSING)/log; exit 1; \
	fi
	@grep -q "wire          pass" $(CONFORMANCE_REFERENCE_MISSING)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the whole matrix went red, so it localises nothing"; \
		  cat $(CONFORMANCE_REFERENCE_MISSING)/log; exit 1; }
	@grep -m1 "cpp / $(CONFORMANCE_REFERENCE_SURFACE)" $(CONFORMANCE_REFERENCE_MISSING)/log
	@echo "negative control: a SURFACE the reference leg never registers turns the harness red"

# THE CROSS-IMPLEMENTATION LOCK for the FLAT NODE TABLE (docs/SPEC-TABLES.md
# §3.1), and it is what makes TWO implementations of one wire mean something.
#
# The compiler's engine (internal/tablewire) and the generated C++ backend were
# not written from each other. A golden proves each against its own past; this
# proves each against the OTHER, in both directions, over a graph carrying every
# shape the numbering has to get right — a shared node named from two places, a
# chain, a diamond that closes on a node already numbered, a variable table
# nested by value, and a null in a pointer-shaped slot.
#
#   C++ writes -> the TOOL cooks and uncooks it -> byte-identical
#   the TOOL writes -> C++ loads and re-saves it -> byte-identical
#
# The second direction also checks the SIZES: the region bytes and the
# attribution bytes C++ computes from the framing alone are the data and
# attribution parts the tool's cook writes.
.PHONY: tables-flat-wire
tables-flat-wire: build/tables-generated/.stamp bin/schema test/tables/flatwire_main.cpp
	@mkdir -p build/flatwire
	$(CXX) $(TABLES_CXXFLAGS) -Ibuild/tables-generated/pointers -Itest/tables \
		test/tables/flatwire_main.cpp -o build/schema_test_flatwire
	./build/schema_test_flatwire write build/flatwire/cpp.wire
	./bin/schema cook --root Scene --in build/flatwire/cpp.wire --out build/flatwire/cpp.cook --verbose tables/pointers
	./bin/schema uncook --root Scene --in build/flatwire/cpp.cook --out build/flatwire/cpp-back.wire tables/pointers
	cmp build/flatwire/cpp.wire build/flatwire/cpp-back.wire
	@echo "flat wire: C++ wrote it, the tool read it, and the bytes came back identical"
	@go build -o build/cookgen ./test/cookgen
	./build/cookgen --bytes 4096 --root Scene --ref head --chain ListNode --next next --values --out build/flatwire/tool.cook
	./bin/schema uncook --root Scene --in build/flatwire/tool.cook --out build/flatwire/tool.wire --verbose tables/pointers
	./build/schema_test_flatwire reload build/flatwire/tool.wire build/flatwire/tool-back.wire
	cmp build/flatwire/tool.wire build/flatwire/tool-back.wire
	@echo "flat wire: the tool wrote it, C++ read it, and the bytes came back identical"

# ITS NEGATIVE CONTROL: the numbering is a FIRST-VISIT order, and getting the
# order wrong is the defect a byte comparison exists to catch. Numbering a
# node's children before the node itself is a legal-looking walk that produces a
# different, self-consistent wire — so the C++ side still round-trips its own
# bytes, and only the CROSS-IMPLEMENTATION comparison goes red.
.PHONY: tables-flat-wire-negative-control
tables-flat-wire-negative-control: bin/schema test/tables/flatwire_main.cpp
	@mkdir -p build/flatwire
	@sed -e 's|if ( !TableNumberingAppend( numbering, node ) ) { return false; }\\n")|if ( !%sNumber( ctx, numbering, *pointee ) ) { return false; } // SABOTAGED\\n", t)|' \
	     -e 's|if ( !%sNumber( ctx, numbering, \*pointee ) ) { return false; }\\n", t)|if ( !TableNumberingAppend( numbering, node ) ) { return false; }\\n")|' \
		internal/codegen/cpptable/pointers.go > build/pointers-postorder.gotext
	@cmp -s build/pointers-postorder.gotext internal/codegen/cpptable/pointers.go && \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/codegen/cpptable/pointers.go":"%s/build/pointers-postorder.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/pointers-postorder-overlay.json
	@go build -overlay=build/pointers-postorder-overlay.json -o build/schema-postorder ./cmd/schema
	@rm -rf build/tables-postorder && mkdir -p build/tables-postorder
	./build/schema-postorder generate --lang cpp --out build/tables-postorder/pointers tables/pointers
	$(CXX) $(TABLES_CXXFLAGS) -Ibuild/tables-postorder/pointers -Itest/tables \
		test/tables/flatwire_main.cpp -o build/schema_test_flatwire_postorder
	./build/schema_test_flatwire_postorder write build/flatwire/postorder.wire
	@if cmp -s build/flatwire/postorder.wire build/flatwire/cpp.wire; then \
		echo "NEGATIVE CONTROL FAILED: numbering a node AFTER its children moved no byte"; exit 1; \
	fi
	@if ./bin/schema cook --root Scene --in build/flatwire/postorder.wire --out build/flatwire/postorder.cook tables/pointers >/dev/null 2>&1 && \
	    ./bin/schema uncook --root Scene --in build/flatwire/postorder.cook --out build/flatwire/postorder-back.wire tables/pointers >/dev/null 2>&1 && \
	    cmp -s build/flatwire/postorder.wire build/flatwire/postorder-back.wire; then \
		echo "NEGATIVE CONTROL FAILED: the tool agreed with a walk that is not first-visit pre-order"; exit 1; \
	fi
	@echo "negative control: numbering a node after its children turns the CROSS-IMPLEMENTATION lock red"

# The NEGATIVE CONTROL for the SHARED NODE (docs/SPEC-TABLES.md §6.2). Lock's
# whole claim is that its walk carries one entry per node, so a node two
# references name is packed ONCE and both references resolve to it. A pack that
# duplicates reads correct through either reference — every field is right, the
# graph is walkable, the region relocates — which is exactly why the defect
# lived under a green suite until the byte count was measured.
#
# It sabotages the EMITTER's identity map so that a CLOSED entry reads as a
# first visit again — which is exactly the defect, and no more than it: a
# reference to an OPEN entry is still the cycle it is, so the sabotage removes
# identity without removing the refusal that keeps the walk terminating. THE
# TABLES SUITE ITSELF must go red. The sabotaged emitter reaches the compiler
# through `go build -overlay`, so no tracked file is ever written to.
.PHONY: tables-shared-node-negative-control
tables-shared-node-negative-control: bin/schema
	@mkdir -p build
	@sed -e 's|taken = entry->key != key;|taken = entry->key != key \|\| entry->open == 0; // SABOTAGED: a CLOSED entry is a first visit again|' \
		internal/codegen/cpptable/arena.go > build/cpptable-no-identity.gotext
	@cmp -s build/cpptable-no-identity.gotext internal/codegen/cpptable/arena.go && \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/codegen/cpptable/arena.go":"%s/build/cpptable-no-identity.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/cpptable-no-identity-overlay.json
	@go build -overlay=build/cpptable-no-identity-overlay.json -o build/schema-no-identity ./cmd/schema
	@rm -rf build/tables-no-identity && mkdir -p build/tables-no-identity
	$(call tables_generate,./build/schema-no-identity,build/tables-no-identity)
	$(CXX) $(TABLES_CXXFLAGS) $(call tables_includes,build/tables-no-identity) \
		test/tables/main.cpp $$(ls build/tables-no-identity/*/*Table.cpp) -o build/schema_test_tables_no_identity
	@if ./build/schema_test_tables_no_identity > build/tables-no-identity.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a pack with no identity map left the tables suite GREEN"; exit 1; \
	fi
	@grep -q "^FAIL test/tables/main.cpp" build/tables-no-identity.log || \
		{ echo "NEGATIVE CONTROL FAILED: the suite went red, but not on a CHECK"; cat build/tables-no-identity.log; exit 1; }
	@echo "negative control: a pack with no identity map turns the TABLES SUITE red — $$(grep -c '^FAIL' build/tables-no-identity.log) failures"

# The NEGATIVE CONTROL for a keyed array's ITERATION RANGE (docs/SPEC-TABLES.md
# §2.4). The iteration's whole promise is that it walks EVERY stored slot and
# yields the KEY it holds, 1..E.Max; an off-by-one at either end reads as an
# ordinary walk, because every untouched slot holds the same declared defaults.
#
# It sabotages the EMITTER's begin() past the first stored slot and requires
# THE TABLES SUITE ITSELF — the same test/tables/main.cpp the leg runs, against
# a whole corpus regenerated from the sabotaged compiler — to go red. A
# purpose-written fixture would only prove the sabotage is observable by a
# fixture written for it; what has to be shown is that the GUARDED test reddens.
#
# The sabotaged emitter reaches the compiler through `go build -overlay`, so no
# tracked file is ever written to (the big-endian control's rule).
.PHONY: tables-keyed-iteration-negative-control
tables-keyed-iteration-negative-control: bin/schema
	@mkdir -p build
	@sed -e 's|Iterator begin() { return Iterator{ slots, 0 }; }|Iterator begin() { return Iterator{ slots, 1 }; } // SABOTAGED|' \
	     -e 's|ConstIterator begin() const { return ConstIterator{ slots, 0 }; }|ConstIterator begin() const { return ConstIterator{ slots, 1 }; } // SABOTAGED|' \
		internal/codegen/cpptable/cpptable.go > build/cpptable-first-slot.gotext
	@cmp -s build/cpptable-first-slot.gotext internal/codegen/cpptable/cpptable.go && \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/codegen/cpptable/cpptable.go":"%s/build/cpptable-first-slot.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/cpptable-first-slot-overlay.json
	@go build -overlay=build/cpptable-first-slot-overlay.json -o build/schema-first-slot ./cmd/schema
	@rm -rf build/tables-first-slot && mkdir -p build/tables-first-slot
	$(call tables_generate,./build/schema-first-slot,build/tables-first-slot)
	$(CXX) $(TABLES_CXXFLAGS) $(call tables_includes,build/tables-first-slot) \
		test/tables/main.cpp $$(ls build/tables-first-slot/*/*Table.cpp) -o build/schema_test_tables_first_slot
	@if ./build/schema_test_tables_first_slot > build/tables-first-slot.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: begin() past the first stored slot left the tables suite GREEN"; exit 1; \
	fi
	@grep -q "^FAIL test/tables/main.cpp" build/tables-first-slot.log || \
		{ echo "NEGATIVE CONTROL FAILED: the suite went red, but not on a CHECK"; cat build/tables-first-slot.log; exit 1; }
	@echo "negative control: begin() past the first stored slot turns the TABLES SUITE red — $$(grep -c '^FAIL' build/tables-first-slot.log) failures"

# THE None REFUSAL, HELD UNDER -DNDEBUG (docs/SPEC-TABLES.md §2.4). The refusal is
# unconditional by ruling: indexing a keyed array by None is a program error in
# EVERY configuration, because the shifted storage has no slot for None and a
# build that let the index through would read one element BEFORE the array.
#
# The tables suite proves the refusal fires, but it compiles with asserts LIVE,
# so it cannot tell an unconditional refusal from an assert. This gate compiles
# ONE translation unit -DNDEBUG — the configuration a game ships, and the one
# that removes an assert — and requires the None index to end the program
# there too.
.PHONY: tables-keyed-none-refusal-ndebug
tables-keyed-none-refusal-ndebug: build/tables-generated/.stamp test/tables/keyed_none_ndebug_main.cpp
	@mkdir -p build
	$(CXX) $(TABLES_CXXFLAGS) -DNDEBUG -Ibuild/tables-generated/examples \
		test/tables/keyed_none_ndebug_main.cpp -o build/schema_test_keyed_none_ndebug
	./build/schema_test_keyed_none_ndebug

# THE HOOKS (docs/SPEC-TABLES.md §13.9, docs/USAGE.md): a consumer supplies its
# own assert, its own fatal handler and its own allocate/free pair, and every
# call the table runtime makes has to land in them. This unit defines all four
# before it includes a generated header, so it observes what a consumer
# observes.
#
# It carries its OWN negative control rather than a sabotage build, and that is
# the stronger form here: the DEFAULT pair — schema_allocate / schema_release —
# is where a bypassing malloc, calloc, realloc or free would land, and the test
# defines it to a separate counter that must read ZERO. Put one C-library call
# back anywhere in the arena, the pack walk, the numbering, the region or the
# node directory and that counter fires.
.PHONY: tables-hooks
tables-hooks: build/tables-generated/.stamp test/tables/hooks_main.cpp
	@mkdir -p build
	$(CXX) $(TABLES_CXXFLAGS) $(TABLES_INCLUDES) test/tables/hooks_main.cpp \
		$$(ls build/tables-generated/pointers/*Table.cpp build/tables-generated/blobs/*Table.cpp) -o build/schema_test_hooks
	./build/schema_test_hooks

# and its NEGATIVE CONTROL: put the refusal back to a bare assert — the shape
# the ruling replaced — and the gate above must go RED, because -DNDEBUG then
# removes it. A gate that only ever passes proves nothing about what it checks.
.PHONY: tables-keyed-none-refusal-negative-control
tables-keyed-none-refusal-negative-control: bin/schema test/tables/keyed_none_ndebug_main.cpp
	@mkdir -p build
	@sed -e 's|            schema_fatal();|            /* SABOTAGED: a debug-only guard again */|' \
		internal/codegen/cpptable/cpptable.go > build/cpptable-assert-only.gotext
	@grep -q SABOTAGED build/cpptable-assert-only.gotext || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; }
	@printf '{"Replace":{"%s/internal/codegen/cpptable/cpptable.go":"%s/build/cpptable-assert-only.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/cpptable-assert-only-overlay.json
	@go build -overlay=build/cpptable-assert-only-overlay.json -o build/schema-assert-only ./cmd/schema
	@rm -rf build/tables-assert-only && mkdir -p build/tables-assert-only
	@./build/schema-assert-only generate --lang cpp --out build/tables-assert-only tables/examples
	@$(CXX) $(TABLES_CXXFLAGS) -DNDEBUG -Ibuild/tables-assert-only \
		test/tables/keyed_none_ndebug_main.cpp -o build/schema_test_keyed_none_assert_only
	@if ./build/schema_test_keyed_none_assert_only > build/keyed-none-assert-only.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a debug-only guard left the -DNDEBUG gate GREEN"; exit 1; \
	fi
	@grep -q "the refusal was compiled out" build/keyed-none-assert-only.log || \
		{ echo "NEGATIVE CONTROL FAILED: the gate went red, but not on the refusal"; cat build/keyed-none-assert-only.log; exit 1; }
	@echo "negative control: a debug-only guard turns the -DNDEBUG refusal gate red"

# THE OTHER END OF THE SAME REFUSAL, HELD UNDER -DNDEBUG (docs/SPEC-TABLES.md
# §2.4, issue #377). The storage holds one slot per NAMED variant, so a key
# past Max names a variant this enum does not have: the same program error as
# None, refused at the same compare, in every configuration. A build that let
# it through would read PAST THE END of the storage.
.PHONY: tables-keyed-max-refusal-ndebug
tables-keyed-max-refusal-ndebug: build/tables-generated/.stamp test/tables/keyed_max_ndebug_main.cpp
	@mkdir -p build
	$(CXX) $(TABLES_CXXFLAGS) -DNDEBUG -Ibuild/tables-generated/examples \
		test/tables/keyed_max_ndebug_main.cpp -o build/schema_test_keyed_max_ndebug
	./build/schema_test_keyed_max_ndebug

# and its NEGATIVE CONTROL: put the accessor back to the None-ONLY compare —
# what it refused before #377 — and the gate above must go RED, because a key
# past Max then indexes past the end. Deleting the abort would turn both gates
# red at once and prove nothing about THIS end, so the sabotage is the compare
# itself.
.PHONY: tables-keyed-max-refusal-negative-control
tables-keyed-max-refusal-negative-control: bin/schema test/tables/keyed_max_ndebug_main.cpp
	@mkdir -p build
	@sed -e 's|        if ( (uint32_t) ( (int32_t) key - 1 ) >= (uint32_t) kSlots )|        if ( key == E::None ) /* SABOTAGED: the None-only compare again */|' \
		internal/codegen/cpptable/cpptable.go > build/cpptable-none-only.gotext
	@grep -q SABOTAGED build/cpptable-none-only.gotext || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; }
	@printf '{"Replace":{"%s/internal/codegen/cpptable/cpptable.go":"%s/build/cpptable-none-only.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/cpptable-none-only-overlay.json
	@go build -overlay=build/cpptable-none-only-overlay.json -o build/schema-none-only ./cmd/schema
	@rm -rf build/tables-none-only && mkdir -p build/tables-none-only
	@./build/schema-none-only generate --lang cpp --out build/tables-none-only tables/examples
	@$(CXX) $(TABLES_CXXFLAGS) -DNDEBUG -Ibuild/tables-none-only \
		test/tables/keyed_max_ndebug_main.cpp -o build/schema_test_keyed_max_none_only
	@if ./build/schema_test_keyed_max_none_only > build/keyed-max-none-only.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a None-only compare left the past-Max gate GREEN"; exit 1; \
	fi
	@grep -q "the refusal was compiled out" build/keyed-max-none-only.log || \
		{ echo "NEGATIVE CONTROL FAILED: the gate went red, but not on the refusal"; cat build/keyed-max-none-only.log; exit 1; }
	@echo "negative control: the None-only compare turns the past-Max refusal gate red"

# The NEGATIVE CONTROL for the SHIFT itself (docs/SPEC-TABLES.md §2.4, owner ruling
# 2026-09-03). The storage holds E.Max slots with the key k at index k-1 and
# nothing for None. Putting the None slot BACK — E.Max + 1 slots, no shift —
# is the exact edit the ruling reversed, and it must not be able to pass
# quietly: the compiler computes the layout from E.Max and both backends assert
# it (§19.3), so a storage type one element wider than the compiler says
# must fail to COMPILE, and the corpus's own sizeof checks must fail too.
#
# The whole point is that this is caught by the GATES already in the tree — the
# block projection's static_asserts and the tables suite — rather than by a
# fixture written to notice it.
.PHONY: tables-keyed-shift-negative-control
tables-keyed-shift-negative-control: bin/schema
	@mkdir -p build
	@sed -e 's|static constexpr int32_t kSlots = (int32_t) E::Max;|static constexpr int32_t kSlots = (int32_t) E::Max + 1; // SABOTAGED: None'"'"'s slot back|' \
	     -e 's|return slots\[ (int32_t) key - 1 \];|return slots[ (int32_t) key ]; // SABOTAGED: no shift|' \
	     -e 's|Entry operator\*() const { return Entry{ (E) ( index + 1 ), slots\[index\] }; }|Entry operator*() const { return Entry{ (E) index, slots[index] }; } // SABOTAGED|' \
	     -e 's|ConstEntry operator\*() const { return ConstEntry{ (E) ( index + 1 ), slots\[index\] }; }|ConstEntry operator*() const { return ConstEntry{ (E) index, slots[index] }; } // SABOTAGED|' \
	     -e 's|Iterator begin() { return Iterator{ slots, 0 }; }|Iterator begin() { return Iterator{ slots, 1 }; } // SABOTAGED|' \
	     -e 's|ConstIterator begin() const { return ConstIterator{ slots, 0 }; }|ConstIterator begin() const { return ConstIterator{ slots, 1 }; } // SABOTAGED|' \
		internal/codegen/cpptable/cpptable.go > build/cpptable-none-slot.gotext
	@test $$(grep -c SABOTAGED build/cpptable-none-slot.gotext) -eq 7 || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched $$(grep -c SABOTAGED build/cpptable-none-slot.gotext) of 7 sites"; exit 1; }
	@printf '{"Replace":{"%s/internal/codegen/cpptable/cpptable.go":"%s/build/cpptable-none-slot.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/cpptable-none-slot-overlay.json
	@go build -overlay=build/cpptable-none-slot-overlay.json -o build/schema-none-slot ./cmd/schema
	@rm -rf build/tables-none-slot && mkdir -p build/tables-none-slot
	$(call tables_generate,./build/schema-none-slot,build/tables-none-slot)
	@# the LAYOUT GATE: the block corpus asserts the compiler's own offsets and
	@# sizes, and a keyed array one element wider moves them
	@if $(CXX) $(BLOCK_CXXFLAGS) -Ibuild/tables-none-slot/block -Ibuild/tables-none-slot/blockhome \
			-fsyntax-only build/tables-none-slot/block/PaddedBlock.cpp > build/tables-none-slot-layout.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the None slot back left the BLOCK LAYOUT GATE green"; exit 1; \
	fi
	@grep -q "projection" build/tables-none-slot-layout.log || \
		{ echo "NEGATIVE CONTROL FAILED: the block build went red, but not on a layout assert"; cat build/tables-none-slot-layout.log; exit 1; }
	@# and the SIZEOF assertions in the tables suite itself
	@if $(CXX) $(TABLES_CXXFLAGS) $(call tables_includes,build/tables-none-slot) \
			test/tables/main.cpp $$(ls build/tables-none-slot/*/*Table.cpp) -o build/schema_test_tables_none_slot \
			> build/tables-none-slot-build.log 2>&1 && \
		./build/schema_test_tables_none_slot > build/tables-none-slot.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the None slot back left the tables suite GREEN"; exit 1; \
	fi
	@echo "negative control: the None slot back turns the BLOCK LAYOUT GATE and the tables suite red"

# THE CLAMP-AT-STORAGE-LIMITS GATE (docs/SPEC-TABLES.md §4). A read clamps a
# value to the field's declared range; a bound sitting ON its storage type's
# own limit describes a check with no false case, and the emitter does not
# write it — the same test that already drops a bits(N) width clamp when N is
# the storage width.
#
# That rule is a BUILD rule and not only a shape: a tautological comparison is
# an ERROR under the repo's own flags, and the two compilers split the work.
# gcc reds the unsigned half — `decoded_v < 0ull`, "comparison of unsigned
# expression in '< 0' is always false", -Wtype-limits, which -Wextra implies
# and this names anyway. clang reds the signed half — `decoded_v < -128`,
# -Wtautological-type-limit-compare. So a tree carrying such a field can read
# clean on one platform and fail on the other, which is how it stayed
# invisible until the tables bench corpus met it.
#
# tables/examples/Ranges.schema declares every integer width four ways — both
# bounds at the limits, each limit alone, and one value off each end — so all
# four emission shapes are compiled here under both diagnostics.
CLAMP_CXXFLAGS := -std=c++17 -Wall -Wextra -Werror -Wtype-limits -ffp-contract=off

.PHONY: tables-clamp-limits
tables-clamp-limits: build/tables-generated/.stamp
	@mkdir -p build
	@printf '#include "RangesTable.h"\n' > build/clamp-limits-tu.cpp
	$(CXX) $(CLAMP_CXXFLAGS) -Ibuild/tables-generated/examples -fsyntax-only build/clamp-limits-tu.cpp
	@echo "clamp-at-storage-limits gate: the bounded corpus carries no comparison that cannot fire"

# The negative control puts the PRE-FIX emission back through `go build
# -overlay`, so no tracked file is written: the two comparisons that decide
# which ends are live become non-strict, which is always true given the
# checker already refuses a bound outside its storage — every declared end
# written, storage limit or not.
.PHONY: tables-clamp-limits-negative-control
tables-clamp-limits-negative-control: bin/schema
	@mkdir -p build
	@sed 's|return rlo.Cmp(lo) > 0, rhi.Cmp(hi) < 0|return rlo.Cmp(lo) >= 0, rhi.Cmp(hi) <= 0 // SABOTAGED: both ends always written|' \
		internal/codegen/cpptable/codecs.go > build/cpptable-always-clamp.gotext
	@grep -q SABOTAGED build/cpptable-always-clamp.gotext || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; }
	@printf '{"Replace":{"%s/internal/codegen/cpptable/codecs.go":"%s/build/cpptable-always-clamp.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/cpptable-always-clamp-overlay.json
	@go build -overlay=build/cpptable-always-clamp-overlay.json -o build/schema-always-clamp ./cmd/schema
	@rm -rf build/clamp-sabotage && mkdir -p build/clamp-sabotage
	@./build/schema-always-clamp generate --lang cpp --out build/clamp-sabotage tables/examples
	@# the rest of the corpus is UNMOVED by the sabotage — no other declared
	@# bound in it sits on a storage limit, which is why the defect survived
	@cmp -s build/clamp-sabotage/TablesTable.h build/tables-generated/examples/TablesTable.h || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage moved a file it has no business moving"; exit 1; }
	@printf '#include "RangesTable.h"\n' > build/clamp-limits-tu.cpp
	@if $(CXX) $(CLAMP_CXXFLAGS) -Ibuild/clamp-sabotage -fsyntax-only build/clamp-limits-tu.cpp \
			> build/clamp-limits-negative.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the pre-fix emission compiled clean — this compiler diagnoses neither half"; exit 1; \
	fi
	@grep -q "always false" build/clamp-limits-negative.log || \
		{ echo "NEGATIVE CONTROL FAILED: the build went red, but not on a comparison that cannot fire"; \
		  cat build/clamp-limits-negative.log; exit 1; }
	@echo "negative control: the storage-limit clamps back turn the build red — $$(grep -c 'always false' build/clamp-limits-negative.log) comparisons that cannot fire"

# THE ZERO-RANGE GATE and its NEGATIVE CONTROL (SPEC §4.6). A field whose
# declared range excludes zero and declares no default is born outside its own
# range: zero initialization is the language's rule, so the fresh value is one
# the wire cannot carry. The checker refuses that shape; the refusal's corpus
# case lives in the break-the-language suite.
#
# The control shows the refusal is load-bearing. It takes the gate back out
# through `go build -overlay` (no tracked file is written), generates the
# refused unit, and compiles it against NDEBUG — the shipping configuration,
# where the write-side range assert is gone and the defect is silent. A
# freshly constructed value must fail to survive its own wire.
.PHONY: check-zero-range-negative-control
check-zero-range-negative-control: bin/schema test/zero_range_negative_main.cpp
	@mkdir -p build/zero-range
	@printf 'package rangezero\n\ntype Probe\n{\n    x    uint8 | min = 1, max = 255\n    tail uint8\n}\n' > build/zero-range/RangeZero.schema
	@if ./bin/schema check build/zero-range > build/zero-range-check.log 2>&1; then \
		echo "GATE FAILED: the checker accepted a [1, 255] field with no declared default"; exit 1; \
	fi
	@grep -q "excludes zero" build/zero-range-check.log || \
		{ echo "GATE FAILED: it was refused, but not for excluding zero"; cat build/zero-range-check.log; exit 1; }
	@sed 's|c.requireDefaultInRange(f, out)|// SABOTAGED: the zero-range gate removed|' \
		internal/check/check.go > build/check-no-zero-gate.gotext
	@grep -q SABOTAGED build/check-no-zero-gate.gotext || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; }
	@printf '{"Replace":{"%s/internal/check/check.go":"%s/build/check-no-zero-gate.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/check-no-zero-gate-overlay.json
	@go build -overlay=build/check-no-zero-gate-overlay.json -o build/schema-no-zero-gate ./cmd/schema
	@rm -rf build/zero-range-sabotage && mkdir -p build/zero-range-sabotage
	@./build/schema-no-zero-gate generate --lang cpp --out build/zero-range-sabotage build/zero-range
	@grep -q 'uint8_t x = 0; // wire \[1, 255\]' build/zero-range-sabotage/RangeZero.h || \
		{ echo "NEGATIVE CONTROL FAILED: the gateless compiler did not emit the out-of-range initializer"; exit 1; }
	$(CXX) -std=c++17 -Wall -Wextra -Werror -ffp-contract=off -DNDEBUG \
		-I$(SERIALIZE) -Ibuild/zero-range-sabotage \
		test/zero_range_negative_main.cpp -o build/schema_test_zero_range_negative
	./build/schema_test_zero_range_negative

# THE VARIANT-ORDER NEGATIVE CONTROL (SPEC §3.1, issue #462). An enum value
# rides as its declaration ordinal and a flags variant as its bit position, so
# the projection carries both declarations' variant names in declaration order:
# without them a reorder is invisible and two builds either side of an
# alphabetized enum hold ONE id while every ordinal means something else.
#
# The control takes the variant lists back out of a COPY of the rendering
# through `go test -overlay` — no tracked file is written — and the reorder
# gate must go RED, on that surface and NOT on the union's, whose order rides
# in its payload types and must stay green under the same sabotage.
.PHONY: projection-variant-order-negative-control
projection-variant-order-negative-control:
	@mkdir -p build
	@sed -e 's|fmt.Fprintf(&b, "  variant %d name=.*, i+1, v)$$|_, _ = i, v // SABOTAGED: the enum variant list removed|' \
		-e 's|fmt.Fprintf(&b, "  bit %d name=.*|_, _ = i, v // SABOTAGED: the flags variant list removed|' \
		ir/projection.go > build/projection-no-variants.gotext
	@[ "$$(grep -c SABOTAGED build/projection-no-variants.gotext)" = "2" ] || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage did not remove both variant lists"; exit 1; }
	@printf '{"Replace":{"%s/ir/projection.go":"%s/build/projection-no-variants.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/projection-no-variants-overlay.json
	@if go test -count=1 -overlay=build/projection-no-variants-overlay.json \
		./internal/check -run TestIdMovesUnderVariantOrder > build/projection-no-variants.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the reorder gate passed with the variant lists gone"; \
		cat build/projection-no-variants.log; exit 1; \
	fi
	@grep -q "an enum reordered did NOT move the protocol id" build/projection-no-variants.log || \
		{ echo "NEGATIVE CONTROL FAILED: it went red, but not on the enum reorder"; \
		  cat build/projection-no-variants.log; exit 1; }
	@grep -q "a flags declaration reordered did NOT move the protocol id" build/projection-no-variants.log || \
		{ echo "NEGATIVE CONTROL FAILED: it went red, but not on the flags reorder"; \
		  cat build/projection-no-variants.log; exit 1; }
	@go test -count=1 -overlay=build/projection-no-variants-overlay.json \
		./internal/check -run 'TestUnionId' > build/projection-no-variants-union.log 2>&1 || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage reddened the union gate too — it is not surgical"; \
		  cat build/projection-no-variants-union.log; exit 1; }
	@echo "negative control: without the variant lists the enum and flags reorders go silent, and the union gate stays green"

# THE CODEC LAW NEGATIVE CONTROL (SPEC §3.1, issue #463). The projection's
# second version line is what a compiler change that moves the BYTES under an
# unchanged shape rides on — the 2026-08-15 rounding amendment moved bytes with
# every id unmoved, which is a false match. Take the line out of a COPY of the
# rendering and both law gates must go red, while the variant-order gate,
# which the line has nothing to do with, stays green.
.PHONY: projection-wire-law-negative-control
projection-wire-law-negative-control:
	@mkdir -p build
	@sed -e 's|fmt.Fprintf(&b, "schema-wire-law .*|// SABOTAGED: the codec law line removed|' \
		ir/projection.go > build/projection-no-wire-law.gotext
	@grep -q SABOTAGED build/projection-no-wire-law.gotext || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage did not remove the codec law line"; exit 1; }
	@printf '{"Replace":{"%s/ir/projection.go":"%s/build/projection-no-wire-law.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/projection-no-wire-law-overlay.json
	@if go test -count=1 -overlay=build/projection-no-wire-law-overlay.json \
		./internal/check -run TestWireLawLineMovesTheId > build/projection-no-wire-law.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the law gate passed with the line gone"; \
		cat build/projection-no-wire-law.log; exit 1; \
	fi
	@grep -q "the projection must open with its rendering version and then its codec law" build/projection-no-wire-law.log || \
		{ echo "NEGATIVE CONTROL FAILED: it went red, but not on the missing law line"; \
		  cat build/projection-no-wire-law.log; exit 1; }
	@if go test -count=1 -overlay=build/projection-no-wire-law-overlay.json \
		./internal/goldens -run TestWireLawBumpMovesEveryId > build/projection-no-wire-law-corpus.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: every corpus id survived the line's removal in silence"; \
		cat build/projection-no-wire-law-corpus.log; exit 1; \
	fi
	@grep -q "does not carry the codec law line" build/projection-no-wire-law-corpus.log || \
		{ echo "NEGATIVE CONTROL FAILED: the corpus gate went red for another reason"; \
		  cat build/projection-no-wire-law-corpus.log; exit 1; }
	@go test -count=1 -overlay=build/projection-no-wire-law-overlay.json \
		./internal/check -run TestIdMovesUnderVariantOrder > build/projection-no-wire-law-variants.log 2>&1 || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage reddened the variant-order gate too — it is not surgical"; \
		  cat build/projection-no-wire-law-variants.log; exit 1; }
	@echo "negative control: without the codec law line both law gates go red, and the variant-order gate stays green"

# THE UNION ARM-ORDER NEGATIVE CONTROL (SPEC §3.1, §4.8, issue #491). A union's
# arm order rides in its payload types only while the arms DIFFER in type: two
# arms of one type reorder with every projected type unmoved, so the arm names
# project beside them. Take the names out of a COPY of the rendering and the
# same-typed reorder goes silent, while the enum and flags gate — which the arm
# names have nothing to do with — stays green.
.PHONY: projection-union-arm-order-negative-control
projection-union-arm-order-negative-control:
	@mkdir -p build
	@sed -E -e 's|"  variant %d name=%s payload=|"  variant %d payload=|' \
		-e 's|i\+1, v\.Name, v\.Type\)|i+1, v.Type) // SABOTAGED: the arm names removed|' \
		ir/projection.go > build/projection-no-arm-names.gotext
	@grep -q SABOTAGED build/projection-no-arm-names.gotext || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage did not remove the arm names"; exit 1; }
	@printf '{"Replace":{"%s/ir/projection.go":"%s/build/projection-no-arm-names.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/projection-no-arm-names-overlay.json
	@if go test -count=1 -overlay=build/projection-no-arm-names-overlay.json \
		./internal/check -run 'TestUnionId' > build/projection-no-arm-names.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the union gates passed with the arm names gone"; \
		cat build/projection-no-arm-names.log; exit 1; \
	fi
	@grep -q "two arms of one payload type reordered and the id did not move" build/projection-no-arm-names.log || \
		{ echo "NEGATIVE CONTROL FAILED: it went red, but not on the same-typed reorder"; \
		  cat build/projection-no-arm-names.log; exit 1; }
	@grep -q "an arm renamed (arm order is spelled in names) did NOT move the protocol id" build/projection-no-arm-names.log || \
		{ echo "NEGATIVE CONTROL FAILED: it went red, but not on the arm rename"; \
		  cat build/projection-no-arm-names.log; exit 1; }
	@go test -count=1 -overlay=build/projection-no-arm-names-overlay.json \
		./internal/check -run TestIdMovesUnderVariantOrder > build/projection-no-arm-names-variants.log 2>&1 || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage reddened the variant-order gate too — it is not surgical"; \
		  cat build/projection-no-arm-names-variants.log; exit 1; }
	@echo "negative control: without the arm names a same-typed union reorder goes silent, and the variant-order gate stays green"

# Deliberately compiled WITHOUT -I$(SERIALIZE): the generated Table headers
# carry no serialize dependency, and this build proves it stays that way.
#
# TABLES_INCLUDES is shared with the sanitized twin below, so the two builds
# can never drift into covering different code.
TABLES_INCLUDES := $(call tables_includes,build/tables-generated)
TABLES_CXXFLAGS := -std=c++17 -Wall -Wextra -Werror -Wshadow -ffp-contract=off -pthread

# The text form's runtime is a generated TRANSLATION UNIT now, not header
# content (docs/SPEC-TABLES.md §16.1): a consumer that calls FromJson/ToJson
# compiles the generated <Base>Table.cpp, and one that never does compiles
# nothing for it. Expanded in the recipe because these are build-time output.
TABLES_JSON_SOURCES = $$(ls build/tables-generated/*/*Table.cpp)

build/schema_test_tables: build/tables-generated/.stamp test/tables/main.cpp
	@mkdir -p build
	$(CXX) $(TABLES_CXXFLAGS) $(TABLES_INCLUDES) test/tables/main.cpp $(TABLES_JSON_SOURCES) -o $@

# The SANITIZED twin (issue #277). The tables leg is where the pointer
# machinery lives — an arena whose Lock() frees it one way, a packed region
# read through self-relative deltas, a cooked file Open validates by walking
# an offset graph, a builder that goes wide across threads — and those are
# lifetime and bounds questions -Werror cannot see. A heap-use-after-free
# lived here with CI green over it, because the assertion it corrupted
# happened to compare equal every time (#276).
#
# -fno-sanitize-recover=all is what makes it a GATE: UBSan's default is to
# print and continue, which exits 0 and proves nothing.
#
# Both builds run, and that is not duplication: ASan replaces the allocator,
# so the two see different addresses and alignments — and this suite refuses
# unaligned bases and misaligned region references by design. Each build
# reaches states the other does not.
build/schema_test_tables_asan: build/tables-generated/.stamp test/tables/main.cpp
	@mkdir -p build
	$(CXX) $(TABLES_CXXFLAGS) -fsanitize=address,undefined -fno-sanitize-recover=all \
		-fno-omit-frame-pointer -g $(TABLES_INCLUDES) test/tables/main.cpp $(TABLES_JSON_SOURCES) -o $@

# ---- the BIG-ENDIAN leg (docs/SPEC-TABLES.md §3 and §19.1) ----------------------
#
# The wire is little-endian and byte-oriented (§3), and a block is produced in
# the byte order of the build that wrote it (§19.1). Both were rules on a page:
# every host this repo builds on is little-endian, so nothing ever read a
# golden the other way round. This leg builds the tables battery for a
# BIG-ENDIAN target and runs it under an emulator, which turns the two rules
# into gates — the goldens a little-endian host wrote are loaded, re-written
# and byte-compared by a big-endian reader, and a block that crosses the byte
# order is proven to refuse rather than to garble.
#
# The toolchain is not a system binary and is not assumed: CI installs an
# exact pinned version (.github/workflows/ci.yml) and these two variables name
# what it installed, so the leg runs anywhere the same pair is on PATH.
BE_CXX ?= s390x-linux-gnu-g++
BE_RUN ?= qemu-s390x

# Same flags and includes as the plain leg, shared through the same variables
# for the same reason the sanitized twin shares them (#278): a twin that
# covers different code is not a twin. -static so the runner needs no sysroot
# and the emulator invocation is just the binary.
build/schema_test_tables_be: build/tables-generated/.stamp test/tables/main.cpp
	@mkdir -p build
	$(BE_CXX) $(TABLES_CXXFLAGS) -static $(TABLES_INCLUDES) test/tables/main.cpp $(TABLES_JSON_SOURCES) -o $@

# THE MAP GATE, for a BIG-ENDIAN target (docs/SPEC-TABLES.md §2.8, §3). Its
# wire goldens were written by a LITTLE-ENDIAN build, so a codec that reached
# for host byte order anywhere in the map's framing — the array's L, its N, an
# entry's L, the key's length — would read them wrong and write them back
# differently. The sorted entry array is the same bytes on both hosts because
# the ORDER is over the wire's bytes and never over a machine word.
build/schema_test_maps_be: build/tables-generated/.stamp test/tables/maps_main.cpp
	@mkdir -p build
	$(BE_CXX) $(TABLES_CXXFLAGS) -static $(TABLES_INCLUDES) test/tables/maps_main.cpp $(MAPS_SOURCES) -o $@

# The COOK's read side, for a BIG-ENDIAN target. A cook is produced in the byte
# order of the build it is cooked for (docs/SPEC-TABLES.md §7), so this is where that
# stops being a sentence: the big-endian build opens the big-endian cook
# NATIVELY — magic, header words, deltas and all, with no fix-up pass anywhere —
# and refuses the little-endian one, and this host does the mirror image.
build/schema_test_cook_be: build/tables-generated/.stamp test/tables/cook_main.cpp
	@mkdir -p build
	$(BE_CXX) $(COOK_CXXFLAGS) -static $(COOK_INCLUDES) test/tables/cook_main.cpp -o $@

# The cross-endian BLOCK driver, built BOTH ways: a block is produced in the
# byte order of the build that wrote it, and every other block test in this
# tree runs producer and consumer in one process. This is the one part of
# §19.1 that needs two builds and a file between them.
build/schema_test_block_endian: build/tables-generated/.stamp test/tables/block_endian_main.cpp
	@mkdir -p build
	$(CXX) $(BLOCK_CXXFLAGS) $(BLOCK_INCLUDES) test/tables/block_endian_main.cpp $(BLOCK_SOURCES) -o $@

build/schema_test_block_endian_be: build/tables-generated/.stamp test/tables/block_endian_main.cpp
	@mkdir -p build
	$(BE_CXX) $(BLOCK_CXXFLAGS) -static $(BLOCK_INCLUDES) test/tables/block_endian_main.cpp $(BLOCK_SOURCES) -o $@

.PHONY: tables-big-endian
tables-big-endian: build/schema_test_tables_be build/schema_test_maps_be build/schema_test_block_endian build/schema_test_block_endian_be build/schema_test_cook build/schema_test_cook_be build/cook-open/.stamp
	$(BE_RUN) ./build/schema_test_tables_be
	$(BE_RUN) ./build/schema_test_maps_be
	@echo "big-endian leg: the wire crosses the byte order, a map's framing and its sorted entry array with it"
	./build/schema_test_block_endian write build/block-host.bin
	$(BE_RUN) ./build/schema_test_block_endian_be write build/block-target.bin
	$(BE_RUN) ./build/schema_test_block_endian_be accept build/block-target.bin
	$(BE_RUN) ./build/schema_test_block_endian_be refuse build/block-host.bin
	./build/schema_test_block_endian accept build/block-host.bin
	./build/schema_test_block_endian refuse build/block-target.bin
	@echo "big-endian leg: a block does not cross the byte order either — the magic refuses it and the prologue's word names the order that wrote it"
	$(BE_RUN) ./build/schema_test_cook_be accept Scene build/cook-open/Scene-be.cook
	$(BE_RUN) ./build/schema_test_cook_be golden Scene build/cook-open/Scene-be.cook
	$(BE_RUN) ./build/schema_test_cook_be refuse Scene build/cook-open/Scene.cook
	./build/schema_test_cook accept Scene build/cook-open/Scene.cook
	./build/schema_test_cook refuse Scene build/cook-open/Scene-be.cook
	@echo "big-endian leg: a cook opens NATIVELY in the order it was cooked for, whole graph and all, and a cook of the other order is refused by the magic"
	$(MAKE) tables-cook-endian
	$(MAKE) tables-c-big-endian

# THE COOK IS THE HOST'S BUSINESS IN NEITHER DIRECTION (docs/SPEC-TABLES.md §7).
# The byte order is settled AT COOK TIME for the TARGET build, so what a cook
# holds must depend on `--byte-order` and on nothing else — least of all on the
# order of the machine that ran the tool. Every host this repo builds on is
# little-endian, so that is an assertion on a page until a big-endian host runs
# the same command: a cooker that reached for host order anywhere would have
# produced byte-identical files on every other leg in this file.
#
# Go cross-compiles, so the target binary is one env var and the pinned
# emulator is already installed for the legs above. The gate is BYTE IDENTITY
# ACROSS HOSTS, both orders, over a fixed root and a pointered one: four files
# from this host and four from s390x, compared pairwise.
.PHONY: tables-cook-endian
tables-cook-endian: bin/schema
	@rm -rf build/cook-endian && mkdir -p build/cook-endian
	GOOS=linux GOARCH=s390x go build -o build/cook-endian/schema-be ./cmd/schema
	GOOS=linux GOARCH=s390x go build -o build/cook-endian/cookgen-be ./test/cookgen
	go build -o build/cook-endian/cookgen ./test/cookgen
	./bin/schema pack --root PackConfig --out build/cook-endian/fixed.bin tables/pack/pinned/PackConfig tables/examples
	@for order in little big; do \
		./bin/schema cook --root PackConfig --in build/cook-endian/fixed.bin \
			--out build/cook-endian/host-$$order.cook --byte-order $$order tables/examples || exit 1; \
		$(BE_RUN) ./build/cook-endian/schema-be cook --root PackConfig --in build/cook-endian/fixed.bin \
			--out build/cook-endian/target-$$order.cook --byte-order $$order tables/examples || exit 1; \
		cmp build/cook-endian/host-$$order.cook build/cook-endian/target-$$order.cook || \
			{ echo "FAILED: a $$order-endian cook differs between a little-endian host and a big-endian one"; exit 1; }; \
		./build/cook-endian/cookgen --bytes 65536 --byte-order $$order --out build/cook-endian/host-gen-$$order.cook || exit 1; \
		$(BE_RUN) ./build/cook-endian/cookgen-be --bytes 65536 --byte-order $$order --out build/cook-endian/target-gen-$$order.cook || exit 1; \
		cmp build/cook-endian/host-gen-$$order.cook build/cook-endian/target-gen-$$order.cook || \
			{ echo "FAILED: a $$order-endian synthetic region differs between hosts"; exit 1; }; \
		$(BE_RUN) ./build/cook-endian/schema-be cook-check --root PackConfig \
			build/cook-endian/host-$$order.cook tables/examples || exit 1; \
		./bin/schema cook-check --root PackConfig build/cook-endian/target-$$order.cook tables/examples || exit 1; \
	done
	@echo "big-endian leg: a cook's bytes are the TARGET's order and never the host's, and either host checks either file"

# Its NEGATIVE CONTROL. Put ONE of the wire's byte-order-neutral stores back to
# a host-order copy — put16, which writes every field id, every enum value and
# every union arm id — and the leg must go RED on the big-endian target. The
# same sabotage stays GREEN on this little-endian host, and that pair is the
# whole argument for the leg: this is precisely the defect class no
# little-endian CI can see.
#
# The sabotaged emitter reaches the compiler through `go build -overlay`, so no
# tracked file is ever written to: an interrupt cannot leave a sabotaged
# working tree, and a parallel `make -j` cannot compile the sabotage into
# something else.
.PHONY: tables-big-endian-negative
tables-big-endian-negative: tables-big-endian
	@mkdir -p build
	@sed 's|void put16( uint16_t v ) { uint8_t b\[2\] = { uint8_t( v ), uint8_t( v >> 8 ) }; raw( b, 2 ); }|void put16( uint16_t v ) { raw( \&v, 2 ); } // SABOTAGED: host order|' \
		internal/codegen/cpptable/cpptable.go > build/cpptable-host-order.gotext
	@cmp -s build/cpptable-host-order.gotext internal/codegen/cpptable/cpptable.go && \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/codegen/cpptable/cpptable.go":"%s/build/cpptable-host-order.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/cpptable-overlay.json
	@go build -overlay=build/cpptable-overlay.json -o build/schema-host-order ./cmd/schema
	@rm -rf build/tables-host-order && mkdir -p build/tables-host-order
	$(call tables_generate,./build/schema-host-order,build/tables-host-order)
	$(CXX) $(TABLES_CXXFLAGS) $(call tables_includes,build/tables-host-order) \
		test/tables/main.cpp $$(ls build/tables-host-order/*/*Table.cpp) -o build/schema_test_tables_host_order
	$(BE_CXX) $(TABLES_CXXFLAGS) -static $(call tables_includes,build/tables-host-order) \
		test/tables/main.cpp $$(ls build/tables-host-order/*/*Table.cpp) -o build/schema_test_tables_host_order_be
	@./build/schema_test_tables_host_order > /dev/null || \
		{ echo "NEGATIVE CONTROL FAILED: a host-order put16 is visible on a little-endian host — this host is not little-endian, or the sabotage is not the one described"; exit 1; }
	@echo "negative control: a host-order put16 leaves the LITTLE-ENDIAN leg green"
	@if $(BE_RUN) ./build/schema_test_tables_host_order_be > build/host-order-be.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a host-order put16 left the BIG-ENDIAN leg green"; exit 1; \
	fi
	@grep -q "table wire golden" build/host-order-be.log || \
		{ echo "NEGATIVE CONTROL FAILED: the big-endian leg went red, but not on a wire golden"; cat build/host-order-be.log; exit 1; }
	@echo "negative control: the same put16 turns the BIG-ENDIAN leg red, on the wire goldens"

# The PACK GOLDEN (docs/SPEC-TABLES.md §17.4, issue #257). `schema pack` carries an
# IR-driven engine in Go — the compiler cannot run the code it emits — so the
# gate that makes the two ONE WIRE is a byte comparison: the bytes pack builds
# from the directory tree at tables/pack/config must equal PackConfigSave of
# the same instance built by hand in C++, and a C++ Load of pack's bytes must
# report nothing at all.
PACK_TREE := $(shell find tables/pack/config tables/pack/root -type f 2>/dev/null)

build/tables-pack.bin: bin/schema tables/examples/Pack.schema $(PACK_TREE)
	@mkdir -p build
	./bin/schema pack --root PackConfig --out $@ tables/pack/config tables/examples

build/tables-pack-root.bin: bin/schema $(PACK_TREE)
	@mkdir -p build
	./bin/schema pack --root RootConfig --out $@ tables/pack/root tables/examples

# PACK_INCLUDES is shared with the sanitized twins below, so a build and its
# twin can never drift into covering different code (#278's rule).
PACK_INCLUDES := -Ibuild/tables-generated/examples -Ibuild/tables-generated/pointers -Ibuild/tables-generated/scalars -Ibuild/tables-generated/messages -Ibuild/tables-generated/stream -Ibuild/tables-generated/blobs -Itest/tables -I$(SERIALIZE)
# these drivers CALL the text form, so they compile the generated translation
# unit that holds it (docs/SPEC-TABLES.md §16.1) — the same rule any consumer follows
PACK_JSON_SOURCES = $$(ls build/tables-generated/examples/*Table.cpp build/tables-generated/pointers/*Table.cpp build/tables-generated/scalars/*Table.cpp build/tables-generated/messages/*Table.cpp build/tables-generated/stream/*Table.cpp build/tables-generated/blobs/*Table.cpp)
PACK_CXXFLAGS := -std=c++17 -Wall -Wextra -Werror -Wshadow -ffp-contract=off
PACK_SANITIZE := -fsanitize=address,undefined -fno-sanitize-recover=all -fno-omit-frame-pointer -g

build/schema_test_pack: build/tables-generated/.stamp test/tables/pack_main.cpp
	@mkdir -p build
	$(CXX) $(PACK_CXXFLAGS) $(PACK_INCLUDES) test/tables/pack_main.cpp $(PACK_JSON_SOURCES) -o $@

# The SANITIZED twins (#278, applied to this PR's drivers). Both read file
# sizes off disk and size their own buffers from them, and both compare
# byte ranges whose lengths came out of a file or a manifest — bounds and
# lifetime questions -Werror cannot see, and exactly the class #278 stood the
# tables leg up against. -fno-sanitize-recover=all is what makes it a GATE.
build/schema_test_pack_asan: build/tables-generated/.stamp test/tables/pack_main.cpp
	@mkdir -p build
	$(CXX) $(PACK_CXXFLAGS) $(PACK_SANITIZE) $(PACK_INCLUDES) test/tables/pack_main.cpp $(PACK_JSON_SOURCES) -o $@

# §17.1's THIRD golden needs the engine's own text of each root, so unpack
# writes the one-file form beside the bins and the driver compares ToJson to it.
build/pack-text/.stamp: bin/schema build/tables-pack.bin build/tables-pack-root.bin
	@rm -rf build/pack-text
	@mkdir -p build/pack-text
	./bin/schema unpack --one-file --root PackConfig --in build/tables-pack.bin build/pack-text tables/examples
	./bin/schema unpack --one-file --root RootConfig --in build/tables-pack-root.bin build/pack-text tables/examples
	@touch $@

.PHONY: tables-pack
tables-pack: build/schema_test_pack build/schema_test_pack_asan build/tables-pack.bin build/tables-pack-root.bin build/pack-text/.stamp
	./build/schema_test_pack build/tables-pack.bin build/tables-pack-root.bin \
		build/pack-text/PackConfig.json build/pack-text/RootConfig.json
	./build/schema_test_pack_asan build/tables-pack.bin build/tables-pack-root.bin \
		build/pack-text/PackConfig.json build/pack-text/RootConfig.json

# The HOSTILE-VALUE gate (docs/SPEC-TABLES.md §16.2, §16.3, §17.5). One tree per rule
# the text form states — malformed number tokens, a value past a bits(N) width,
# a lone surrogate, `null` on a `?T`, a "None" key, duplicate keys — packed by
# the Go engine and then READ BY THE GENERATED BACKEND. The manifest says which
# trees refuse and what report the rest produce; the backend half asserts the
# invariant the report is a promise about, that bytes the engine called clean
# load clean and re-save byte-identically. The Go half lives in
# internal/tablepack's tests and reads the same manifest.
#
# THE CORPUS AND ITS MANIFEST LIVE IN THE CONFORMANCE HARNESS's DATA
# (testdata/conformance/tables): the battery was always data, so it moved there
# whole rather than keeping a registry of its own, and the harness's
# `json-hostile` surface reads the same rows this gate does. One corpus, one set
# of expectations, two gates asking different things of it.
HOSTILE_MANIFEST := testdata/conformance/tables/MANIFEST.txt
HOSTILE_TREES := $(shell find testdata/conformance/tables/json-hostile -type f 2>/dev/null)

build/hostile-values/.stamp: bin/schema $(HOSTILE_MANIFEST) $(HOSTILE_TREES)
	@rm -rf build/hostile-values
	@mkdir -p build/hostile-values
	@grep '^json-hostile ' $(HOSTILE_MANIFEST) | grep -v ' refused$$' | \
	while read -r kind name unit root tree verdict; do \
		./bin/schema pack --root $$root --out build/hostile-values/$$name.bin --tolerate \
			$$tree $$(awk -v u=$$unit '$$1 == "unit" && $$2 == u { for ( i = 3; i <= NF; i++ ) printf "%s ", $$i }' $(HOSTILE_MANIFEST)) || exit 1; \
	done
	@touch $@

build/schema_test_hostile: build/tables-generated/.stamp test/tables/hostile_main.cpp
	@mkdir -p build
	$(CXX) $(PACK_CXXFLAGS) $(PACK_INCLUDES) test/tables/hostile_main.cpp $(PACK_JSON_SOURCES) -o $@

build/schema_test_hostile_asan: build/tables-generated/.stamp test/tables/hostile_main.cpp
	@mkdir -p build
	$(CXX) $(PACK_CXXFLAGS) $(PACK_SANITIZE) $(PACK_INCLUDES) test/tables/hostile_main.cpp $(PACK_JSON_SOURCES) -o $@

.PHONY: tables-hostile-values
tables-hostile-values: build/schema_test_hostile build/schema_test_hostile_asan build/hostile-values/.stamp
	./build/schema_test_hostile $(HOSTILE_MANIFEST) build/hostile-values
	./build/schema_test_hostile_asan $(HOSTILE_MANIFEST) build/hostile-values

# Its NEGATIVE CONTROL: relax ONE rule of the number grammar — accept a leading
# `+`, which RFC 8259 does not — and the gate must go red, because a tree the
# manifest says is REFUSED starts packing. Same overlay mechanism as the wire
# negative control: no tracked file is ever written to.
.PHONY: tables-hostile-negative
tables-hostile-negative: tables-hostile-values
	@mkdir -p build
	@sed "s/in.text\[in.pos\] == '-' {/in.text[in.pos] == '-' || in.text[in.pos] == '+' { \/\/ SABOTAGED/" \
		internal/tabletext/read.go > build/read-sabotaged.gotext
	@cmp -s build/read-sabotaged.gotext internal/tabletext/read.go && \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/tabletext/read.go":"%s/build/read-sabotaged.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/read-overlay.json
	@go build -overlay=build/read-overlay.json -o build/schema-loose-numbers ./cmd/schema
	@if ./build/schema-loose-numbers pack --root RootConfig --out build/loose.bin --tolerate \
		testdata/conformance/tables/json-hostile/num-leading-plus tables/examples > /dev/null 2>&1; then \
		echo "pack hostile-value negative control: a leading + packs once the grammar is relaxed"; \
	else \
		echo "NEGATIVE CONTROL FAILED: relaxing the number grammar left num-leading-plus refused"; exit 1; \
	fi
	@./bin/schema pack --root RootConfig --out build/loose.bin --tolerate \
		testdata/conformance/tables/json-hostile/num-leading-plus tables/examples > /dev/null 2>&1 && \
		{ echo "NEGATIVE CONTROL FAILED: the real engine accepts a leading + too"; exit 1; } || true

# The NEGATIVE CONTROL for that golden: break ONE framing rule in the Go
# encoder — elide a present `?T`, which §2.3 forbids — and the golden must go
# red. A gate that cannot be made to fail is not a gate, so this target runs the
# POSITIVE one first: a red golden must not be able to pass as a successful
# sabotage.
#
# The sabotaged source lives under build/ and reaches the compiler through
# `go build -overlay`, so no tracked file is ever written to: an interrupt in
# the middle of this target cannot leave a sabotaged working tree, and a
# parallel `make -j` cannot compile the sabotage into something else.
.PHONY: tables-pack-negative
tables-pack-negative: tables-pack
	@mkdir -p build
	@sed 's/if !fv\.Present {/if true { \/\/ SABOTAGED: a present ?T elides/' \
		internal/tablewire/encode.go > build/encode-sabotaged.gotext
	@cmp -s build/encode-sabotaged.gotext internal/tablewire/encode.go && \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/tablewire/encode.go":"%s/build/encode-sabotaged.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/sabotage-overlay.json
	@go build -overlay=build/sabotage-overlay.json -o build/schema-sabotaged ./cmd/schema
	@./build/schema-sabotaged pack --root PackConfig --out build/tables-pack-sabotaged.bin \
		tables/pack/config tables/examples
	@if ./build/schema_test_pack build/tables-pack-sabotaged.bin build/tables-pack-root.bin \
		build/pack-text/PackConfig.json build/pack-text/RootConfig.json > /dev/null 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: eliding a present ?T left the golden green"; exit 1; \
	fi
	@echo "pack negative control: eliding a present ?T turns the golden red"

build/schema_test_random: generated/cpp/.stamp test/random_main.cpp
	@mkdir -p build
	$(CXX) $(CXXFLAGS) -Igenerated/cpp -Itest test/random_main.cpp -o $@

build/schema_test_ludicrous: generated/cpp/ludicrous/.stamp test/ludicrous_main.cpp
	@mkdir -p build
	$(CXX) $(CXXFLAGS) -Igenerated/cpp/ludicrous test/ludicrous_main.cpp -o $@

# the bench corpus — TWO units sharing one directory (SPEC §3.2: one package
# per unit, so every corpus command below names its unit's file, never the
# directory):
#   bench/corpus/Bench.schema      package bench      — the bench-corpus shapes
#     (BENCH-STANDARD.md §1.3); goldens testdata/wire/bench_*.bin
#   bench/corpus/RealWorld.schema  package realworld  — the §1.7 realistic
#     snapshot (RealPacket); golden testdata/wire/real_packet.bin
# Both are generated into all six languages so the bench shapes have ONE
# definition; the goldens come from the generated C++ (test/bench/main.cpp)
# and every bench runner is §1.5-gated against them. Languages whose module
# layout cannot hold two packages in one directory put the realworld unit in
# its own subdirectory (go, cs) or sibling crate
# (rust — one crate root per unit).
#
# COMPILE gates: generating a unit proves nothing about it compiling — the cs
# realworld unit shipped uncompilable (SerializeFixed had no 8-bit overload,
# issue #80's C# leg was its first consumer) while every gate stayed green.
# The test target builds generated/bench/go and both rust crates directly;
# C# has no unit-local build file, so `cd bench/cs && dotnet build` is the
# compile gate for the realworld unit (the runner is its one consumer). The
# cs Bench unit still has NO compile gate — nothing consumes it.
generated/bench/cpp/.stamp: bin/schema $(SCHEMAS_BENCH)
	./bin/schema generate --lang cpp --out generated/bench/cpp bench/corpus/Bench.schema
	./bin/schema generate --lang cpp --out generated/bench/cpp bench/corpus/RealWorld.schema
	@touch $@

# The TABLES bench corpus (bench/tables/README.md): bench/corpus/BenchTable.schema,
# one representative fixed table mirroring BenchMixed, generated for the
# backends that CARRY tables. It goes into its own directory rather than
# beside the type corpus because every other backend REFUSES a unit that
# declares tables, by name (docs/SPEC-TABLES.md §11) — generating it into
# generated/bench/<lang> would break seven legs' generation the day it landed.
# A port adds its stamp in its make/<lang>.mk, registered in BENCH_TABLES_LEGS,
# beside its bench/tables/<lang>/leg.
generated/bench/tables/cpp/.stamp: bin/schema bench/corpus/BenchTable.schema
	@mkdir -p generated/bench/tables/cpp
	./bin/schema generate --lang cpp --out generated/bench/tables/cpp bench/corpus/BenchTable.schema
	@touch $@

# the C++ producer/verifier of the bench-corpus goldens, and the C twin that
# proves the C emitter compiles under the strict flags AND matches those bytes
build/schema_test_bench: generated/bench/cpp/.stamp test/bench/main.cpp
	@mkdir -p build
	$(CXX) $(CXXFLAGS) -Igenerated/bench/cpp test/bench/main.cpp -o $@

# the tables bench corpus's producer and oracle (bench/tables/README.md): the
# ONE place that names a field of BenchTable.schema, so no language leg does
build/schema_test_bench_table: generated/bench/tables/cpp/.stamp test/bench/table_main.cpp
	@mkdir -p build
	$(CXX) $(CXXFLAGS) -Igenerated/bench/tables/cpp test/bench/table_main.cpp -o $@

test: build/schema_test build/schema_test_guard build/schema_test_tables build/schema_test_block build/schema_test_block_asan build/schema_test_block_fuzz build/schema_test_block_fuzz_asan build/pack-text/.stamp build/schema_test_hostile build/schema_test_hostile_asan build/hostile-values/.stamp build/schema_test_pack build/schema_test_pack_asan build/tables-pack.bin build/tables-pack-root.bin build/schema_test_tables_asan build/schema_test_random build/schema_test_ludicrous build/schema_test_bench build/schema_test_bench_table build/conformance-harness
	./build/schema_test
	./build/schema_test_guard
	$(MAKE) check-zero-range-negative-control
	$(MAKE) projection-variant-order-negative-control
	$(MAKE) projection-wire-law-negative-control
	$(MAKE) projection-union-arm-order-negative-control
	./build/schema_test_tables
	./build/schema_test_tables_asan
	# THE WIRE FUZZER (docs/SPEC-TABLES.md §4.2): the tolerant read on hostile
	# bytes against the engine, plain and sanitized, at a short random pass —
	# the enumerated passes run whole whatever N is — and its two controls at
	# N=0, where the enumerated passes alone turn each one red. The long pass
	# is `make tables-cpp-release`.
	$(MAKE) tables-wire-fuzz N=20000
	$(MAKE) tables-wire-fuzz-negative-control N=0
	$(MAKE) tables-zero-cost
	$(MAKE) tables-zero-cost-negative-control
	$(MAKE) tables-maps
	$(MAKE) tables-json-map-walk
	$(MAKE) tables-maps-negative-controls
	$(MAKE) tables-json-walk
	$(MAKE) tables-json-graph-walk
	$(MAKE) tables-json-negative-control
	$(MAKE) tables-ports-refuse-wide-scalars
	$(MAKE) tables-scalars-block-asserts
	# THE CONFORMANCE HARNESS (test/conformance/README.md): the same corpus as
	# data, one driver per language, and the matrix that says which surfaces a
	# backend has. The reference leg's own negative controls ride beside it;
	# every other leg's ride in that leg's test-<lang> (make/<lang>.mk).
	$(MAKE) conformance
	$(MAKE) conformance-negative-control
	$(MAKE) conformance-negative-control-absent
	$(MAKE) conformance-negative-control-reference-surface
	$(MAKE) conformance-negative-control-block-dump
	$(MAKE) tables-json-keyed-dup-negative-control
	$(MAKE) tables-flat-wire
	$(MAKE) tables-flat-wire-negative-control
	$(MAKE) tables-shared-node-negative-control
	$(MAKE) tables-keyed-iteration-negative-control
	$(MAKE) tables-hooks
	$(MAKE) tables-keyed-none-refusal-ndebug
	$(MAKE) tables-keyed-none-refusal-negative-control
	$(MAKE) tables-keyed-max-refusal-ndebug
	$(MAKE) tables-keyed-max-refusal-negative-control
	$(MAKE) tables-keyed-shift-negative-control
	$(MAKE) tables-clamp-limits
	$(MAKE) tables-clamp-limits-negative-control
	$(MAKE) tables-block
	$(MAKE) tables-block-fuzz
	$(MAKE) tables-block-fuzz-extent-negative-control
	$(MAKE) tables-block-fuzz-maximum-negative-control
	$(MAKE) tables-cook-cli
	$(MAKE) tables-cook-scale
	$(MAKE) tables-cook-fuzz-negative-control
	$(MAKE) tables-cook-open
	$(MAKE) tables-cook-write
	$(MAKE) tables-cook-write-negative-control
	$(MAKE) tables-cook-write-hooks-negative-control
	$(MAKE) tables-blob-read-hooks-negative-control
	$(MAKE) tables-blob-span-negative-control
	$(MAKE) tables-cook-valued
	$(MAKE) tables-cook-open-lengths-negative-control
	$(MAKE) tables-cook-open-root-negative-control
	$(MAKE) tables-block-zero-cost
	$(MAKE) tables-block-build-version
	$(MAKE) tables-block-fill-refuser
	$(MAKE) tables-block-fill-refuser-negative-control
	$(MAKE) tables-block-padding-negative-control
	$(MAKE) tables-block-pitch-negative-control
	$(MAKE) tables-block-layout-model-negative-control
	$(MAKE) tables-block-race-negative-control
	$(MAKE) tables-block-home-negative-control
	$(MAKE) tables-runtime-home
	$(MAKE) tables-runtime-home-negative-control
	$(MAKE) tables-block-inline-array-negative-control
	$(MAKE) tables-pack
	$(MAKE) tables-pack-negative
	$(MAKE) tables-hostile-values
	$(MAKE) tables-hostile-negative
	./build/schema_test_random
	./build/schema_test_ludicrous
	./build/schema_test_bench
	# the tables bench corpus's oracle — the generated table unit has no other
	# consumer under `make test`, and a unit that generates but does not compile
	# is issue #80's lesson, so each leg's compile gate rides in its test-<lang>
	./build/schema_test_bench_table
	# EVERY REGISTERED LEG (make/<lang>.mk, TEST_LEGS): its generated trees, its
	# gates and negative controls, its packet and table tests. One sub-make per
	# leg, so a red names the leg.
	@set -e; for leg in $(TEST_LEGS); do echo "$(MAKE) $$leg"; $(MAKE) $$leg; done
	go test ./...


# ---------------------------------------------------------------------------
# THE MAP GATE (docs/SPEC-TABLES.md §2.8) ----------------------------------
#
# One binary over the `tables/maps` corpus: the builder's five, the sort the
# four writing walks hold, the node extent a region and a cook carry, and every
# reader rule §2.8 states. Its wire goldens are the reference's, pinned like
# every other table golden.
#
# THE SANITIZED TWIN rides beside it for the reason the tables leg's does: the
# map machinery is an arena of segments, an entry array carved from a node's
# own extent and a binary search over mapped bytes, which are lifetime and
# bounds questions -Werror cannot see.

MAPS_SOURCES = $$(ls build/tables-generated/maps/*Table.cpp)

build/schema_test_maps: build/tables-generated/.stamp test/tables/maps_main.cpp
	@mkdir -p build
	$(CXX) $(TABLES_CXXFLAGS) $(TABLES_INCLUDES) test/tables/maps_main.cpp $(MAPS_SOURCES) -o $@

build/schema_test_maps_asan: build/tables-generated/.stamp test/tables/maps_main.cpp
	@mkdir -p build
	$(CXX) $(TABLES_CXXFLAGS) -fsanitize=address,undefined -fno-omit-frame-pointer -g \
		$(TABLES_INCLUDES) test/tables/maps_main.cpp $(MAPS_SOURCES) -o $@

.PHONY: tables-maps
tables-maps: build/schema_test_maps build/schema_test_maps_asan
	./build/schema_test_maps
	./build/schema_test_maps_asan

# THE MAP-WALK GATE (docs/SPEC-TABLES.md §2.8, §16): the map's half of the text
# form is emitted only in a unit that declares one, and it is ONE half too —
# the same bytes in every map-bearing .cpp of the corpus, on the walk's own
# terms — and none of it reaches a map-free unit, which is the zero-cost
# property (§2.2) holding for the text form.
.PHONY: tables-json-map-walk
tables-json-map-walk: build/tables-generated/.stamp
	@rm -rf build/json-map-walk && mkdir -p build/json-map-walk
	@for f in build/tables-generated/maps/*Table.cpp; do \
		out=build/json-map-walk/$$(echo $$f | tr / _); \
		awk '/---- json map walk: begin ----/,/---- json map walk: end ----/' $$f > $$out; \
		if [ ! -s $$out ]; then echo "MAP-WALK GATE FAILED: no map half in $$f"; exit 1; fi; \
	done
	@first=""; for f in build/json-map-walk/*; do \
		if [ -z "$$first" ]; then first=$$f; else \
			cmp -s $$first $$f || { echo "MAP-WALK GATE FAILED: the map half in $$f is not the map half in $$first"; exit 1; }; \
		fi; \
	done
	@for f in build/tables-generated/examples/*Table.cpp build/tables-generated/pointers/*Table.cpp; do \
		if grep -q "json map walk: begin" $$f; then \
			echo "MAP-WALK GATE FAILED: the map half reached the map-free unit $$f"; exit 1; \
		fi; \
	done
	@echo "tables map-walk gate: one map half, byte-identical in $$(ls build/json-map-walk | wc -l | tr -d ' ') map-bearing .cpp files, and none in a map-free one"

# ---- the NEGATIVE CONTROLS §2.8 names ------------------------------------
#
# Each names the sabotage, patches the GENERATOR through a Go overlay,
# regenerates the corpus, rebuilds the gate and requires it to go RED. A
# sabotage that patches nothing is itself a failure, so a control cannot rot
# into a no-op when the emitter it names moves.
#
# $(1) the control's short name, $(2) the sed script, $(3) the file to patch,
# $(4) the sentence a reader gets when the gate stayed green.
define map_negative_control
	@mkdir -p build
	@sed -e $(2) $(3) > build/map-$(1).gotext
	@cmp -s build/map-$(1).gotext $(3) && \
		{ echo "NEGATIVE CONTROL FAILED: the $(1) sabotage patched nothing"; exit 1; } || true
	@printf '{"Replace":{"%s/$(3)":"%s/build/map-$(1).gotext"}}\n' "$(CURDIR)" "$(CURDIR)" > build/map-$(1)-overlay.json
	@go build -overlay=build/map-$(1)-overlay.json -o build/schema-map-$(1) ./cmd/schema
	@rm -rf build/tables-map-$(1) && mkdir -p build/tables-map-$(1)
	@./build/schema-map-$(1) generate --lang cpp --out build/tables-map-$(1)/maps tables/maps
	@$(CXX) $(TABLES_CXXFLAGS) -Ibuild/tables-map-$(1)/maps -Itest/tables test/tables/maps_main.cpp \
		build/tables-map-$(1)/maps/*Table.cpp -o build/schema_test_maps_$(1)
	@if ./build/schema_test_maps_$(1) > build/map-$(1).log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: $(4)"; exit 1; \
	fi
	@grep -q "^FAIL test/tables/maps_main.cpp" build/map-$(1).log || \
		{ echo "NEGATIVE CONTROL FAILED: the gate went red, but not on a CHECK"; cat build/map-$(1).log; exit 1; }
	@echo "negative control: $(1) turns the MAP GATE red — $$(grep -c '^FAIL' build/map-$(1).log) failures"
endef

# THE WRITER EMITS INSERTION ORDER instead of sorted. The instance built OUT OF
# KEY ORDER meets it: the byte compare against its pinned wire goes red while
# measure == save still holds, which says the sabotage is the sort and not the
# arithmetic.
.PHONY: tables-maps-sort-negative-control
tables-maps-sort-negative-control: bin/schema build/tables-generated/.stamp
	$(call map_negative_control,sort,'s@int32_t start = count / 2 - 1@int32_t start = -1@' -e 's@int32_t end = count - 1@int32_t end = 0@',internal/codegen/cpptable/maps.go,the writer emitting insertion order left the map gate GREEN)

# SAVE EMITS A DEAD ENTRY. The instance that ERASES meets it, and the byte
# compare against its pinned wire goes red while measure == save still holds.
.PHONY: tables-maps-dead-entry-negative-control
tables-maps-dead-entry-negative-control: bin/schema build/tables-generated/.stamp
	$(call map_negative_control,dead,'s@segment->dead\[ i / 32 \] |= 1u << ( i % 32 );@(void) i; // SABOTAGED@',internal/codegen/cpptable/maps.go,a dead entry riding on the wire left the map gate GREEN)

# THE READER'S ASCENDING CHECK IS DROPPED. The SHUFFLED report row meets it,
# and the row's `malformed` flag goes red.
.PHONY: tables-maps-ascending-negative-control
tables-maps-ascending-negative-control: bin/schema build/tables-generated/.stamp
	$(call map_negative_control,ascending,'s@if ( order > 0 )@if ( order > 0 \&\& false )@',internal/codegen/cpptable/maps.go,a shuffled map left the map gate GREEN)

# THE DUPLICATE RULE IS DROPPED, first wins or both kept. The row whose
# DUPLICATE entry ELIDES a field the first occurrence set meets it, and the
# decoded value goes red — a reader that overlays instead of resetting agrees
# with the rule on every other body.
.PHONY: tables-maps-duplicate-negative-control
tables-maps-duplicate-negative-control: bin/schema build/tables-generated/.stamp
	$(call map_negative_control,duplicate,'s@slot = TableMapFillLast( fill );@slot = TableMapFillNext( fill ); // SABOTAGED@',internal/codegen/cpptable/maps.go,keeping both occurrences of a duplicate left the map gate GREEN)

# THE KEY-KIND RULE DECODES ANYWAY. The row written under a CHANGED KEY KIND
# meets it, and its five counters go red, because an entry lands under a
# defaulted key where the row says the map is empty.
.PHONY: tables-maps-key-kind-negative-control
tables-maps-key-kind-negative-control: bin/schema build/tables-generated/.stamp
	$(call map_negative_control,keykind,'s@out.kind_bad = field_kind != %d;@out.kind_bad = ( field_kind != %d ) \&\& false;@',internal/codegen/cpptable/maps.go,decoding under a changed key kind left the map gate GREEN)

# KEYS NEVER CLAMP. The row whose key is longer than the reader's bound meets
# it, and the DECODED VALUE is the half that says it: a clamped key merges two
# entries where the rule drops one, and the `clamped` count alone cannot
# separate the two.
.PHONY: tables-maps-clamp-negative-control
tables-maps-clamp-negative-control: bin/schema build/tables-generated/.stamp
	$(call map_negative_control,clamp,'s@out.over = key_len > %d;@out.over = ( key_len > %d ) \&\& false;@',internal/codegen/cpptable/maps.go,clamping an over-long key left the map gate GREEN)

# N = 0xFFFFFFFF UNDER A SHORT L. A row of a few dozen bytes asking for
# gigabytes meets it, and LoadMeasure's answer goes red if the fit check is
# dropped.
.PHONY: tables-maps-fit-negative-control
tables-maps-fit-negative-control: bin/schema build/tables-generated/.stamp
	$(call map_negative_control,fit,'s@if ( n > rest / kTableMapEntryFloor ) { return false; }@if ( n > rest / kTableMapEntryFloor \&\& false ) { return false; }@',internal/codegen/cpptable/maps.go,an N the map L cannot carry left the map gate GREEN)

# LOADMEASURE OVER A MAP OF MAPS, summed at ONE DEPTH only. The instance whose
# value is itself a map meets it, and the measure goes red against the region
# Load fills.
.PHONY: tables-maps-depth-negative-control
tables-maps-depth-negative-control: bin/schema build/tables-generated/.stamp
	$(call map_negative_control,depth,'s@if ( inner == NULL ) { return true; }@return true; // SABOTAGED@',internal/codegen/cpptable/maps.go,summing the extent at one depth only left the map gate GREEN)

# TOJSON WRITES ENTRIES IN ASCENDING KEY ORDER, so unpack then pack is
# byte-stable and a diff of two texts is a diff of two maps (§2.8, §17.2). The
# instance built out of key order meets it, and the round trip's byte compare
# goes red if the writer walks the entries any other way.
.PHONY: tables-maps-text-order-negative-control
tables-maps-text-order-negative-control: bin/schema build/tables-generated/.stamp
	$(call map_negative_control,textorder,'s@const void \* entry = f->map_at( slot, i );@const void * entry = f->map_at( slot, count - 1 - i );@',internal/codegen/cpptable/json.go,writing a map text out of key order left the map gate GREEN)

# AN UNREACHED NON-EMPTY MAP SLOT IS REFUSED by Cook and by Lock, the same
# refusal §7.6 gives a pointer in that position. The `Depth` instance whose
# counted array holds a map PAST ITS LIVE COUNT meets it, and dropping the
# refusal lets Lock write that map's entries into an extent nothing reserved
# for them.
.PHONY: tables-maps-unreached-negative-control
tables-maps-unreached-negative-control: bin/schema build/tables-generated/.stamp
	$(call map_negative_control,unreached,'s@inline bool TableMapUnreachedEmpty( int64_t extent ) { return extent == 0; }@inline bool TableMapUnreachedEmpty( int64_t ) { return true; }@',internal/codegen/cpptable/maps.go,writing an unreached non-empty map left the map gate GREEN)

.PHONY: tables-maps-negative-controls
tables-maps-negative-controls: tables-maps-sort-negative-control \
	tables-maps-dead-entry-negative-control \
	tables-maps-ascending-negative-control \
	tables-maps-duplicate-negative-control \
	tables-maps-key-kind-negative-control \
	tables-maps-clamp-negative-control \
	tables-maps-fit-negative-control \
	tables-maps-depth-negative-control \
	tables-maps-text-order-negative-control \
	tables-maps-unreached-negative-control

# Re-pin the goldens DELIBERATELY (SPEC §7.2 gates 1, 2, 7). A wire golden
# breaking under an unchanged schema is stop-the-line, never a quiet re-pin
# (SPEC §3.1) — this target is for intentional emitter/schema changes only.
update-goldens: build/schema_test build/schema_test_ludicrous build/schema_test_bench build/schema_test_bench_table build/schema_test_tables build/schema_test_block build/schema_test_maps
	@mkdir -p testdata/golden testdata/wire testdata/wire/tables
	go test ./internal/goldens -update -run 'TestGolden'
	SCHEMA_UPDATE_WIRE_GOLDENS=1 ./build/schema_test
	SCHEMA_UPDATE_WIRE_GOLDENS=1 ./build/schema_test_tables
	SCHEMA_UPDATE_WIRE_GOLDENS=1 ./build/schema_test_block
	SCHEMA_UPDATE_WIRE_GOLDENS=1 ./build/schema_test_maps
	@for d in examples pointers block blockhome messages stream blobs scalars maps; do \
		mkdir -p testdata/golden/tables/$$d; \
		cp build/tables-generated/$$d/*Table.h build/tables-generated/$$d/*Table.cpp testdata/golden/tables/$$d/ 2>/dev/null || true; \
	done
	# every leg's committed table goldens (make/<lang>.mk, GOLDENS_LEGS)
	@set -e; for leg in $(GOLDENS_LEGS); do $(MAKE) $$leg; done
	SCHEMA_UPDATE_WIRE_GOLDENS=1 ./build/schema_test_ludicrous
	SCHEMA_UPDATE_WIRE_GOLDENS=1 ./build/schema_test_bench
	./build/schema_test_bench_table pin
	go test ./...

# the cross-language serialize profiling harness (bench/README.md): builds and
# runs whichever language runners are available, Release flags, results CSV
# under bench/results/
bench:
	bench/run.sh

# The bench_mixed variant data (issue #191): 64 wire buffers the data-driven
# drivers bench, regenerated from bench/corpus/Bench.schema's generated Go
# codec. Deterministic — a regeneration that changes the committed file means
# the shape or the §2.7 LCG mapping moved, and the tool refuses outright if
# variant 0 stops equalling testdata/wire/bench_mixed.bin. Needs the
# serialize.go checkout ($(SERIALIZE_GO)); the committed data's own gate,
# bench/corpus/variants_test.go, needs nothing and runs in `make test`.
bench-variants: generated/bench/go/.stamp
	cd bench/tools/variantgen && go run .

# ---------------------------------------------------------------------------
# THE TABLES BENCH (bench/tables/README.md) ---------------------------------
#
# One measured shape: a representative fixed table written and read on the
# TOLERANT WIRE (docs/SPEC-TABLES.md §3) — the tables layer's per-language
# release gate, and the number a reader who knows protobuf or flatbuffers
# already has a comparison for. Block-form and cook numbers are NOT here: the
# owner's scope ruling on issue #330 keeps those C++/C# and takes them in the
# game on real render data.
#
# The corpus DATA is produced and verified by build/schema_test_bench_table,
# which `make test` runs. `bench-table-corpus` re-pins it: deterministic, so a
# re-pin that changes the committed files means the shape or the vary mapping
# moved — the same stop-the-line rule the wire goldens carry.
bench-table-corpus: build/schema_test_bench_table
	./build/schema_test_bench_table pin

bench-table-check: build/schema_test_bench_table
	./build/schema_test_bench_table verify


# Prove the COMMITTED generated/ tree matches what the current compiler
# emits (issue #30). `make test` regenerates every tracked generated file in
# place, so staleness is precisely a dirty tree afterwards — a tracked file
# that changed, or a newly emitted file nobody committed. CI runs the same
# two checks after its make test step.
generated-current: test
	@git diff --exit-code generated/ || { \
		echo "committed generated/ tree is STALE — the current compiler emits different text."; \
		echo "review the diff above, then commit the regenerated files."; \
		exit 1; \
	}
	@untracked=$$(git status --porcelain generated/); \
	if [ -n "$$untracked" ]; then \
		echo "$$untracked"; \
		echo "the generator emitted files that are not committed under generated/ — add them."; \
		exit 1; \
	fi
	@echo "generated/ tree is current"

# bench/corpus holds two units (one package per unit, SPEC §3.2), so the
# corpus commands name each unit's file rather than the directory
check: bin/schema
	./bin/schema check examples
	./bin/schema check examples128
	./bin/schema check tables/examples
	./bin/schema check tables/pointers
	./bin/schema check tables/block
	./bin/schema check tables/blockhome
	./bin/schema check tables/messages
	./bin/schema check tables/stream
	./bin/schema check tables/blobs
	./bin/schema check tables/scalars
	./bin/schema check test/tables/V1.schema
	./bin/schema check test/tables/V2.schema
	./bin/schema check test/tables/P1.schema
	./bin/schema check test/tables/P2.schema
	./bin/schema check test/tables/P3.schema
	./bin/schema check test/tables/M1.schema
	./bin/schema check test/tables/M2.schema
	./bin/schema check bench/corpus/Bench.schema
	./bin/schema check bench/corpus/RealWorld.schema
	./bin/schema check bench/corpus/BenchTable.schema

id: bin/schema
	./bin/schema id examples
	./bin/schema id examples128
	./bin/schema id bench/corpus/Bench.schema
	./bin/schema id bench/corpus/RealWorld.schema

fmt: bin/schema
	./bin/schema fmt examples
	./bin/schema fmt examples128
	./bin/schema fmt tables/examples
	./bin/schema fmt tables/pointers
	./bin/schema fmt tables/block
	./bin/schema fmt tables/blockhome
	./bin/schema fmt tables/messages
	./bin/schema fmt tables/stream
	./bin/schema fmt tables/blobs
	./bin/schema fmt tables/scalars
	./bin/schema fmt test/tables/V1.schema
	./bin/schema fmt test/tables/V2.schema
	./bin/schema fmt test/tables/P1.schema
	./bin/schema fmt test/tables/P2.schema
	./bin/schema fmt test/tables/P3.schema
	./bin/schema fmt test/tables/M1.schema
	./bin/schema fmt test/tables/M2.schema
	./bin/schema fmt bench/corpus/Bench.schema
	./bin/schema fmt bench/corpus/RealWorld.schema
	./bin/schema fmt bench/corpus/BenchTable.schema

# The one-benchmark rule, made mechanical: no hand-coded measurement of a
# schema shape anywhere in this repo except what a SHAPE-GATE.allow names —
# bench/SHAPE-GATE.allow for shared tooling, one beside each leg for its own.
shape-gate:
	go run ./bench/tools/shapegate

clean:
	rm -rf bin build generated

.PHONY: all test check id fmt clean update-goldens bench bench-variants bench-tables bench-table-corpus bench-table-check generated-current shape-gate

# ---------------------------------------------------------------------------
# THE TABLES CONFORMANCE HARNESS (test/conformance/README.md) ----------------
#
# A port of the tables layer is "make the driver pass". The DATA lives under
# testdata/conformance/tables and names no language; the CONTRACT lives in
# test/conformance/README.md; the harness runs every registered driver over
# every surface and prints the matrix. Registering a port is its driver at
# test/conformance/<lang>/driver, which the harness discovers.
#
# The rule this target lives under is the two-minute one (#320). Measured on
# arm64 macOS at the landing, everything already built, median of three:
#
#   all three legs, 260 cases each   10.5 s
#   the cpp leg alone                 0.79 s   (native execs, plus materialising)
#   the cs leg alone                 10.0 s   (`dotnet run` start-ups)
#   the go leg alone                  1.07 s   (one native exec per surface)
#
# The cost is per-PROCESS, not per-case: the C# leg starts a runtime once per
# surface plus once per cook, because test/cs-cook's dump takes one root per
# invocation, and that is where nearly the whole wall is. The Go leg is one
# native exec per surface and no more — its cook and cook-forgery are answered
# in the same binary as everything else — which is the cheapest shape a leg can
# have under this contract. So the budget left for six more languages is most
# of the two minutes, and sharding per language leg, the way the type wire's
# nine legs already are, is what the numbers say to do if that stops holding;
# it is not needed at this size.
CONFORMANCE_INCLUDES := -Ibuild/tables-generated/examples -Ibuild/tables-generated/v1 \
	-Ibuild/tables-generated/v2 -Ibuild/tables-generated/p1 -Ibuild/tables-generated/p3 \
	-Ibuild/tables-generated/block -Ibuild/tables-generated/pointers \
	-Ibuild/tables-generated/p2 -Ibuild/tables-generated/messages -Ibuild/tables-generated/stream \
	-Ibuild/tables-generated/m1 -Ibuild/tables-generated/m2 -Ibuild/tables-generated/a1 -Ibuild/tables-generated/a2 -Ibuild/tables-generated/g1 -Ibuild/tables-generated/k1 -Ibuild/tables-generated/k2 -Ibuild/tables-generated/blobs -Itest/tables -Ibuild/tables-generated/scalars -Ibuild/tables-generated/scalars2 -I$(SERIALIZE)
CONFORMANCE_SOURCES = build/tables-generated/examples/TablesTable.cpp \
	build/tables-generated/scalars/ScalarsTable.cpp build/tables-generated/scalars2/Scalars2Table.cpp \
	build/tables-generated/examples/WideTable.cpp build/tables-generated/examples/NestedTable.cpp \
	build/tables-generated/examples/KeyedTable.cpp build/tables-generated/examples/PackTable.cpp build/tables-generated/v1/V1Table.cpp \
	build/tables-generated/v2/V2Table.cpp build/tables-generated/p1/P1Table.cpp \
	build/tables-generated/p2/P2Table.cpp build/tables-generated/p3/P3Table.cpp build/tables-generated/block/RenderBlock.cpp \
	build/tables-generated/block/PaddedBlock.cpp build/tables-generated/pointers/GraphTable.cpp \
	build/tables-generated/pointers/MarksTable.cpp build/tables-generated/pointers/PartsTable.cpp \
	build/tables-generated/messages/MessagesTable.cpp build/tables-generated/stream/StreamTable.cpp \
	build/tables-generated/blobs/AssetsTable.cpp \
	build/tables-generated/m1/M1Table.cpp build/tables-generated/m2/M2Table.cpp \
	build/tables-generated/a1/A1Table.cpp build/tables-generated/a2/A2Table.cpp \
	build/tables-generated/k1/K1Table.cpp build/tables-generated/k2/K2Table.cpp \
	build/tables-generated/g1/G1Table.cpp

# the harness LINKS the compiler's own engine — internal/tablewire and
# internal/tabletext, reached through compiler/ — so its dependencies are the
# compiler's sources as well as its own. Without that, an edit to the engine
# left a stale binary generating the data every other gate then compares.
build/conformance-harness: $(wildcard test/conformance/harness/*.go) $(GO_SOURCES)
	@mkdir -p build
	go build -o $@ ./test/conformance/harness

build/conformance-cpp: build/tables-generated/.stamp test/conformance/cpp/main.cpp
	@mkdir -p build
	$(CXX) $(TABLES_CXXFLAGS) $(CONFORMANCE_INCLUDES) test/conformance/cpp/main.cpp \
		$(CONFORMANCE_SOURCES) -o $@

# ---- THE WIRE FUZZER (docs/SPEC-TABLES.md §4.2; docs/SECURITY.md) -------------
#
# A table read is untrusted input, so the tolerant read IS the verifier, and
# this is the gate on that claim for the C++ reference: `harness wire-fuzz`
# mutates every pinned wire in the corpus, feeds each mutant to the leg on a
# pipe and to the compiler's own engine, and requires the same report, the
# same decoded value and a LoadMeasure inside the framing's bound, for every
# mutant. The leg runs twice — plain, and under ASan and UBSan — because the
# two see different heaps: the plain build is the divergence oracle at speed,
# and the sanitized one turns a one-byte over-read into a finding on the
# mutant that caused it. SEED and N size the random pass; the enumerated
# passes run whatever N is.
build/wire-fuzz-cpp: build/tables-generated/.stamp test/tables/wire_fuzz_main.cpp
	@mkdir -p build
	$(CXX) $(TABLES_CXXFLAGS) -O1 $(CONFORMANCE_INCLUDES) test/tables/wire_fuzz_main.cpp \
		$(CONFORMANCE_SOURCES) -o $@

build/wire-fuzz-cpp-asan: build/tables-generated/.stamp test/tables/wire_fuzz_main.cpp
	@mkdir -p build
	$(CXX) $(TABLES_CXXFLAGS) -O1 -fsanitize=address,undefined -fno-sanitize-recover=all \
		-fno-omit-frame-pointer -g $(CONFORMANCE_INCLUDES) test/tables/wire_fuzz_main.cpp \
		$(CONFORMANCE_SOURCES) -o $@

.PHONY: tables-wire-fuzz
tables-wire-fuzz: build/conformance-harness build/wire-fuzz-cpp build/wire-fuzz-cpp-asan
	./build/conformance-harness wire-fuzz --driver ./build/wire-fuzz-cpp --seed $(SEED) --n $(N)
	./build/conformance-harness wire-fuzz --driver ./build/wire-fuzz-cpp-asan --seed $(SEED) --n $(N) \
		--failed build/wire-fuzz/failed-asan.bin

# THE WIRE FUZZER'S NEGATIVE CONTROLS (docs/SPEC-TABLES.md §4.2). A fuzzer
# that has never gone red proves nothing about the reader it points at, so
# each control removes ONE check from the EMITTER — through `go build
# -overlay`, so no tracked file moves — regenerates the corpus from the
# sabotaged compiler, builds the same leg against it, and requires the fuzzer
# to go red on the verdict that check guards. Two checks, because the tolerant
# reader and the variable-class loader are two readers: a length check in the
# string read, and the node-index range check in the numbering's resolve.
#
# $(1) the control's name  $(2) the emitter file  $(3) the sed program
# $(4) the verdict the fuzzer must print
define wire_fuzz_control
	@rm -rf build/wire-fuzz-nc-$(1) && mkdir -p build/wire-fuzz-nc-$(1)
	@sed -e '$(3)' $(2) > build/wire-fuzz-nc-$(1)/emitter.go.txt
	@cmp -s $(2) build/wire-fuzz-nc-$(1)/emitter.go.txt && \
		{ echo "NEGATIVE CONTROL: the $(1) sabotage patched nothing"; exit 1; } || true
	@printf '{"Replace":{"%s/$(2)":"%s/build/wire-fuzz-nc-$(1)/emitter.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/wire-fuzz-nc-$(1)/overlay.json
	go build -overlay build/wire-fuzz-nc-$(1)/overlay.json -o build/wire-fuzz-nc-$(1)/schema ./cmd/schema
	$(call tables_generate,./build/wire-fuzz-nc-$(1)/schema,build/wire-fuzz-nc-$(1)/generated)
	$(CXX) $(TABLES_CXXFLAGS) -O1 $(call tables_includes,build/wire-fuzz-nc-$(1)/generated) \
		test/tables/wire_fuzz_main.cpp \
		$(subst build/tables-generated/,build/wire-fuzz-nc-$(1)/generated/,$(CONFORMANCE_SOURCES)) \
		-o build/wire-fuzz-nc-$(1)/leg
	@if ./build/conformance-harness wire-fuzz --driver ./build/wire-fuzz-nc-$(1)/leg --seed $(SEED) --n $(N) \
			--failed build/wire-fuzz-nc-$(1)/failed.bin > build/wire-fuzz-nc-$(1)/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the $(1) check is gone and the wire fuzzer stayed green"; \
		cat build/wire-fuzz-nc-$(1)/log; exit 1; \
	fi
	@grep -q "$(4)" build/wire-fuzz-nc-$(1)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the wire fuzzer went red, but not on the $(1) check"; \
		  cat build/wire-fuzz-nc-$(1)/log; exit 1; }
	@grep -m1 "FAILED" build/wire-fuzz-nc-$(1)/log
	@echo "negative control: removing the $(1) check from the emitter turns the wire fuzzer RED"
endef

# THE C++ RELEASE GATE: the wire fuzzer at a long random pass, both builds.
# certify.yml runs every `tables-<lang>-release` target by name.
.PHONY: tables-cpp-release
tables-cpp-release:
	$(MAKE) tables-wire-fuzz N=500000
	$(MAKE) tables-wire-fuzz SEED=2 N=500000

.PHONY: tables-wire-fuzz-negative-control tables-wire-fuzz-length-negative-control tables-wire-fuzz-index-negative-control tables-wire-fuzz-arm-width-negative-control tables-wire-fuzz-arm-terminator-negative-control tables-wire-fuzz-oracle-negative-control
tables-wire-fuzz-negative-control: tables-wire-fuzz-length-negative-control tables-wire-fuzz-index-negative-control tables-wire-fuzz-arm-width-negative-control tables-wire-fuzz-arm-terminator-negative-control tables-wire-fuzz-oracle-negative-control

# the string read's `room( len )`: a length past the body is then read anyway,
# so the sabotaged leg takes a string out of a neighbour's bytes and CLAMPS it
# to its capacity where the oracle stops at the body — the clamp it counts is
# a fact about bytes the field never owned. A LENGTH IS A 64-BIT NUMBER (§3),
# so the check is unsigned: cast to int64 first, 0xFFFFFFFFFFFFFFFF reads as
# -1 and a negative length looks like room.
tables-wire-fuzz-length-negative-control: build/conformance-harness
	$(call wire_fuzz_control,length,internal/codegen/cpptable/codecs.go,s|if ( !r.getleb( len ) \|\| !r.room( len ) ) { r.report->malformed = true; return false; }|if ( !r.getleb( len ) ) { r.report->malformed = true; return false; } // NEGATIVE CONTROL: the fit check is gone|,the report differs)

# the numbering's `index - 1 >= map.count`: an index past the node table then
# reads a directory entry the region does not hold
tables-wire-fuzz-index-negative-control: build/conformance-harness
	$(call wire_fuzz_control,index,internal/codegen/cpptable/arena.go,s|if ( index - 1 >= (uint64_t) map.count )|if ( false ) // NEGATIVE CONTROL: the range check is gone|,the report differs)

# AN ARM'S `L` IS CHECKED AGAINST ITS KIND'S WIDTH (docs/SPEC-TABLES.md §3):
# the arm header carries the kind, and an `L` that is not that kind's width is
# the arm's own framing damage. The fuzzer moves a fixed-width arm's `L` to
# every width the closed set has and to zero; without the check the arm decodes
# a value out of a payload that is not its own.
tables-wire-fuzz-arm-width-negative-control: build/conformance-harness
	$(call wire_fuzz_control,arm-width,internal/codegen/cpptable/arms.go,s|if ( %s.size != %d ) { %s = %s; r.report->malformed = true|if ( false \&\& %s.size != %d ) { %s = %s; r.report->malformed = true|,differs)

# A BODY'S TERMINATOR IS THE END OF ITS PAYLOAD (§3), for an arm whose payload
# is a body as much as for a kind `13` field: the fuzzer writes the ZERO
# REFERENCE ahead of the payload's last byte, so the body ends inside its own
# `L` and the bytes after it are claimed by no field.
tables-wire-fuzz-arm-terminator-negative-control: build/conformance-harness
	$(call wire_fuzz_control,arm-terminator,internal/codegen/cpptable/arms.go,s|if ( %s.offset != %s.size ) { %s = %s; r.report->malformed = true|if ( false \&\& %s.offset != %s.size ) { %s = %s; r.report->malformed = true|,the decoded value differs)

# THE ORACLE NEVER PANICS, which is what makes it an oracle. The four controls
# above sabotage the C++ EMITTER and ask whether the fuzzer notices a leg that
# lost a check. This one sabotages the ORACLE and asks whether the fuzzer
# notices the engine itself dying on hostile bytes. It is the standing control
# for the pinned vector stream_count_past_body: a body header is read against
# the parent buffer, so a wide count leaves the cursor past the end its own `L`
# set, and the span from cursor to end goes negative. Without the clamp the
# oracle slices with it and panics. The run must go red, on the VECTOR and on
# the panic, and not merely somewhere.
#
# Nothing tracked is written to: the clamp is removed from a COPY and reached
# through a Go build overlay, so an interrupt cannot leave a sabotaged tree.
# The leg is the ordinary one, so this control costs no second C++ build.
ORACLE_NC := build/wire-fuzz-nc-oracle
.PHONY: tables-wire-fuzz-oracle-negative-control
tables-wire-fuzz-oracle-negative-control: build/conformance-harness build/wire-fuzz-cpp
	@rm -rf $(ORACLE_NC) && mkdir -p $(ORACLE_NC)
	@sed -e 's|if end < r.off {|if false { // NEGATIVE CONTROL: the clamp is gone|' \
		internal/tablewire/decode.go > $(ORACLE_NC)/decode.go.txt
	@cmp -s internal/tablewire/decode.go $(ORACLE_NC)/decode.go.txt && \
		{ echo "NEGATIVE CONTROL: the oracle sabotage patched nothing"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/tablewire/decode.go":"%s/$(ORACLE_NC)/decode.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > $(ORACLE_NC)/overlay.json
	go build -overlay $(ORACLE_NC)/overlay.json -o $(ORACLE_NC)/harness ./test/conformance/harness
	@if $(ORACLE_NC)/harness wire-fuzz --driver ./build/wire-fuzz-cpp --seed $(SEED) --n 0 \
			--failed $(ORACLE_NC)/failed.bin > $(ORACLE_NC)/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the oracle's clamp is gone and the wire fuzzer stayed green"; \
		cat $(ORACLE_NC)/log; exit 1; \
	fi
	@grep -q "the oracle PANICKED" $(ORACLE_NC)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the wire fuzzer went red, but not on an oracle panic"; \
		  cat $(ORACLE_NC)/log; exit 1; }
	@grep -q "stream_count_past_body" $(ORACLE_NC)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the panic was not on the pinned vector"; \
		  cat $(ORACLE_NC)/log; exit 1; }
	@grep -m1 "FAILED" $(ORACLE_NC)/log
	@echo "negative control: removing the oracle's body-span clamp turns the pinned vector RED"

# The GENERATED half of the data: the JSON text of every instance and the read
# report of every evolution case, both from the compiler's own engine.
.PHONY: conformance-generate
conformance-generate: build/conformance-harness
	./build/conformance-harness generate

# The PINNED half: the cook's canonical node dump and the block forgery battery
# resolved to byte offsets, both from the reference leg — C++ writes the pins,
# every other leg compares, exactly as the wire goldens work.
.PHONY: conformance-pin
conformance-pin: build/conformance-harness build/conformance-cpp build/schema_test_cook
	./build/conformance-harness pin

# THE NEGATIVE CONTROL, and a harness that has never gone red is watching
# nothing. One byte of ONE dump is flipped in a COPY of the C++ driver — no
# tracked file is written to, so an interrupt cannot leave a sabotaged working
# tree — and the harness must go red, on that surface and on no other. The
# second half is the point: a matrix whose every cell went red would be saying
# "something broke" rather than "the text form broke".
.PHONY: conformance-negative-control
conformance-negative-control: build/conformance-harness build/conformance-cpp
	@rm -rf build/conformance-negative && mkdir -p build/conformance-negative
	@sed 's|if ( !spill( out, f\[1\] + ".json", text.data(), (size_t) size ) ) return 1;|text[0] = (char) ( text[0] ^ 1 ); if ( !spill( out, f[1] + ".json", text.data(), (size_t) size ) ) return 1; // SABOTAGED|' \
		test/conformance/cpp/main.cpp > build/conformance-negative/main.cpp
	@cmp -s build/conformance-negative/main.cpp test/conformance/cpp/main.cpp && \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; } || true
	$(CXX) $(TABLES_CXXFLAGS) $(CONFORMANCE_INCLUDES) build/conformance-negative/main.cpp \
		$(CONFORMANCE_SOURCES) -o build/conformance-negative/driver-bin
	@printf '#!/bin/sh\nexec build/conformance-negative/driver-bin "$$@"\n' > build/conformance-negative/driver
	@chmod +x build/conformance-negative/driver
	@printf 'cpp build/conformance-negative/driver\n' > build/conformance-negative/drivers.txt
	@if ./build/conformance-harness run --drivers build/conformance-negative/drivers.txt \
			--work build/conformance-negative/work > build/conformance-negative/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: one byte off in a dump left the harness green"; \
		cat build/conformance-negative/log; exit 1; \
	fi
	@grep -q "cpp / json-write" build/conformance-negative/log || \
		{ echo "NEGATIVE CONTROL FAILED: the harness went red, but not on the sabotaged surface"; \
		  cat build/conformance-negative/log; exit 1; }
	@grep -q "wire          pass" build/conformance-negative/log || \
		{ echo "NEGATIVE CONTROL FAILED: the whole matrix went red, so it localises nothing"; \
		  cat build/conformance-negative/log; exit 1; }
	@grep -m1 "cpp / json-write" build/conformance-negative/log
	@echo "negative control: one byte off in one dump turns the harness RED on that surface alone"

# THE NEGATIVE CONTROL FOR THE BLOCK ROW DUMP (docs/SPEC-TABLES.md §19.2), and it
# sabotages neither driver: it flips one byte INSIDE A ROW of the block image
# itself. That is the whole reason the surface exists. `Open` reads the prologue
# and the triples and nothing else (§19.2's O(1) promise), so a byte inside a
# row cannot move its answer — `block` must stay GREEN and `block-dump` must go
# RED, which is the harness saying "the reader opened it and then read it
# wrong". A control that turned both red would be proving the image was
# corrupted, not that anyone reads rows.
#
# The byte is ships row 0's `object_id`: the array starts at 320 and the field
# sits at 64 inside a row, both facts the pinned dump states. If the fixture's
# layout ever moves under it the `cmp` below fails loudly rather than the
# control quietly checking nothing.
CONFORMANCE_NEGATIVE_BLOCK := build/conformance-negative-block

.PHONY: conformance-negative-control-block-dump
conformance-negative-control-block-dump: build/conformance-harness build/conformance-cpp
	@rm -rf $(CONFORMANCE_NEGATIVE_BLOCK) && mkdir -p $(CONFORMANCE_NEGATIVE_BLOCK)
	@cp testdata/wire/tables/block_render.bin $(CONFORMANCE_NEGATIVE_BLOCK)/block_render.bin
	@printf '\001' | dd of=$(CONFORMANCE_NEGATIVE_BLOCK)/block_render.bin bs=1 seek=384 count=1 conv=notrunc 2>/dev/null
	@cmp -s testdata/wire/tables/block_render.bin $(CONFORMANCE_NEGATIVE_BLOCK)/block_render.bin && \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage changed no byte of the image"; exit 1; } || true
	@sed 's|testdata/wire/tables/block_render.bin|$(CONFORMANCE_NEGATIVE_BLOCK)/block_render.bin|' \
		testdata/conformance/tables/MANIFEST.txt > $(CONFORMANCE_NEGATIVE_BLOCK)/MANIFEST.txt
	@printf 'cpp test/conformance/cpp/driver\n' > $(CONFORMANCE_NEGATIVE_BLOCK)/drivers.txt
	@if ./build/conformance-harness run --manifest $(CONFORMANCE_NEGATIVE_BLOCK)/MANIFEST.txt \
			--drivers $(CONFORMANCE_NEGATIVE_BLOCK)/drivers.txt \
			--work $(CONFORMANCE_NEGATIVE_BLOCK)/work > $(CONFORMANCE_NEGATIVE_BLOCK)/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: one byte off inside a row left the harness green"; \
		cat $(CONFORMANCE_NEGATIVE_BLOCK)/log; exit 1; \
	fi
	@grep -q "cpp / block-dump" $(CONFORMANCE_NEGATIVE_BLOCK)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the harness went red, but not on block-dump"; \
		  cat $(CONFORMANCE_NEGATIVE_BLOCK)/log; exit 1; }
	@grep -q "^block         pass" $(CONFORMANCE_NEGATIVE_BLOCK)/log || \
		{ echo "NEGATIVE CONTROL FAILED: block went red too, so the control does not localise the ROW READ"; \
		  cat $(CONFORMANCE_NEGATIVE_BLOCK)/log; exit 1; }
	@grep -q "^forgery       pass" $(CONFORMANCE_NEGATIVE_BLOCK)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the whole matrix went red, so it localises nothing"; \
		  cat $(CONFORMANCE_NEGATIVE_BLOCK)/log; exit 1; }
	@grep -m1 "cpp / block-dump" $(CONFORMANCE_NEGATIVE_BLOCK)/log
	@echo "negative control: one byte off INSIDE A ROW turns the harness RED on block-dump alone — block still opens"

.PHONY: conformance-generate conformance-pin conformance-negative-control conformance-negative-control-block-dump

# ---------------------------------------------------------------------------
# THE LANGUAGE REGISTRY (docs/CONTRIBUTING.md, "Adding a language") ----------
#
# Every language is one file, make/<lang>.mk, included here by wildcard and
# named nowhere in this Makefile. A file registers its leg by appending to the
# lists below, and the aggregate targets under the include are the only places
# the lists are read — so a port adds its file and edits nothing here.
#
#   TEST_LEGS          test-<lang>: the leg's whole `make test` half — its
#                      generated trees, its gates and negative controls, its
#                      packet and table tests
#   CONFORMANCE_LEGS   what `make conformance` builds before the harness runs
#   CONFORMANCE_ENV    environment the harness run carries to the drivers
#   BENCH_TABLES_LEGS  the generated unit a leg of `make bench-tables` needs
#   GOLDENS_LEGS       update-goldens-<lang>: the leg's committed table goldens
# THE WIDE-SCALAR REFUSAL GATE (docs/SPEC-TABLES.md §3, §15): every scalar the
# type wire carries rides in a table in the C++ reference and the tool, and a
# port that has not landed the kinds yet must REFUSE a unit declaring them, by
# name, rather than emit a second wire for them. tables/scalars is the unit that
# declares each; every DISCOVERED port (make/<lang>.mk, the registry) is run
# over it and either generates the unit — it carries the kinds, and the
# conformance matrix holds it to the corpus — or stops with the diagnostic that
# names the fields and the follow-on. Nothing here lists a language.
.PHONY: tables-ports-refuse-wide-scalars
tables-ports-refuse-wide-scalars: bin/schema
	@rm -rf build/tables-wide-refusal && mkdir -p build/tables-wide-refusal
	@carry=0; refuse=0; \
	for lang in $(patsubst make/%.mk,%,$(wildcard make/*.mk)); do \
		if ./bin/schema generate --lang $$lang --out build/tables-wide-refusal/$$lang tables/scalars > build/tables-wide-refusal/$$lang.log 2>&1; then \
			carry=$$((carry+1)); continue; \
		fi; \
		grep -q "does not carry the fixed-point and 128-bit table-wire kinds yet" build/tables-wide-refusal/$$lang.log || \
			{ echo "REFUSAL GATE FAILED: the $$lang backend stopped for another reason:"; cat build/tables-wide-refusal/$$lang.log; exit 1; }; \
		grep -q "SimState.reach fixed(112, 16)" build/tables-wide-refusal/$$lang.log || \
			{ echo "REFUSAL GATE FAILED: the $$lang backend does not name the refused field"; exit 1; }; \
		refuse=$$((refuse+1)); \
	done; \
	if [ $$((carry+refuse)) -eq 0 ]; then echo "REFUSAL GATE FAILED: no port discovered under make/"; exit 1; fi; \
	echo "tables wide-scalar refusal gate: $$refuse ports refuse the unit by name, naming every wide field and the follow-on; $$carry carry the kinds"

# THE WIDE-SCALAR LAYOUT ASSERTS (docs/SPEC-TABLES.md §7.2, §19.3): the scalars
# unit's block form is compiled on its own, which is what makes its static_asserts
# — sixteen bytes at sixteen for every 128-bit field, the compiler's one model
# against this compiler's — a build fact rather than a claim.
.PHONY: tables-scalars-block-asserts
tables-scalars-block-asserts: build/tables-generated/.stamp
	@mkdir -p build
	$(CXX) $(BLOCK_CXXFLAGS) -I$(SERIALIZE) -Ibuild/tables-generated/scalars -c build/tables-generated/scalars/ScalarsBlock.cpp -o build/scalars-block.o
	@echo "tables wide-scalar layout asserts: the scalars block form compiles, every sizeof, alignof and offsetof asserted"

include $(wildcard make/*.mk)

# THE CONFORMANCE MATRIX (test/conformance/README.md): every discovered driver
# over every surface it lists. The reference leg is C++ and is built here; the
# rest are what the legs registered.
.PHONY: conformance
conformance: build/conformance-harness build/conformance-cpp build/schema_test_cook $(CONFORMANCE_LEGS)
	$(CONFORMANCE_ENV) ./build/conformance-harness run

# THE TABLES BENCH PASS (bench/tables/README.md): every leg under
# bench/tables/*/leg, results under bench/tables/results/. A PUBLISHABLE
# number is a box sitting under the estate's bench rules — core 15, server
# stopped, not live, blessed per run; a run on a shared interactive machine is
# a pairing check and the board says which one it is.
.PHONY: bench-tables
bench-tables: generated/bench/tables/cpp/.stamp bench-table-check $(BENCH_TABLES_LEGS)
	bench/tables/run.sh

# What the include registered, one list per line. The registry gate
# (test/conformance/harness/registry_test.go) reads it to prove a planted
# language is discovered without an edit to this file.
.PHONY: registry
registry:
	@echo "test: $(TEST_LEGS)"
	@echo "conformance: $(CONFORMANCE_LEGS)"
	@echo "bench-tables: $(BENCH_TABLES_LEGS)"
	@echo "goldens: $(GOLDENS_LEGS)"
