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


# THE EMITTED C++ IS ANALYZER-CLEAN AND FORMAT-CANONICAL (docs/PORTING.md J4,
# issue #424). tables-cpp-clean holds every generated C++ file of the table
# corpus, the wide corpus and the committed generated/ C++ trees (tables AND
# packets) to two things, both run in parallel and neither touching shared state:
#
#   (a) FORMAT: clang-format in the repo's .clang-format (the C leg's, one
#       house C and C++ style: four spaces, Allman braces, `( x )`, `type * name`)
#       writes the file back unchanged. The version is the C leg's pin
#       (CLANG_FORMAT_PIN in make/c.mk); a different major fails by name.
#   (b) WARNINGS: clang++ -Weverything, every generated header compiled as the
#       one-line translation unit a consumer writes, every .cpp as it is, the
#       runtime -isystem. The ONE thing dropped is the c++98-compat family
#       (-Wno-c++98-compat -Wno-c++98-compat-pedantic): the emitted code is
#       C++17 by design, so "not valid C++98" is not a finding. No pragma and no
#       other flag is silenced; a warning is fixed at the emitter that wrote it.
#
# Both ride a SHRINK-ONLY RATCHET (make/cpp-format-allow.txt, one path a line;
# make/cpp-warn-allow.txt, one path and its warning kinds a line): the table
# emitter's output is not clean yet. A new finding not on the list fails; a
# listed one that no longer occurs fails until its line is deleted. The packet
# emitter's files carry on the warning list only the three kinds that cannot
# be cleared without altering the packet wire (see the list's own header) and
# are on the format list never. Where the tool is absent the gate SKIPS, except
# under SCHEMA_REQUIRE_CLANG_FORMAT=1 / SCHEMA_REQUIRE_CLANGXX=1 (CI sets both):
# a gate that always skips is no gate.
CLANGXX ?= clang++
CPP_CLEAN_TREES := build/tables-generated build/wide-generated generated/cpp generated/bench/cpp generated/bench/tables/cpp generated/bench/paired/cpp
CPP_CLEAN_STAMPS := build/tables-generated/.stamp build/wide-generated/.stamp generated/cpp/.stamp generated/cpp/ludicrous/.stamp generated/bench/cpp/.stamp generated/bench/tables/cpp/.stamp generated/bench/paired/cpp/.stamp
CPP_FORMAT_ALLOW := make/cpp-format-allow.txt
CPP_WARN_ALLOW   := make/cpp-warn-allow.txt

.PHONY: tables-cpp-clean tables-cpp-format tables-cpp-warnings tables-cpp-clean-census
tables-cpp-clean: tables-cpp-format tables-cpp-warnings

# the two halves are separate targets so a parallel make runs them together;
# $(MAKE) -j runs them and each fans out over the cores itself
tables-cpp-format: $(CPP_CLEAN_STAMPS) make/checks/cpp-clean-format.sh $(CPP_FORMAT_ALLOW)
	@if ! command -v $(CLANG_FORMAT) >/dev/null 2>&1; then \
		if [ "$(SCHEMA_REQUIRE_CLANG_FORMAT)" = 1 ]; then echo "tables-cpp-clean: $(CLANG_FORMAT) is not installed and SCHEMA_REQUIRE_CLANG_FORMAT=1"; exit 1; fi; \
		echo "SKIP tables-cpp-format: $(CLANG_FORMAT) is not installed"; exit 0; \
	fi; \
	v=$$($(CLANG_FORMAT) --version | sed -n 's/.*version \([0-9][0-9]*\)\..*/\1/p'); \
	if [ "$$v" != "$(CLANG_FORMAT_MAJOR)" ]; then \
		echo "tables-cpp-clean: $(CLANG_FORMAT) is major $$v; the gate is pinned to $(CLANG_FORMAT_PIN) (major $(CLANG_FORMAT_MAJOR)) — install it, or pass CLANG_FORMAT=<that binary>"; exit 1; \
	fi; \
	OUT=build/cpp-clean-format CLANG_FORMAT=$(CLANG_FORMAT) sh make/checks/cpp-clean-format.sh $(CPP_FORMAT_ALLOW) check $(CPP_CLEAN_TREES)

tables-cpp-warnings: $(CPP_CLEAN_STAMPS) make/checks/cpp-clean-warnings.sh make/checks/cpp-clean-one.sh $(CPP_WARN_ALLOW)
	@if ! $(CLANGXX) --version 2>/dev/null | grep -qi clang; then \
		if [ "$(SCHEMA_REQUIRE_CLANGXX)" = 1 ]; then echo "tables-cpp-clean: $(CLANGXX) is not a clang and SCHEMA_REQUIRE_CLANGXX=1"; exit 1; fi; \
		echo "SKIP tables-cpp-warnings: $(CLANGXX) is not a clang (-Weverything is clang's)"; exit 0; \
	fi; \
	test -f $(SERIALIZE)/serialize.h || { echo "tables-cpp-clean: $(SERIALIZE)/serialize.h not found (SERIALIZE names the serialize checkout)"; exit 1; }; \
	OUT=build/cpp-clean-warn CLANGXX=$(CLANGXX) SERIALIZE_DIR=$(SERIALIZE) sh make/checks/cpp-clean-warnings.sh $(CPP_WARN_ALLOW) check $(CPP_CLEAN_TREES)

# what the two ratchets should hold today (a maintainer's tool, never a gate)
tables-cpp-clean-census: $(CPP_CLEAN_STAMPS)
	@OUT=build/cpp-clean-census-format CLANG_FORMAT=$(CLANG_FORMAT) sh make/checks/cpp-clean-format.sh $(CPP_FORMAT_ALLOW) census $(CPP_CLEAN_TREES) > build/cpp-format-census.txt
	@OUT=build/cpp-clean-census-warn CLANGXX=$(CLANGXX) SERIALIZE_DIR=$(SERIALIZE) sh make/checks/cpp-clean-warnings.sh $(CPP_WARN_ALLOW) census $(CPP_CLEAN_TREES) > build/cpp-warn-census.txt
	@echo "wrote build/cpp-format-census.txt ($$(wc -l < build/cpp-format-census.txt | tr -d ' ') files) and build/cpp-warn-census.txt ($$(wc -l < build/cpp-warn-census.txt | tr -d ' ') files)"

test: tables-cpp-clean

# THE CONTROLS (make/negative-controls.json). Each one rebuilds the compiler
# under a Go overlay with ONE planted fault in the PACKET emitter
# (tools/sabotage/cpp_clean.go), generates the examples corpus with it, and
# requires the gate's own engine to go red naming the file. The packet emitter's
# output is held to zero, so the control needs no ratchet entry to hide in. The
# guard comes first: the unsabotaged emitter's output must be GREEN under the
# same engine and the same ratchet, or the control would be watching a tree
# that was red anyway.
#
# WARNINGS: one placement-new written with a C-style cast again (-Wold-style-cast);
# the control needs clang++ only. FORMAT: one struct brace written K&R; the
# control needs the pinned clang-format, as tables-cpp-format does, so it
# refuses by name when that does not resolve (it is on the exclusion list of the
# plan and runs in the leg that installs the pin).
CPP_CLEAN_CONTROL := build/cpp-clean-control

.PHONY: tables-cpp-warnings-negative-control
tables-cpp-warnings-negative-control: bin/schema tools/sabotage/cpp_clean.go make/checks/cpp-clean-warnings.sh make/checks/cpp-clean-one.sh
	@$(CLANGXX) --version 2>/dev/null | grep -qi clang || { echo "NEGATIVE CONTROL CANNOT RUN: $(CLANGXX) is not a clang"; exit 1; }
	@test -f $(SERIALIZE)/serialize.h || { echo "NEGATIVE CONTROL CANNOT RUN: $(SERIALIZE)/serialize.h not found"; exit 1; }
	@rm -rf $(CPP_CLEAN_CONTROL)/warn && mkdir -p $(CPP_CLEAN_CONTROL)/warn
	./bin/schema generate --lang cpp --out $(CPP_CLEAN_CONTROL)/warn/clean examples
	@OUT=$(CPP_CLEAN_CONTROL)/warn/census CLANGXX=$(CLANGXX) SERIALIZE_DIR=$(SERIALIZE) sh make/checks/cpp-clean-warnings.sh /dev/null census $(CPP_CLEAN_CONTROL)/warn/clean > $(CPP_CLEAN_CONTROL)/warn/ratchet.txt
	@OUT=$(CPP_CLEAN_CONTROL)/warn/guard CLANGXX=$(CLANGXX) SERIALIZE_DIR=$(SERIALIZE) sh make/checks/cpp-clean-warnings.sh $(CPP_CLEAN_CONTROL)/warn/ratchet.txt check $(CPP_CLEAN_CONTROL)/warn/clean > $(CPP_CLEAN_CONTROL)/warn/guard.out 2>&1 || \
		{ echo "NEGATIVE CONTROL FAILED: the unsabotaged packet emitter is not green under its own census, so this control would watch a tree that was red anyway"; cat $(CPP_CLEAN_CONTROL)/warn/guard.out; exit 1; }
	@! grep -q old-style-cast $(CPP_CLEAN_CONTROL)/warn/ratchet.txt || \
		{ echo "NEGATIVE CONTROL FAILED: the unsabotaged packet emitter already draws -Wold-style-cast, so the planted cast would prove nothing"; exit 1; }
	go run ./tools/sabotage -name cpp-clean-old-style-cast -out $(CPP_CLEAN_CONTROL)/warn/cpp.gotext internal/codegen/cpp/cpp.go
	@printf '{"Replace":{"%s/internal/codegen/cpp/cpp.go":"%s/$(CPP_CLEAN_CONTROL)/warn/cpp.gotext"}}\n' "$(CURDIR)" "$(CURDIR)" > $(CPP_CLEAN_CONTROL)/warn/overlay.json
	go build -overlay $(CPP_CLEAN_CONTROL)/warn/overlay.json -o $(CPP_CLEAN_CONTROL)/warn/schema ./cmd/schema
	$(CPP_CLEAN_CONTROL)/warn/schema generate --lang cpp --out $(CPP_CLEAN_CONTROL)/warn/sabotaged examples
	@grep -q '::new ( (void \*) &' $(CPP_CLEAN_CONTROL)/warn/sabotaged/ArmDefaults.h || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotaged emitter wrote no C-style cast"; exit 1; }
	@if OUT=$(CPP_CLEAN_CONTROL)/warn/red CLANGXX=$(CLANGXX) SERIALIZE_DIR=$(SERIALIZE) sh make/checks/cpp-clean-warnings.sh $(CPP_CLEAN_CONTROL)/warn/ratchet.txt check $(CPP_CLEAN_CONTROL)/warn/sabotaged > $(CPP_CLEAN_CONTROL)/warn/red.out 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a C-style cast in the emitted C++ left the warnings gate green"; cat $(CPP_CLEAN_CONTROL)/warn/red.out; exit 1; \
	fi
	@grep -q 'warn/sabotaged/ArmDefaults\.h old-style-cast' $(CPP_CLEAN_CONTROL)/warn/red.out || \
		{ echo "NEGATIVE CONTROL FAILED: the warnings gate went red, but not on -Wold-style-cast in ArmDefaults.h"; cat $(CPP_CLEAN_CONTROL)/warn/red.out; exit 1; }
	@echo "negative control: one C-style cast in the emitted C++ turns the warnings gate (clang++ -Weverything) RED naming -Wold-style-cast"

.PHONY: tables-cpp-format-negative-control
tables-cpp-format-negative-control: bin/schema tools/sabotage/cpp_clean.go make/checks/cpp-clean-format.sh
	@command -v $(CLANG_FORMAT) >/dev/null 2>&1 && [ "$$($(CLANG_FORMAT) --version | sed -n 's/.*version \([0-9][0-9]*\)\..*/\1/p')" = "$(CLANG_FORMAT_MAJOR)" ] || \
		{ echo "NEGATIVE CONTROL CANNOT RUN: clang-format $(CLANG_FORMAT_PIN) does not resolve (CLANG_FORMAT = $(CLANG_FORMAT)), and a drift control without the formatter proves nothing"; exit 1; }
	@rm -rf $(CPP_CLEAN_CONTROL)/format && mkdir -p $(CPP_CLEAN_CONTROL)/format && : > $(CPP_CLEAN_CONTROL)/format/empty.txt
	./bin/schema generate --lang cpp --out $(CPP_CLEAN_CONTROL)/format/clean examples
	@OUT=$(CPP_CLEAN_CONTROL)/format/guard CLANG_FORMAT=$(CLANG_FORMAT) sh make/checks/cpp-clean-format.sh $(CPP_CLEAN_CONTROL)/format/empty.txt check $(CPP_CLEAN_CONTROL)/format/clean > $(CPP_CLEAN_CONTROL)/format/guard.out 2>&1 || \
		{ echo "NEGATIVE CONTROL FAILED: the unsabotaged packet emitter's output is not format-clean, so this control would watch a tree that was red anyway"; cat $(CPP_CLEAN_CONTROL)/format/guard.out; exit 1; }
	go run ./tools/sabotage -name cpp-clean-format-drift -out $(CPP_CLEAN_CONTROL)/format/cpp.gotext internal/codegen/cpp/cpp.go
	@printf '{"Replace":{"%s/internal/codegen/cpp/cpp.go":"%s/$(CPP_CLEAN_CONTROL)/format/cpp.gotext"}}\n' "$(CURDIR)" "$(CURDIR)" > $(CPP_CLEAN_CONTROL)/format/overlay.json
	go build -overlay $(CPP_CLEAN_CONTROL)/format/overlay.json -o $(CPP_CLEAN_CONTROL)/format/schema ./cmd/schema
	$(CPP_CLEAN_CONTROL)/format/schema generate --lang cpp --out $(CPP_CLEAN_CONTROL)/format/sabotaged examples
	@grep -q '^struct [A-Za-z0-9_]* {$$' $(CPP_CLEAN_CONTROL)/format/sabotaged/Types.h || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotaged emitter wrote no K&R brace"; exit 1; }
	@if OUT=$(CPP_CLEAN_CONTROL)/format/red CLANG_FORMAT=$(CLANG_FORMAT) sh make/checks/cpp-clean-format.sh $(CPP_CLEAN_CONTROL)/format/empty.txt check $(CPP_CLEAN_CONTROL)/format/sabotaged > $(CPP_CLEAN_CONTROL)/format/red.out 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a K&R struct brace in the emitted C++ left the format gate green"; cat $(CPP_CLEAN_CONTROL)/format/red.out; exit 1; \
	fi
	@grep -q 'clang-format drift in .*/format/sabotaged/Types\.h' $(CPP_CLEAN_CONTROL)/format/red.out || \
		{ echo "NEGATIVE CONTROL FAILED: the format gate went red, but not on Types.h"; cat $(CPP_CLEAN_CONTROL)/format/red.out; exit 1; }
	@echo "negative control: one K&R struct brace in the emitted C++ turns the format gate (clang-format) RED naming the file"
