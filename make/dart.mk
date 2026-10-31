# make/dart.mk — the Dart leg (docs/CONTRIBUTING.md, "Adding a language").
# Included by the Makefile's wildcard include; the Makefile names no language.
# The leg registers itself at the end of this file.

# The Dart SDK, pinned per project (generated Dart is self-contained — no
# runtime checkout — so the SDK is the only Dart dependency). The default
# points at the repo-local unpacked SDK; CI installs the same version and
# overrides with DART=dart. To populate dist/ (gitignored):
#   Dart SDK 3.13.2 (stable, macos-arm64)
#   url:    https://storage.googleapis.com/dart-archive/channels/stable/release/3.13.2/sdk/dartsdk-macos-arm64-release.zip
#   sha256: 1e79f51341937f84cc1563a3fcad4a91706e35dee72bda69f4e955065c0e373a
#   unzip into dist/ and rename dart-sdk -> dart-sdk-3.13.2
DART ?= $(CURDIR)/dist/dart-sdk-3.13.2/bin/dart
# The SDK version the JIT float measurement (tables-dart-float-jit) certifies
# on; the gate refuses any other before it measures.
DART_SDK_PIN := 3.13.2

# THE OPTIONAL UNIT REGISTRY (SPEC-TABLES §8.7): a generated Dart program
# walks UnitView() for every VIEW_CORPUS unit, byte-compared with the IR pin.
.PHONY: tables-dart-view
tables-dart-view: bin/schema test/dart-tables/view_main.dart
	@rm -rf build/dart-view && mkdir -p build/dart-view
	@set -e; for entry in $(VIEW_CORPUS); do \
		dir=$${entry%%:*}; pkg=$${entry##*:}; \
		cap=$$(printf '%s' "$$pkg" | cut -c1 | tr 'a-z' 'A-Z')$$(printf '%s' "$$pkg" | cut -c2-); \
		source=tables/$$dir; if [ "$$dir" = wide ]; then source=examples-wide; fi; \
		./bin/schema generate --lang dart --out build/dart-view/$$dir $$source; \
		sed "s|VIEW_LIBRARY|$$dir/$${cap}View.dart|" test/dart-tables/view_main.dart > build/dart-view/$$pkg-main.dart; \
		$(DART) compile exe -o build/dart-view/prog-$$pkg build/dart-view/$$pkg-main.dart; \
		./build/dart-view/prog-$$pkg > build/dart-view/$$pkg.listing; \
	done
	SCHEMA_VIEW_LISTING_DIR="$(CURDIR)/build/dart-view" go test -count=1 -timeout 600s ./internal/viewlisting -run TestUnitViewListingMatchesTheIR
	@echo "Dart unit registry: $(words $(VIEW_CORPUS)) units match the independent listing"

test-dart: tables-dart-view

# THE TOOLCHAIN GATE, this leg's half (issue #599; the Makefile's header and
# docs/CONTRIBUTING.md, "Adding a language"). This leg is the one the issue was
# opened over: a merge deleted the clone's dist link, the leg was passed over
# in silence, and the red inside it rode a green run.
.PHONY: toolchain-dart
toolchain-dart:
	@$(call toolchain_probe,dart,DART,$(DART))
build/packet-defaults/dart/.stamp: bin/schema test/packet-defaults/Defaults.schema test/packet-defaults/Plain.schema make/dart.mk
	./bin/schema generate --lang dart --out build/packet-defaults/dart/defaults test/packet-defaults/Defaults.schema
	./bin/schema generate --lang dart --out build/packet-defaults/dart/plain test/packet-defaults/Plain.schema
	@touch $@

.PHONY: packet-defaults-dart packet-defaults-dart-negative-control
packet-defaults-dart: build/packet-defaults/dart/.stamp packet-defaults-cpp
	$(DART) analyze build/packet-defaults/dart test/packet-defaults/dart
	$(DART) format --set-exit-if-changed --output=none build/packet-defaults/dart
	$(DART) --enable-asserts test/packet-defaults/dart/main.dart testdata/wire/packet-defaults
	$(DART) compile exe -o build/packet-defaults/dart/checker test/packet-defaults/dart/main.dart
	./build/packet-defaults/dart/checker testdata/wire/packet-defaults

packet-defaults-dart-negative-control: packet-defaults-dart
	@mkdir -p build/packet-defaults/dart-negative
	go run ./tools/sabotage -name packet-defaults-dart-constructor-bytes \
		-out build/packet-defaults/dart-negative/dart.gotext internal/codegen/dart/dart.go
	@printf '{"Replace":{"%s/internal/codegen/dart/dart.go":"%s/build/packet-defaults/dart-negative/dart.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/packet-defaults/dart-negative/overlay.json
	go build -overlay=build/packet-defaults/dart-negative/overlay.json -o build/packet-defaults/dart-negative/schema ./cmd/schema
	./build/packet-defaults/dart-negative/schema generate --lang dart --out build/packet-defaults/dart-negative/generated test/packet-defaults/Defaults.schema
	@sed -e 's|../../../build/packet-defaults/dart/defaults|$(CURDIR)/build/packet-defaults/dart-negative/generated|' \
		-e 's|../../../build/packet-defaults/dart/plain|$(CURDIR)/build/packet-defaults/dart/plain|' \
		test/packet-defaults/dart/main.dart > build/packet-defaults/dart-negative/main.dart
	$(DART) compile exe -o build/packet-defaults/dart-negative/checker build/packet-defaults/dart-negative/main.dart
	@if ./build/packet-defaults/dart-negative/checker testdata/wire/packet-defaults > build/packet-defaults/dart-negative/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: missing constructor bytes passed in Dart'; exit 1; fi
	@grep -Fq 'packet-default constructor bytes' build/packet-defaults/dart-negative/log || \
		{ echo 'NEGATIVE CONTROL FAILED: Dart failed for another reason'; cat build/packet-defaults/dart-negative/log; exit 1; }
	@echo 'packet defaults Dart negative control: missing constructor bytes fail the runtime check'

test-dart: packet-defaults-dart packet-defaults-dart-negative-control

# the Dart target: generated libraries only, no wiring file at all —
# generated Dart is self-contained (the bitpacker is inlined per issue #155),
# so there is no runtime checkout and no pubspec; the test legs import the
# generated files by relative path directly
generated/dart/.stamp: bin/schema $(SCHEMAS)
	./bin/schema generate --lang dart --out generated/dart examples
	@touch $@

generated/dart-ludicrous/.stamp: bin/schema $(SCHEMAS128)
	./bin/schema generate --lang dart --out generated/dart-ludicrous examples128
	@touch $@

generated/bench/dart/.stamp: bin/schema $(SCHEMAS_BENCH)
	./bin/schema generate --lang dart --out generated/bench/dart bench/corpus/Bench.schema
	./bin/schema generate --lang dart --out generated/bench/dart/realworld bench/corpus/RealWorld.schema
	@touch $@

# THE PAIRED UNIT, for bench/tables/dart — the FIXED FORM's leg in `bench/paired`
# (bench/paired/main.go, bench/tables/dart/table_main.dart). Bench.schema and
# FixedTable.schema are ONE unit, exactly as the C, C++, Go, JS and C# paired
# generations are: the fixed root reaches the packet type so the compiler emits
# its table codec. The runner imports these libraries by relative path and
# `dart compile exe` compiles them into it; the driver regenerates the same
# files itself, so the two agree byte for byte or `generated-current` says so.
generated/bench/paired/dart/.stamp: bin/schema bench/corpus/Bench.schema bench/corpus/FixedTable.schema
	@mkdir -p generated/bench/paired/dart
	./bin/schema generate --lang dart --out generated/bench/paired/dart bench/corpus/Bench.schema bench/corpus/FixedTable.schema
	@touch $@

 
# THE TABLES BOARD UNIT, for bench/tables/dart/leg (bench/tables/README.md): the
# VARIABLE form (form 1) of TableMixed, the board's representative corpus.
# Committed, like every other language's generated/bench/tables/<lang>.
generated/bench/tables/dart/.stamp: bin/schema bench/corpus/BenchTable.schema
	@mkdir -p generated/bench/tables/dart
	./bin/schema generate --lang dart --out generated/bench/tables/dart bench/corpus/BenchTable.schema
	@touch $@

# THE BENCH LEG'S GOLDEN GATE RUNS BEFORE THE CLOCK, with no clock at all: the
# golden, all 64 variants load clean and re-save byte-identically, and two
# rotations (docs/PORTING.md J5).
.PHONY: tables-dart-bench-gate
tables-dart-bench-gate: generated/bench/tables/dart/.stamp
	bench/tables/dart/leg build
	bench/tables/dart/leg run --gate
test-dart: tables-dart-bench-gate
BENCH_TABLES_LEGS += generated/bench/tables/dart/.stamp

# The same corpus through the DART table backend (docs/SPEC-TABLES.md): the tables
# corpus plus the evolution pair, generated at build time into build/ —
# test-only, never part of the committed generated/ tree. The full unit is
# generated (packet .dart + <Base>Block.dart + <Base>Cook.dart, and the wire
# forms where the unit owns them). Fixed-class units own form 3, and every
# carried root also owns form 1, including the V, W and R evolution pairs.
build/tables-generated-dart/.stamp: bin/schema $(wildcard tables/backend/*.schema) $(wildcard tables/maps/*.schema) $(wildcard tables/messages/*.schema) $(wildcard tables/scalars/*.schema) $(wildcard tables/stream/*.schema) $(wildcard tables/vocab/*.schema) $(wildcard tables/vocab9/*.schema) $(wildcard examples-wide/*.schema) $(SCHEMAS_TABLES) $(SCHEMAS_TABLES_POINTERS) $(SCHEMAS_TABLES_BLOCK) $(SCHEMAS_TABLES_BLOBS) $(SCHEMAS_TABLES_LISTS) test/tables/V1.schema test/tables/V2.schema test/tables/W1.schema test/tables/W2.schema test/tables/R1.schema test/tables/R2.schema test/tables/P1.schema test/tables/P3.schema test/tables/G1.schema test/tables/M1.schema test/tables/M2.schema test/tables/P2.schema test/tables/K1.schema test/tables/K2.schema test/tables/A2.schema test/tables/Scalars2.schema test/tables/RT1.schema
	@mkdir -p build/tables-generated-dart
	./bin/schema generate --lang dart --out build/tables-generated-dart/examples tables/examples
	# the POINTERED unit: form 1 for Scene, form 3 and cook beside it.
	./bin/schema generate --lang dart --out build/tables-generated-dart/pointers tables/pointers
	./bin/schema generate --lang dart --out build/tables-generated-dart/blobs tables/blobs
	./bin/schema generate --lang dart --out build/tables-generated-dart/lists tables/lists
	./bin/schema generate --lang dart --out build/tables-generated-dart/maps tables/maps
	./bin/schema generate --lang dart --out build/tables-generated-dart/messages tables/messages
	# backend, vocab and vocab9 are also the MESSAGE FORM's units (docs/SPEC-TABLES.md
	# §3.3): the conformance driver's `message` surface reads their batches.
	./bin/schema generate --lang dart --out build/tables-generated-dart/backend tables/backend
	./bin/schema generate --lang dart --out build/tables-generated-dart/stream tables/stream
	./bin/schema generate --lang dart --out build/tables-generated-dart/vocab tables/vocab
	./bin/schema generate --lang dart --out build/tables-generated-dart/vocab9 tables/vocab9
	# tables/scalars beside the tree, not in it: the standalone and clean gates
	# walk this tree, and the conformance driver reads its form-1 library from
	# there. The packet emitter writes Int128.dart beside a table-only 128-bit
	# unit, so its form-3 libraries resolve under the strict analyzer too.
	./bin/schema generate --lang dart --out build/tables-generated-dart-scalars/scalars tables/scalars
	./bin/schema generate --lang dart --out build/tables-generated-dart/wide examples-wide
	./bin/schema generate --lang dart --out build/tables-generated-dart/block tables/block
	./bin/schema generate --lang dart --out build/tables-generated-dart/blockhome tables/blockhome
	./bin/schema generate --lang dart --out build/tables-generated-dart/v1 test/tables/V1.schema
	./bin/schema generate --lang dart --out build/tables-generated-dart/v2 test/tables/V2.schema
	./bin/schema generate --lang dart --out build/tables-generated-dart/w1 test/tables/W1.schema
	./bin/schema generate --lang dart --out build/tables-generated-dart/w2 test/tables/W2.schema
	./bin/schema generate --lang dart --out build/tables-generated-dart/r1 test/tables/R1.schema
	./bin/schema generate --lang dart --out build/tables-generated-dart/r2 test/tables/R2.schema
	./bin/schema generate --lang dart --out build/tables-generated-dart/p1 test/tables/P1.schema
	./bin/schema generate --lang dart --out build/tables-generated-dart/p3 test/tables/P3.schema
	./bin/schema generate --lang dart --out build/tables-generated-dart/g1 test/tables/G1.schema
	./bin/schema generate --lang dart --out build/tables-generated-dart/m1 test/tables/M1.schema
	./bin/schema generate --lang dart --out build/tables-generated-dart/m2 test/tables/M2.schema
	# RT1 is the RETAIN surfaces' unit (docs/SPEC-TABLES.md §6.6).
	./bin/schema generate --lang dart --out build/tables-generated-dart/rt1 test/tables/RT1.schema
	# the REPORT surface's remaining readers (docs/SPEC-TABLES.md §4, §5): the
	# pointer-presence middle P2, the id-table pair K1/K2, the arm retypes A2,
	# and Scalars2 (beside the tree, as tables/scalars is, for its Int128.dart).
	./bin/schema generate --lang dart --out build/tables-generated-dart/p2 test/tables/P2.schema
	./bin/schema generate --lang dart --out build/tables-generated-dart/k1 test/tables/K1.schema
	./bin/schema generate --lang dart --out build/tables-generated-dart/k2 test/tables/K2.schema
	./bin/schema generate --lang dart --out build/tables-generated-dart/a2 test/tables/A2.schema
	./bin/schema generate --lang dart --out build/tables-generated-dart-scalars/scalars2 test/tables/Scalars2.schema
	@touch $@

# THE DART TABLE SOURCES ARE FORMAT-CANONICAL. `dart format` is the language's
# one formatting authority, so an emitter that has to be hand-reflowed is an
# emitter that drifts; this gate holds the block and cook libraries of the corpus
# to what the formatter would write, and the analyzer holds it to what the
# language accepts.
.PHONY: tables-dart-clean
tables-dart-clean: build/tables-generated-dart/.stamp
	$(DART) analyze build/tables-generated-dart
	@rm -rf build/tables-dart-fmt && cp -r build/tables-generated-dart build/tables-dart-fmt
	@rm -f build/tables-dart-fmt/.stamp
	@$(DART) format build/tables-dart-fmt >/dev/null
	@for f in build/tables-generated-dart/*/*Block.dart \
		  build/tables-generated-dart/*/*Cook.dart; do \
		test -e $$f || continue; \
		cmp -s $$f build/tables-dart-fmt/$${f#build/tables-generated-dart/} || \
			{ echo "dart format drift in $$f"; exit 1; }; \
	done
	@echo "tables Dart: analyzer clean and format-canonical"

# ITS CONTROL: a copy of one generated block library carries a planted unused
# private declaration spelled off-format. The analyzer must reject the copy on
# unused_element, and the formatter must rewrite it, which is the drift test the
# gate above applies file by file. No generated file is written to.
.PHONY: tables-dart-clean-negative-control
tables-dart-clean-negative-control: build/tables-generated-dart/.stamp
	@rm -rf build/tables-dart-clean-nc && mkdir -p build/tables-dart-clean-nc/tree
	@cp build/tables-generated-dart/block/BlockdemoBlock.dart build/tables-dart-clean-nc/tree/BlockdemoBlock.dart
	@printf '\nint   _plantedUnused=1;\n' >> build/tables-dart-clean-nc/tree/BlockdemoBlock.dart
	@cp -r build/tables-dart-clean-nc/tree build/tables-dart-clean-nc/fmt
	@$(DART) format build/tables-dart-clean-nc/fmt >/dev/null
	@cmp -s build/tables-dart-clean-nc/tree/BlockdemoBlock.dart build/tables-dart-clean-nc/fmt/BlockdemoBlock.dart && \
		{ echo "NEGATIVE CONTROL FAILED: the planted off-format line is format-canonical, so the drift gate cannot see it"; exit 1; } || true
	@if $(DART) analyze build/tables-dart-clean-nc/tree > build/tables-dart-clean-nc/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the analyzer stayed green with an unused private declaration planted"; \
		cat build/tables-dart-clean-nc/log; exit 1; \
	fi
	@grep -Fq 'unused_element' build/tables-dart-clean-nc/log || \
		{ echo "NEGATIVE CONTROL FAILED: the analyzer went red for some other reason"; cat build/tables-dart-clean-nc/log; exit 1; }
	@echo "negative control: an off-format unused declaration is caught by the formatter drift test and by the analyzer's unused_element"

test-dart: tables-dart-clean-negative-control

# THE DART NAME-CLAIM NEGATIVE CONTROL (docs/SPEC-TABLES.md §11). Every
# library-scope spelling of the Dart table runtime is registered in
# internal/tablenames and refused as a schema declaration, and compiler's
# TestDartTableRuntimeNamesAreClaimed holds the registry to the emitted source.
# This plants an unregistered class in the runtime through `go test -overlay`
# and requires that test to go RED naming it. No tracked file is written to.
.PHONY: tables-dart-names-negative-control
tables-dart-names-negative-control:
	@rm -rf build/dart-names-nc && mkdir -p build/dart-names-nc
	@sed 's|^final class TableBlockInfo {$$|final class TableBogusUnregistered {}\n\nfinal class TableBlockInfo {|' \
		internal/codegen/darttable/blockruntime.go > build/dart-names-nc/runtime.go.txt
	@cmp -s internal/codegen/darttable/blockruntime.go build/dart-names-nc/runtime.go.txt && \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage matched nothing — the runtime moved"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/codegen/darttable/blockruntime.go":"%s/build/dart-names-nc/runtime.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/dart-names-nc/overlay.json
	@if go test -count=1 -overlay build/dart-names-nc/overlay.json -run TestDartTableRuntimeNamesAreClaimed \
			./compiler > build/dart-names-nc/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the name-claim test stayed green with an unregistered runtime class planted"; \
		cat build/dart-names-nc/log; exit 1; \
	fi
	@grep -q "emits TableBogusUnregistered and internal/tablenames does not register it" build/dart-names-nc/log || \
		{ echo "NEGATIVE CONTROL FAILED: the test went red for some other reason"; \
		  cat build/dart-names-nc/log; exit 1; }
	@grep -m1 "TableBogusUnregistered" build/dart-names-nc/log
	@echo "negative control: one unregistered runtime class turns the Dart name-claim test RED"

# THE STANDALONE GATE, Dart: a generated table library imports dart:* and its
# own unit's sibling files, and NOTHING ELSE — no package, no path, no runtime
# checkout. Every import line of every generated Dart file is read, and a
# sibling it names must exist beside it.
.PHONY: tables-dart-standalone
STANDALONE_DART_DIR ?= build/tables-generated-dart
tables-dart-standalone: build/tables-generated-dart/.stamp
	@for f in $(STANDALONE_DART_DIR)/*/*.dart; do \
		d=$$(dirname $$f); \
		grep -E "^import " $$f | sed -E "s/^import '([^']*)'.*/\1/" | while read -r imp; do \
			case "$$imp" in \
				dart:*) ;; \
				*/*|package:*) echo "STANDALONE GATE FAILED: $$f imports $$imp — outside the unit"; exit 1 ;; \
				*) [ -e "$$d/$$imp" ] || { echo "STANDALONE GATE FAILED: $$f imports $$imp, which is not beside it"; exit 1; } ;; \
			esac; \
		done || exit 1; \
	done
	@echo "tables Dart standalone gate: every generated library imports dart:* and its own unit's files only"

# THE STANDALONE GATE'S NEGATIVE CONTROL: one package import planted in the
# runtime through a copy of the emitter, and the gate must go red on it.
.PHONY: tables-dart-standalone-negative-control
tables-dart-standalone-negative-control:
	@rm -rf build/dart-standalone-nc && mkdir -p build/dart-standalone-nc
	@sed 's|h.WriteString("import '"'"'dart:typed_data'"'"';\n\n")|h.WriteString("import '"'"'dart:typed_data'"'"';\nimport '"'"'package:collection/collection.dart'"'"'; // SABOTAGED\n\n")|' \
		internal/codegen/darttable/block.go > build/dart-standalone-nc/darttable.go.txt
	@cmp -s internal/codegen/darttable/block.go build/dart-standalone-nc/darttable.go.txt && \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage matched nothing — the emitter moved"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/codegen/darttable/block.go":"%s/build/dart-standalone-nc/darttable.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/dart-standalone-nc/overlay.json
	go build -overlay build/dart-standalone-nc/overlay.json -o build/dart-standalone-nc/schema ./cmd/schema
	./build/dart-standalone-nc/schema generate --lang dart --out build/dart-standalone-nc/generated/block tables/block
	@grep -lq SABOTAGED build/dart-standalone-nc/generated/block/*.dart || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotaged emitter emitted no package import"; exit 1; }
	@if $(MAKE) -s tables-dart-standalone STANDALONE_DART_DIR=build/dart-standalone-nc/generated > build/dart-standalone-nc/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the standalone gate stayed green with a package import planted"; exit 1; \
	fi
	@grep -q "outside the unit" build/dart-standalone-nc/log || \
		{ echo "NEGATIVE CONTROL FAILED: the gate went red for some other reason"; cat build/dart-standalone-nc/log; exit 1; }
	@grep -m1 "outside the unit" build/dart-standalone-nc/log
	@echo "negative control: one planted package import turns the Dart standalone gate RED"

# THE DART ALLOCATION GATE (docs/SPEC-TABLES.md, docs/PORTING.md I1 and I14).
# Counts new-space scavenges between two stderr markers under --verbose_gc with
# the semi-space pinned to 1 MB so one 16-byte allocation per record triggers a
# scavenge every ~30,000 records. Holds every caller-owned row (fixed-load,
# fixed-measure, fixed-save, block, cook) to zero scavenges under AOT, prints
# the JIT counts and the variable-class card row beside them, and refuses to
# certify off the pinned Dart SDK 3.13.2 unless SCHEMA_DART_ALLOC_ANY_SDK=1 is
# set to report without certifying.
DART_GC_FLAGS = --verbose_gc --new_gen_semi_initial_size=1 --new_gen_semi_max_size=1
DART_GC_COUNT = awk '/gcgate: steady phase begins/{on=1;next} /gcgate: steady phase ends/{on=0} on && /Scavenge/{n++} END{print n+0}'
DART_GC_ITERS ?= 20000

build/dart_gcgate: build/dart-fixed/.stamp build/tables-generated-dart/.stamp cards-dart-generate test/dart-tables/gcgate.dart
	@mkdir -p build
	$(DART) analyze test/dart-tables/gcgate.dart
	$(DART) format --set-exit-if-changed --output=none test/dart-tables/gcgate.dart
	$(DART) compile exe -o $@ test/dart-tables/gcgate.dart >/dev/null

.PHONY: tables-dart-alloc tables-dart-alloc-negative-control tables-dart-alloc-runtime-negative-control
tables-dart-alloc: build/dart_gcgate build/fixedform-corpus/.stamp build/cook-fuzz/.stamp
	$(DART) test/dart-tables/gcgate.dart drive $(DART) $(CURDIR)/build/dart_gcgate $(DART_GC_ITERS)

tables-dart-alloc-negative-control: build/dart_gcgate build/fixedform-corpus/.stamp build/cook-fuzz/.stamp
	@mkdir -p build/dart-alloc-nc
	@if SCHEMA_DART_ALLOC_PLANT=fixed-load SCHEMA_DART_ALLOC_ROWS=fixed-load,fixed-save \
			$(DART) test/dart-tables/gcgate.dart drive $(DART) $(CURDIR)/build/dart_gcgate $(DART_GC_ITERS) \
			> build/dart-alloc-nc/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a planted allocation per record in fixed-load left the gate green"; \
		cat build/dart-alloc-nc/log; exit 1; \
	fi
	@grep -qE '^  fixed-load +[1-9][0-9]* +[0-9]+ +<= 0 +\[FAIL\]' build/dart-alloc-nc/log || \
		{ echo "NEGATIVE CONTROL FAILED: fixed-load did not go red on the planted allocation"; \
		  cat build/dart-alloc-nc/log; exit 1; }
	@grep -qE '^  fixed-save +0 +[0-9]+ +<= 0 +\[PASS\]' build/dart-alloc-nc/log || \
		{ echo "NEGATIVE CONTROL FAILED: neighbour row fixed-save did not stay green (localisation failed)"; \
		  cat build/dart-alloc-nc/log; exit 1; }
	@grep -E '^  fixed-(load|save)' build/dart-alloc-nc/log
	@echo "negative control: one planted allocation per record turns fixed-load RED while fixed-save stays GREEN"

tables-dart-alloc-runtime-negative-control: build/dart_gcgate build/fixedform-corpus/.stamp build/cook-fuzz/.stamp
	@mkdir -p build/dart-alloc-runtime-nc
	@if SCHEMA_DART_ALLOC_SDK_VERSION=0.0.0 SCHEMA_DART_ALLOC_ANY_SDK=0 SCHEMA_DART_ALLOC_ROWS=fixed-load \
			$(DART) test/dart-tables/gcgate.dart drive $(DART) $(CURDIR)/build/dart_gcgate 2000 \
			> build/dart-alloc-runtime-nc/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the allocation gate certified on stand-in SDK 0.0.0"; \
		cat build/dart-alloc-runtime-nc/log; exit 1; \
	fi
	@grep -qE '^  fixed-load +0 +[0-9]+ +<= 0 +\[PASS\]' build/dart-alloc-runtime-nc/log || \
		{ echo "NEGATIVE CONTROL FAILED: the gate did not print its observations before refusing"; \
		  cat build/dart-alloc-runtime-nc/log; exit 1; }
	@grep -Fq 'FAILED: allocation certification requires Dart SDK 3.13.2 (pinned in make/dart.mk), running 0.0.0' build/dart-alloc-runtime-nc/log || \
		{ echo "NEGATIVE CONTROL FAILED: the refusal did not name both the pinned 3.13.2 and running 0.0.0 SDKs"; \
		  cat build/dart-alloc-runtime-nc/log; exit 1; }
	@grep -Fq 'NOT CERTIFIED' build/dart-alloc-runtime-nc/log || \
		{ echo "NEGATIVE CONTROL FAILED: the gate did not state NOT CERTIFIED"; \
		  cat build/dart-alloc-runtime-nc/log; exit 1; }
	@SCHEMA_DART_ALLOC_SDK_VERSION=0.0.0 SCHEMA_DART_ALLOC_ANY_SDK=1 SCHEMA_DART_ALLOC_ROWS=fixed-load \
		$(DART) test/dart-tables/gcgate.dart drive $(DART) $(CURDIR)/build/dart_gcgate 2000 \
		> build/dart-alloc-runtime-nc/any.log 2>&1
	@grep -Fq 'NOT CERTIFIED' build/dart-alloc-runtime-nc/any.log || \
		{ echo "NEGATIVE CONTROL FAILED: SCHEMA_DART_ALLOC_ANY_SDK=1 did not report NOT CERTIFIED"; \
		  cat build/dart-alloc-runtime-nc/any.log; exit 1; }
	@echo "negative control: Dart allocation gate refuses stand-in SDK 0.0.0 and SCHEMA_DART_ALLOC_ANY_SDK=1 reports without certifying"

# THE DART TABLES SOAK (docs/PORTING.md I9). The C soak
# (test/c-tables/soak_main.c) is the model: a functional soak run from a make
# target, never a unit test. The soak loops Load, the reads, Measure and Save
# over the rows the allocation gate gates (fixed load, measure, save, block,
# cook), compiled to an AOT executable like the gate's certified row, running
# under the gate's VM flags, for TABLES_DART_SOAK_ITERATIONS passes (a count, not
# a clock, as the C++ and C# soaks are counted: `make shape-gate` refuses a
# timing primitive outside the sanctioned bench). The driver reads the
# scavenge count at sixteen evenly spaced samples after the warmup and fails if
# it moves, reporting the worker's RSS drift beside it as the weaker instrument.
DART_SOAK_GC_FLAGS = --verbose_gc --new_gen_semi_initial_size=1 --new_gen_semi_max_size=1
# The release act is about an hour on the studio (a pass is ~0.16 ms there);
# `test-dart` runs the short form below with an explicit count.
TABLES_DART_SOAK_ITERATIONS ?= 22500000

build/dart_tables_soak: build/dart-fixed/.stamp build/tables-generated-dart/.stamp cards-dart-generate build/fixedform-corpus/.stamp build/cook-fuzz/.stamp test/dart-tables/soak.dart
	@mkdir -p build
	$(DART) analyze test/dart-tables/soak.dart
	$(DART) format --set-exit-if-changed --output=none test/dart-tables/soak.dart
	$(DART) compile exe -o $@ test/dart-tables/soak.dart >/dev/null

.PHONY: tables-dart-soak tables-dart-soak-negative-control
tables-dart-soak: build/dart_tables_soak
	$(DART) test/dart-tables/soak.dart drive $(DART) $(CURDIR)/build/dart_tables_soak $(TABLES_DART_SOAK_ITERATIONS)

tables-dart-soak-negative-control: build/dart_tables_soak
	@mkdir -p build/dart-soak-nc
	@if SCHEMA_DART_ALLOC_PLANT=1 \
			$(DART) test/dart-tables/soak.dart drive $(DART) $(CURDIR)/build/dart_tables_soak 65536 \
			> build/dart-soak-nc/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a planted allocation per iteration left the soak green"; \
		cat build/dart-soak-nc/log; exit 1; \
	fi
	@grep -q 'SOAK FAILED' build/dart-soak-nc/log || \
		{ echo "NEGATIVE CONTROL FAILED: the soak went red for some other reason"; \
		  cat build/dart-soak-nc/log; exit 1; }
	@grep -qE 'soak: rss drift .*\[PASS\]' build/dart-soak-nc/log || \
		{ echo "NEGATIVE CONTROL FAILED: the RSS drift verdict was not green"; \
		  cat build/dart-soak-nc/log; exit 1; }
	@grep -E 'scavenges=' build/dart-soak-nc/log
	@echo "negative control: one planted allocation per iteration turns the Dart soak RED on the count"

# THE SHORT SOAK, REACHED FROM test-dart: the release act is about an hour
# (the default TABLES_DART_SOAK_ITERATIONS); the leg's own gate runs 65536 passes
# (about ten seconds on the studio) so CI stays quick.
.PHONY: tables-dart-soak-short tables-dart-soak-length
tables-dart-soak-short:
	$(MAKE) tables-dart-soak TABLES_DART_SOAK_ITERATIONS=65536
	$(MAKE) tables-dart-soak-length
	$(MAKE) tables-dart-soak-negative-control

# THE LENGTH CHECK: a 1024-pass soak must run 1024 passes and take its sixteen
# samples, so a worker that ignores TABLES_DART_SOAK_ITERATIONS (and would run its
# default while claiming the release act's count) is red here, not silently
# green.
tables-dart-soak-length: build/dart_tables_soak
	@mkdir -p build/dart-soak-len
	@$(DART) test/dart-tables/soak.dart drive $(DART) $(CURDIR)/build/dart_tables_soak 1024 \
		> build/dart-soak-len/log 2>&1 || { cat build/dart-soak-len/log; exit 1; }
	@grep -q 'soak: worker ran 1024 passes, driver took 16 samples' build/dart-soak-len/log || \
		{ echo "SOAK LENGTH FAILED: the worker did not run TABLES_DART_SOAK_ITERATIONS=1024"; \
		  cat build/dart-soak-len/log; exit 1; }
	@grep 'soak: worker ran' build/dart-soak-len/log
	@echo "soak length: TABLES_DART_SOAK_ITERATIONS=1024 ran 1024 passes"

test-dart: tables-dart-soak-short
# THE RUNTIME HOME GATE (docs/PORTING.md J2). A unit's shared Dart table
# runtime lives in one file named by the PACKAGE — <Package>Block.dart and
# <Package>Cook.dart — never by the file that happens to sort first, so a
# schema file that sorts earlier relocates nothing and the runtime is
# byte-identical across the two trees. The gate adds an earlier-sorting file
# to a copy of the unit and requires the homes not to move; the control puts
# the file-order rule back and requires them to move (#347).
.PHONY: tables-dart-runtime-home
tables-dart-runtime-home: bin/schema
	@rm -rf build/runtime-home-dart && mkdir -p build/runtime-home-dart/src
	@cp tables/examples/*.schema build/runtime-home-dart/src/
	@printf 'package tabledemo\n\ntable AaaRow\n{\n    tag uint8\n}\n' > build/runtime-home-dart/src/Aaa.schema
	@./bin/schema generate --lang dart --out build/runtime-home-dart/base tables/examples
	@./bin/schema generate --lang dart --out build/runtime-home-dart/added build/runtime-home-dart/src
	@for surface in Block Cook; do \
		if [ "$$surface" = "Block" ]; then marker='final class TableBlockInfo'; \
		else marker='final class TableCookInfo'; fi; \
		base=$$(cd build/runtime-home-dart/base && grep -lF "$$marker" *$$surface.dart); \
		added=$$(cd build/runtime-home-dart/added && grep -lF "$$marker" *$$surface.dart); \
		if [ "$$base" != "Tabledemo$$surface.dart" ] || [ "$$added" != "Tabledemo$$surface.dart" ]; then \
			echo "RUNTIME HOME GATE FAILED: the $$surface runtime is in $$base before the added file and $$added after — expected Tabledemo$$surface.dart both times"; exit 1; \
		fi; \
	done
	@grep -v "tableBuildVersion" build/runtime-home-dart/base/TabledemoBlock.dart > build/runtime-home-dart/base.block.strip
	@grep -v "tableBuildVersion" build/runtime-home-dart/added/TabledemoBlock.dart > build/runtime-home-dart/added.block.strip
	@cmp -s build/runtime-home-dart/base.block.strip build/runtime-home-dart/added.block.strip || \
		{ echo "RUNTIME HOME GATE FAILED: the Dart block runtime's bytes moved when the unit gained a file"; exit 1; }
	@grep -v "tableBuildVersion" build/runtime-home-dart/base/TabledemoCook.dart > build/runtime-home-dart/base.cook.strip
	@grep -v "tableBuildVersion" build/runtime-home-dart/added/TabledemoCook.dart > build/runtime-home-dart/added.cook.strip
	@cmp -s build/runtime-home-dart/base.cook.strip build/runtime-home-dart/added.cook.strip || \
		{ echo "RUNTIME HOME GATE FAILED: the Dart cook runtime's bytes moved when the unit gained a file"; exit 1; }
	@echo "runtime home gate (Dart): the block and cook runtimes stay in <Package><Surface>.dart when an earlier-sorting file joins the unit"

.PHONY: tables-dart-runtime-home-negative-control
tables-dart-runtime-home-negative-control: bin/schema tables-dart-runtime-home
	@sed 's|home := capitalize(u.Package)|home := ir.ProtocolIdHome(u) // SABOTAGED: back to the file order|' \
		internal/codegen/darttable/darttable.go > build/dartruntime-fileorder.gotext
	@grep -q SABOTAGED build/dartruntime-fileorder.gotext || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; }
	@printf '{"Replace":{"%s/internal/codegen/darttable/darttable.go":"%s/build/dartruntime-fileorder.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/dartruntime-overlay.json
	@go build -overlay=build/dartruntime-overlay.json -o build/schema-dartruntime-sabotaged ./cmd/schema
	@rm -rf build/runtime-home-dart/base-sabotage build/runtime-home-dart/added-sabotage
	@./build/schema-dartruntime-sabotaged generate --lang dart --out build/runtime-home-dart/base-sabotage tables/examples
	@./build/schema-dartruntime-sabotaged generate --lang dart --out build/runtime-home-dart/added-sabotage build/runtime-home-dart/src
	@base=$$(cd build/runtime-home-dart/base-sabotage && grep -lF "final class TableBlockInfo" *.dart); \
	 added=$$(cd build/runtime-home-dart/added-sabotage && grep -lF "final class TableBlockInfo" *.dart); \
	 if [ "$$base" = "$$added" ]; then \
		echo "NEGATIVE CONTROL FAILED: the file-order rule kept the runtime in $$base — the gate is watching nothing"; exit 1; \
	 fi; \
	 echo "runtime home negative control (Dart): the file-order rule moves the runtime from $$base to $$added"

test-dart: tables-dart-runtime-home tables-dart-runtime-home-negative-control

# THE DART PORT'S RELEASE GATE. certify.yml DERIVES this target by name, so a
# port lands its expensive half by adding the target and nothing else: two
# hundred and eighty thousand forgery-fuzz mutants over the block and cook
# readers under two seeds (20,000 per fixture, seven fixtures, two seeds), and
# the planted controls. The fuzz is measured in minutes and answers a
# question about the runtime under hostile input rather than about the diff,
# which is what makes it a certification instrument and not an iteration one.
.PHONY: tables-dart-release
tables-dart-release:
	$(MAKE) tables-dart-fuzz SEED=1 DART_FUZZ_MUTANTS=20000
	$(MAKE) tables-dart-fuzz SEED=2 DART_FUZZ_MUTANTS=20000
	$(MAKE) tables-dart-fuzz-negative-control
	$(MAKE) tables-dart-wire-fuzz N=500000
	$(MAKE) tables-dart-wire-fuzz SEED=2 N=500000
	$(MAKE) tables-dart-wire-fuzz-negative-control
	$(MAKE) tables-dart-alloc
	$(MAKE) tables-dart-alloc-negative-control
	$(MAKE) tables-dart-alloc-runtime-negative-control
	$(MAKE) tables-dart-order
	$(MAKE) tables-dart-order-negative-control
	$(MAKE) tables-dart-fuzz-lead-negative-control
	$(MAKE) tables-dart-fixed-form-negative-control
	$(MAKE) tables-dart-names-negative-control
	$(MAKE) tables-dart-standalone-negative-control

# THE FORGERY FUZZER over the Dart accelerators: valid images from the corpus,
# mutated, and one oracle over every mutant — refuse, or open and be WHOLE, and
# NOTHING THROWS. That last clause is Dart's own: an out-of-bounds index here is
# a RangeError, and a reader that raises on hostile bytes is not one that
# refuses them.
.PHONY: tables-dart-fuzz
tables-dart-fuzz: build/tables-generated-dart/.stamp build/cook-fuzz/.stamp
	SEED=$(SEED) $(DART) test/dart-tables/fuzz.dart $(DART_FUZZ_MUTANTS)

DART_FUZZ_MUTANTS ?= 4000

# THE FUZZER'S NEGATIVE CONTROL, on the same rule as the block form's C++ one: a
# fuzzer that has never gone red proves nothing about the reader it points at.
# ONE CHECK is removed from the block Open — the count against the declared
# maximum — in a COPY of the emitter, the corpus is regenerated from the
# sabotaged compiler, and the oracle must find it. BOTH halves of the extent
# bound are removed — the rows-inside-extent test AND the padding test — so
# nothing downstream catches what the first lets through (I3: removing the
# rows bound alone leaves the padding check to refuse on the negative slack, so
# only both keep the verdict seed-independent). The count against the declared
# maximum stays in place, so the named finding is the oracle's own walk. Red is
# required on that named finding at both seeds. No tracked file is written to,
# so an interrupt cannot leave a sabotaged working tree.
.PHONY: tables-dart-fuzz-negative-control
tables-dart-fuzz-negative-control: build/cook-fuzz/.stamp
	@rm -rf build/dart-fuzz-nc && mkdir -p build/dart-fuzz-nc
	@sed -e 's|g.pf("      if (rows > extent - offsetOf) {\n        return (null, TableRefuseReason.badLayout);\n      }\n")|_ = field // SABOTAGED: the rows-inside-extent bound is gone|' \
	     -e 's|g.pf("    if (padding > extent - used) {\n      return (null, TableRefuseReason.truncated);\n    }\n")|// SABOTAGED: the padding bound is gone|' \
		internal/codegen/darttable/block.go > build/dart-fuzz-nc/block.go.txt
	@cmp -s internal/codegen/darttable/block.go build/dart-fuzz-nc/block.go.txt && \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage matched nothing — the emitter moved"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/codegen/darttable/block.go":"%s/build/dart-fuzz-nc/block.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/dart-fuzz-nc/overlay.json
	go build -overlay build/dart-fuzz-nc/overlay.json -o build/dart-fuzz-nc/schema ./cmd/schema
	./build/dart-fuzz-nc/schema generate --lang dart --out build/dart-fuzz-nc/generated/block tables/block
	./build/dart-fuzz-nc/schema generate --lang dart --out build/dart-fuzz-nc/generated/pointers tables/pointers
	@sed -e "s|../../build/tables-generated-dart/block/|$(CURDIR)/build/dart-fuzz-nc/generated/block/|g" \
	     -e "s|../../build/tables-generated-dart/pointers/|$(CURDIR)/build/dart-fuzz-nc/generated/pointers/|g" \
		test/dart-tables/fuzz.dart > build/dart-fuzz-nc/fuzz.dart
	@for s in 1 2; do \
		if SEED=$$s $(DART) build/dart-fuzz-nc/fuzz.dart 4000 > build/dart-fuzz-nc/log-$$s 2>&1; then \
			echo "NEGATIVE CONTROL FAILED: the fuzzer stayed green at seed $$s with both extent bounds removed"; \
			tail -5 build/dart-fuzz-nc/log-$$s; exit 1; \
		fi; \
		grep -q "past the claimed" build/dart-fuzz-nc/log-$$s || \
			{ echo "NEGATIVE CONTROL FAILED: the fuzzer at seed $$s went red for some other reason"; \
			  tail -20 build/dart-fuzz-nc/log-$$s; exit 1; }; \
	done
	@grep -m1 "past the claimed" build/dart-fuzz-nc/log-1
	@echo "negative control: both halves of the extent bound removed from the Dart block Open turn the fuzzer RED at both seeds"

# THE LEAD GATE'S NEGATIVE CONTROL: the base-alignment check is removed from the
# block Open (openWithReason, which open delegates to) in a copy, and the
# enumerated lead pass must go red — every middle
# lead 1..63 opens when the check that should refuse them is gone (I5).
.PHONY: tables-dart-fuzz-lead-negative-control
tables-dart-fuzz-lead-negative-control: build/cook-fuzz/.stamp
	@rm -rf build/dart-fuzz-lead-nc && mkdir -p build/dart-fuzz-lead-nc
	@sed -e 's#g.pf("    if (base %% %d != 0) {\n      return (null, TableRefuseReason.unalignedBase);\n    }\n", ir.BlockAlign)#// SABOTAGED: the base-alignment check is gone#' \
		internal/codegen/darttable/block.go > build/dart-fuzz-lead-nc/block.go.txt
	@cmp -s internal/codegen/darttable/block.go build/dart-fuzz-lead-nc/block.go.txt && \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage matched nothing — the emitter moved"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/codegen/darttable/block.go":"%s/build/dart-fuzz-lead-nc/block.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/dart-fuzz-lead-nc/overlay.json
	go build -overlay build/dart-fuzz-lead-nc/overlay.json -o build/dart-fuzz-lead-nc/schema ./cmd/schema
	./build/dart-fuzz-lead-nc/schema generate --lang dart --out build/dart-fuzz-lead-nc/generated/block tables/block
	./build/dart-fuzz-lead-nc/schema generate --lang dart --out build/dart-fuzz-lead-nc/generated/pointers tables/pointers
	@sed -e "s|../../build/tables-generated-dart/block/|$(CURDIR)/build/dart-fuzz-lead-nc/generated/block/|g" \
	     -e "s|../../build/tables-generated-dart/pointers/|$(CURDIR)/build/dart-fuzz-lead-nc/generated/pointers/|g" \
		test/dart-tables/fuzz.dart > build/dart-fuzz-lead-nc/fuzz.dart
	@for s in 1 2; do \
		if SEED=$$s $(DART) build/dart-fuzz-lead-nc/fuzz.dart 4000 > build/dart-fuzz-lead-nc/log-$$s 2>&1; then \
			echo "NEGATIVE CONTROL FAILED: the lead pass stayed green at seed $$s with the alignment check removed"; \
			tail -5 build/dart-fuzz-lead-nc/log-$$s; exit 1; \
		fi; \
		grep -q "answered open, wanted refuse" build/dart-fuzz-lead-nc/log-$$s || \
			{ echo "NEGATIVE CONTROL FAILED: the lead pass at seed $$s went red for some other reason"; \
			  tail -20 build/dart-fuzz-lead-nc/log-$$s; exit 1; }; \
	done
	@grep -m1 "answered open, wanted refuse" build/dart-fuzz-lead-nc/log-1
	@echo "negative control: the base-alignment check removed from the Dart block Open turns the lead pass RED at both seeds"

# THE CROSS-ENDIAN ORDER-WORD GATE (docs/SPEC-TABLES.md §19.1, §7.1; issue
# #415; docs/PORTING.md I7). The harness's two foreign surfaces (cook-foreign,
# block-foreign) reverse the MAGIC word, which every leg answers on any host;
# nothing forges the half a reader leaning on one check would open — a file
# whose magic is intact and whose order word says the other order. This gate
# holds that half: each corpus image opens unmodified, a copy with ONLY the
# order word (offset 16, 8 bytes) set to the other order's value (2) refuses,
# and no exception escapes either call. Dart's typed data is read at an
# explicit endianness everywhere, so the order word is the whole of the
# byte-order check.
.PHONY: tables-dart-order
tables-dart-order: build/tables-generated-dart/.stamp build/cook-fuzz/.stamp
	$(DART) test/dart-tables/order.dart

# ITS NEGATIVE CONTROL: remove the order check from a COPY of the emitter — one
# for block.go and one for cook.go in the same target — regenerate the reader
# from the sabotaged compiler, and require the gate to go red on the named row
# while the other stays green. No tracked file is written to.
.PHONY: tables-dart-order-negative-control
tables-dart-order-negative-control: build/tables-generated-dart/.stamp build/cook-fuzz/.stamp
	# the BLOCK row: the order check gone from block.go must turn only it red
	@rm -rf build/dart-order-nc && mkdir -p build/dart-order-nc
	@sed '/tableBlockRead64(view, base + 16)/d' \
		internal/codegen/darttable/block.go > build/dart-order-nc/block.go.txt
	@grep -q 'tableBlockRead64(view, base + 16)' internal/codegen/darttable/block.go || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage matched nothing — the emitter moved"; exit 1; }
	@if grep -q 'tableBlockRead64(view, base + 16)' build/dart-order-nc/block.go.txt; then \
		echo "NEGATIVE CONTROL FAILED: the order check survived the sabotage"; exit 1; fi
	@printf '{"Replace":{"%s/internal/codegen/darttable/block.go":"%s/build/dart-order-nc/block.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/dart-order-nc/overlay-block.json
	go build -overlay build/dart-order-nc/overlay-block.json -o build/dart-order-nc/schema-block ./cmd/schema
	./build/dart-order-nc/schema-block generate --lang dart --out build/dart-order-nc/generated/block tables/block
	@sed -e "s|../../build/tables-generated-dart/block/|$(CURDIR)/build/dart-order-nc/generated/block/|g" \
		test/dart-tables/order.dart > build/dart-order-nc/order-block.dart
	@if $(DART) build/dart-order-nc/order-block.dart > build/dart-order-nc/log-block 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the block row stayed green with the order check removed"; \
		tail -20 build/dart-order-nc/log-block; exit 1; \
	fi
	@grep -q "ORDER FAIL block" build/dart-order-nc/log-block || \
		{ echo "NEGATIVE CONTROL FAILED: the block row went red for another reason"; \
		  tail -20 build/dart-order-nc/log-block; exit 1; }
	@if grep -q "ORDER FAIL cook" build/dart-order-nc/log-block; then \
		echo "NEGATIVE CONTROL FAILED: the cook row went red too"; \
		tail -20 build/dart-order-nc/log-block; exit 1; fi
	@grep -m1 "ORDER FAIL block" build/dart-order-nc/log-block
	# the COOK row: the order check gone from cook.go must turn only it red
	@sed '/tableCookRead64(view, at + 16)/d' \
		internal/codegen/darttable/cook.go > build/dart-order-nc/cook.go.txt
	@grep -q 'tableCookRead64(view, at + 16)' internal/codegen/darttable/cook.go || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage matched nothing — the emitter moved"; exit 1; }
	@if grep -q 'tableCookRead64(view, at + 16)' build/dart-order-nc/cook.go.txt; then \
		echo "NEGATIVE CONTROL FAILED: the order check survived the sabotage"; exit 1; fi
	@printf '{"Replace":{"%s/internal/codegen/darttable/cook.go":"%s/build/dart-order-nc/cook.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/dart-order-nc/overlay-cook.json
	go build -overlay build/dart-order-nc/overlay-cook.json -o build/dart-order-nc/schema-cook ./cmd/schema
	./build/dart-order-nc/schema-cook generate --lang dart --out build/dart-order-nc/generated/pointers tables/pointers
	@sed -e "s|../../build/tables-generated-dart/pointers/|$(CURDIR)/build/dart-order-nc/generated/pointers/|g" \
		test/dart-tables/order.dart > build/dart-order-nc/order-cook.dart
	@if $(DART) build/dart-order-nc/order-cook.dart > build/dart-order-nc/log-cook 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the cook row stayed green with the order check removed"; \
		tail -20 build/dart-order-nc/log-cook; exit 1; \
	fi
	@grep -q "ORDER FAIL cook" build/dart-order-nc/log-cook || \
		{ echo "NEGATIVE CONTROL FAILED: the cook row went red for another reason"; \
		  tail -20 build/dart-order-nc/log-cook; exit 1; }
	@if grep -q "ORDER FAIL block" build/dart-order-nc/log-cook; then \
		echo "NEGATIVE CONTROL FAILED: the block row went red too"; \
		tail -20 build/dart-order-nc/log-cook; exit 1; fi
	@grep -m1 "ORDER FAIL cook" build/dart-order-nc/log-cook
	@echo "negative control: the order check removed turns its own row red and leaves the other green"

# The DART leg's driver: one AOT executable, because `dart run` would pay a JIT
# start-up per surface and the two-minute rule is measured across every leg.
# The cards wire library is generated beside it so the wire-fuzz mode below can
# bind the Card codec.
build/conformance-dart: build/tables-generated-dart/.stamp cards-dart-generate test/conformance/dart/main.dart
	@mkdir -p build
	$(DART) compile exe -o $@ test/conformance/dart/main.dart >/dev/null

# THE FORM-1 COOK WRITER'S NEGATIVE CONTROL (docs/SPEC-TABLES.md §2.6, §3, §7.6):
# the cook-write rows of messagedemo (an ARRAY OF UNIONS at three depths) and
# scalars (the WIDE KINDS as array elements) are carried by two pieces of
# cookwrite_form1.go / cookwrite.go. Each leg removes one from a COPY of the
# emitter, regenerates that unit beside the tree, compiles a copy of the driver
# against it, and requires the harness to go red on that unit's cook-write
# rows and on nothing else. No tracked file is written to.
DART_COOKWRITE_NC := build/dart-cookwrite-nc
# $(1) leg, $(2) emitter file, $(3) sed sabotage, $(4) unit dir in the tree,
# $(5) schema source, $(6) the failing rows' name prefix
define dart-cookwrite-nc-leg
	@rm -rf $(DART_COOKWRITE_NC)/$(1) && mkdir -p $(DART_COOKWRITE_NC)/$(1)
	@sed $(3) internal/codegen/darttable/$(2) > $(DART_COOKWRITE_NC)/$(1)/$(2).txt
	@grep -q SABOTAGED $(DART_COOKWRITE_NC)/$(1)/$(2).txt || \
		{ echo "NEGATIVE CONTROL FAILED: the $(1) sabotage matched nothing - the emitter moved"; exit 1; }
	@printf '{"Replace":{"%s/internal/codegen/darttable/$(2)":"%s/$(DART_COOKWRITE_NC)/$(1)/$(2).txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > $(DART_COOKWRITE_NC)/$(1)/overlay.json
	go build -overlay $(DART_COOKWRITE_NC)/$(1)/overlay.json -o $(DART_COOKWRITE_NC)/$(1)/schema ./cmd/schema
	$(DART_COOKWRITE_NC)/$(1)/schema generate --lang dart --out $(DART_COOKWRITE_NC)/$(1)/generated $(5)
	@sed -e "s|../../../build/$(4)/|$(CURDIR)/$(DART_COOKWRITE_NC)/$(1)/generated/|g" \
	     -e "s|../../../build/|$(CURDIR)/build/|g" \
		test/conformance/dart/main.dart > $(DART_COOKWRITE_NC)/$(1)/main.dart
	$(DART) compile exe -o $(DART_COOKWRITE_NC)/$(1)/driver-bin $(DART_COOKWRITE_NC)/$(1)/main.dart >/dev/null
	@printf 'dart %s/$(DART_COOKWRITE_NC)/$(1)/driver-bin\n' "$(CURDIR)" > $(DART_COOKWRITE_NC)/$(1)/drivers.txt
	@if ./build/conformance-harness run --drivers $(DART_COOKWRITE_NC)/$(1)/drivers.txt \
			--work $(DART_COOKWRITE_NC)/$(1)/work > $(DART_COOKWRITE_NC)/$(1)/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the $(1) sabotage left the harness green"; \
		tail -30 $(DART_COOKWRITE_NC)/$(1)/log; exit 1; \
	fi
	@grep -q "dart / cook-write: $(6)" $(DART_COOKWRITE_NC)/$(1)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the harness went red, but not on a $(6) cook-write row"; \
		  tail -30 $(DART_COOKWRITE_NC)/$(1)/log; exit 1; }
	@if awk '/^FAILURES/ { f = 1; next } f && /^  /' $(DART_COOKWRITE_NC)/$(1)/log | grep -v "dart / cook-write: $(6)"; then \
		echo "NEGATIVE CONTROL FAILED: the $(1) sabotage turned another row red too"; exit 1; \
	fi
	@grep -m1 "dart / cook-write: $(6)" $(DART_COOKWRITE_NC)/$(1)/log
endef

.PHONY: tables-dart-cookwrite-negative-control
tables-dart-cookwrite-negative-control: build/conformance-dart build/conformance-harness
	$(call dart-cookwrite-nc-leg,unions,cookwrite_form1.go,'s|g.emitCookForm1Union(elem, un, 0, "value", "    ")|// SABOTAGED: no union element is written|',tables-generated-dart/messages,tables/messages,message_)
	$(call dart-cookwrite-nc-leg,wide,cookwrite.go,'s|g.emitCookVariableWide(f, expr, addr, addr+" + 8", stride, ind)|_ = addr // SABOTAGED: no wide element is written|',tables-generated-dart-scalars/scalars,tables/scalars,scalars_)
	@echo "negative control: the form-1 cook writer's union elements and wide elements each turn exactly their own cook-write rows RED"

test-dart: tables-dart-cookwrite-negative-control

# THE RETAIN SURFACES' NEGATIVE CONTROL (docs/SPEC-TABLES.md §6.6): a copy of
# the emitter whose retaining save re-emits NO retained record, built through
# `go build -overlay` (under build/, never committed), regenerates RT1 into a
# copy of the generated corpus, and the driver compiled against it must turn
# `retain-save` red — the retained fields no longer come back to their bodies,
# and `retain` with it where the id list's loss moves — while every other
# surface stays green, so the red is the retention and nothing else. The run is one substituted leg (--drivers), as the harness
# README says a substituted registry is.
.PHONY: tables-dart-retain-negative-control
tables-dart-retain-negative-control: build/conformance-harness build/conformance-dart
	@rm -rf build/dart-retain-nc && mkdir -p build/dart-retain-nc
	@sed -e 's|    if (!identical(rec.owner, owner)) continue;|    continue; // SABOTAGED: no retained record is re-emitted|' \
		internal/codegen/darttable/retainwire.go > build/dart-retain-nc/retainwire.go.txt
	@cmp -s internal/codegen/darttable/retainwire.go build/dart-retain-nc/retainwire.go.txt && \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage matched nothing — the emitter moved"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/codegen/darttable/retainwire.go":"%s/build/dart-retain-nc/retainwire.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/dart-retain-nc/overlay.json
	go build -overlay build/dart-retain-nc/overlay.json -o build/dart-retain-nc/schema ./cmd/schema
	@cp -R build/tables-generated-dart build/dart-retain-nc/generated
	@rm -rf build/dart-retain-nc/generated/rt1
	./build/dart-retain-nc/schema generate --lang dart --out build/dart-retain-nc/generated/rt1 test/tables/RT1.schema
	@sed -e "s|'../../../build/tables-generated-dart/|'$(CURDIR)/build/dart-retain-nc/generated/|" \
	     -e "s|'../../../build/|'$(CURDIR)/build/|" \
		test/conformance/dart/main.dart > build/dart-retain-nc/main.dart
	$(DART) compile exe -o build/dart-retain-nc/conformance-dart build/dart-retain-nc/main.dart >/dev/null
	@echo "dart build/dart-retain-nc/conformance-dart" > build/dart-retain-nc/drivers.txt
	@if ./build/conformance-harness run --drivers build/dart-retain-nc/drivers.txt \
		--work build/dart-retain-nc/work > build/dart-retain-nc/log 2>&1; then \
		cat build/dart-retain-nc/log; \
		echo "NEGATIVE CONTROL FAILED: the Dart leg stayed green with no retained record re-emitted"; exit 1; \
	fi
	@grep -qE '^retain-save +FAIL' build/dart-retain-nc/log || \
		{ cat build/dart-retain-nc/log; echo "NEGATIVE CONTROL FAILED: retain-save did not go red"; exit 1; }
	@if grep -E '^[a-z-]+ +FAIL' build/dart-retain-nc/log | grep -vqE '^retain(-save)? '; then \
		cat build/dart-retain-nc/log; echo "NEGATIVE CONTROL FAILED: a surface other than the two retain surfaces went red"; exit 1; \
	fi
	@grep -E '^retain(-save)? ' build/dart-retain-nc/log
	@echo "negative control: a Dart retaining save that re-emits no retained record turns the retain surfaces RED and only them"

# ---------------------------------------------------------------------------
# THE FIXED FORM, form byte 3 (docs/SPEC-TABLES.md §3.4)
# ---------------------------------------------------------------------------
#
# THE BYTES ARE THE C++ REFERENCE'S, and that is the whole point of this gate.
# The reference writes eight form-3 files — the seven of
# `make tables-fixedform-corpus` and the paired bench's sixty-four logical
# records — and states the VALUES beside them, by hand in
# test/tables/fixedform_dump.cpp and as a JSON oracle beside the bench corpus.
# The Dart leg reads each file, checks every field against those values, writes
# it back, and the bytes must be IDENTICAL. A reader and a writer that share
# one offset mistake round trip perfectly and are both wrong, which is why the
# values ride beside the bytes and not instead of them.
#
# Beside it rides the VERSIONING CONFORMANCE — the FX1/FX2, V1/V2 and P1/P3
# pairs the C++ leg uses (test/tables/fixedform_main.cpp), case for case — and
# the negative controls, of which the one §3.4 names is a reader given the
# WRONG PLAN for a record, which must come out wrong, and one corrupted-layout
# case per NAMED RULE a reader holds an untrusted peer's layout to.
#
# FU1/FU2 is the TEXT-UNDER-AN-ARM pair whose compiled path is a TRAILING
# FIELD, not a slid ordinal: FU1's second arm carries a string(8), FU2 appends
# `extra` so a read of FU1's bytes is a compiled plan, and the two reads have
# to agree on the text (reference-fix 12). C++ already generates the pair;
# this stamp did not.
build/dart-fixed/.stamp: bin/schema bench/corpus/Bench.schema bench/corpus/FixedTable.schema \
		test/tables/FX1.schema test/tables/FX2.schema \
		test/tables/V1.schema test/tables/V2.schema \
		test/tables/P1.schema test/tables/P3.schema test/tables/FXW.schema \
		test/tables/FU1.schema test/tables/FU2.schema \
		$(SCHEMAS_TABLES) make/dart.mk
	@rm -rf build/dart-fixed && mkdir -p build/dart-fixed
	./bin/schema generate --lang dart --out build/dart-fixed/bench bench/corpus/Bench.schema bench/corpus/FixedTable.schema
	./bin/schema generate --lang dart --out build/dart-fixed/fx1 test/tables/FX1.schema
	./bin/schema generate --lang dart --out build/dart-fixed/fx2 test/tables/FX2.schema
	./bin/schema generate --lang dart --out build/dart-fixed/v1 test/tables/V1.schema
	./bin/schema generate --lang dart --out build/dart-fixed/v2 test/tables/V2.schema
	./bin/schema generate --lang dart --out build/dart-fixed/p1 test/tables/P1.schema
	./bin/schema generate --lang dart --out build/dart-fixed/p3 test/tables/P3.schema
	./bin/schema generate --lang dart --out build/dart-fixed/fu1 test/tables/FU1.schema
	./bin/schema generate --lang dart --out build/dart-fixed/fu2 test/tables/FU2.schema
	./bin/schema generate --lang dart --out build/dart-fixed/examples tables/examples
	# THE WIDE TEXT UNIT (docs/SPEC-TABLES.md §3.4, kind 33): its own file and
	# its own directory, because every SHARED schema list is pinned to targets
	# that refuse kind 33 in a table closure. Dart carries it on FORM 3 and on
	# no other form — the block and the cook refuse it by name — so this is the
	# only gate it can ride in, and without it the wide flavour of the text op
	# has no oracle bytes anywhere.
	./bin/schema generate --lang dart --out build/dart-fixed/fxw test/tables/FXW.schema
	@$(DART) format build/dart-fixed >/dev/null
	@touch $@

.PHONY: tables-dart-fixed-form
tables-dart-fixed-form: build/dart-fixed/.stamp build/fixedform-corpus/.stamp build/fixedform-bench-corpus/.stamp
	$(DART) analyze --no-fatal-warnings build/dart-fixed test/dart-tables/fixedform.dart
	$(DART) format --set-exit-if-changed --output=none test/dart-tables/fixedform.dart
	@for f in build/dart-fixed/*/*Fixed.dart; do \
		$(DART) format --set-exit-if-changed --output=none $$f >/dev/null || \
			{ echo "dart format drift in $$f"; exit 1; }; \
	done
	@echo "tables Dart fixed form: every generated library is format-canonical"
	$(DART) --enable-asserts test/dart-tables/fixedform.dart build/fixedform-corpus build/fixedform-bench-corpus
	@mkdir -p build
	$(DART) compile exe -o build/dart_fixedform test/dart-tables/fixedform.dart >/dev/null
	./build/dart_fixedform build/fixedform-corpus build/fixedform-bench-corpus

# ITS NEGATIVE CONTROL: move ONE byte of the write template and the leg must go
# red against the reference's corpus. Without this the byte comparison could be
# comparing a file with itself and nobody would know.
# THE VERSIONING GATE, THIS LEG'S SECOND HALF (docs/FIXED-FORM-ALGORITHM.md
# §5.7 step 6, §5.9 #11): the BYTE gate above proves this leg writes the
# reference's bytes; this one proves it READS BACKWARD — every row of
# docs/FIXED-FORM-VERSIONING-TESTS.md in both columns, NEW-READS-OLD and
# OLD-REFUSES-NEW, plus the three floor rows, the hash rows and the branch
# merge.
#
# THE PROBES ARE GENERATED, one per row per COLUMN, and run with THIS leg's own
# toolchain (§5.9 #18) — the harness is internal/codegen/darttable's
# fixedversioning_test.go, which plays the lock, generates the reader's Dart
# with the older generation's locked entry as its lineage, writes a probe beside
# it and runs `$(DART)` over it.
#
# THIS TARGET EXISTS BECAUSE `go test ./...` ASSERTS NOTHING HERE: the harness
# SKIPS itself when build/fixedform-corpus or the SDK is absent, which is right
# for a bare `go test ./...` and is exactly how a §5 regression would ride into
# a green. So the target builds the oracle first and sets
# SCHEMA_REQUIRE_CORPUS=1, under which that skip is a FAILURE.
.PHONY: tables-dart-versioning
tables-dart-versioning: tables-fixedform-corpus
	SCHEMA_REQUIRE_CORPUS=1 DART=$(DART) go test ./internal/codegen/darttable/ -count=1 -run 'TestFixedVersioning|TestFixedCompiledPlanTagPastArmSet'
	@echo 'tables Dart versioning: §5 read both columns of every row against the C++ reference bytes'

.PHONY: tables-dart-fixed-form-negative-control
tables-dart-fixed-form-negative-control: bin/schema build/fixedform-corpus/.stamp build/fixedform-bench-corpus/.stamp test/tables/FXW.schema
	@rm -rf build/dart-fixed-nc && mkdir -p build/dart-fixed-nc
	@sed 's|fixedOff(base, at), length, "Endian.little"}, ";")|fixedOff(base, at), length + " + 1", "Endian.little"}, ";") // SABOTAGED|' \
		internal/codegen/darttable/fixeddart.go > build/dart-fixed-nc/fixeddart.go.txt
	@grep -q SABOTAGED build/dart-fixed-nc/fixeddart.go.txt || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage matched nothing — the emitter moved"; exit 1; }
	@printf '{"Replace":{"%s/internal/codegen/darttable/fixeddart.go":"%s/build/dart-fixed-nc/fixeddart.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/dart-fixed-nc/overlay.json
	go build -overlay build/dart-fixed-nc/overlay.json -o build/dart-fixed-nc/schema ./cmd/schema
	./build/dart-fixed-nc/schema generate --lang dart --out build/dart-fixed-nc/gen/bench bench/corpus/Bench.schema bench/corpus/FixedTable.schema
	./build/dart-fixed-nc/schema generate --lang dart --out build/dart-fixed-nc/gen/fx1 test/tables/FX1.schema
	./build/dart-fixed-nc/schema generate --lang dart --out build/dart-fixed-nc/gen/fx2 test/tables/FX2.schema
	./build/dart-fixed-nc/schema generate --lang dart --out build/dart-fixed-nc/gen/v1 test/tables/V1.schema
	./build/dart-fixed-nc/schema generate --lang dart --out build/dart-fixed-nc/gen/v2 test/tables/V2.schema
	./build/dart-fixed-nc/schema generate --lang dart --out build/dart-fixed-nc/gen/p1 test/tables/P1.schema
	./build/dart-fixed-nc/schema generate --lang dart --out build/dart-fixed-nc/gen/p3 test/tables/P3.schema
	./build/dart-fixed-nc/schema generate --lang dart --out build/dart-fixed-nc/gen/fu1 test/tables/FU1.schema
	./build/dart-fixed-nc/schema generate --lang dart --out build/dart-fixed-nc/gen/fu2 test/tables/FU2.schema
	./build/dart-fixed-nc/schema generate --lang dart --out build/dart-fixed-nc/gen/examples tables/examples
	./build/dart-fixed-nc/schema generate --lang dart --out build/dart-fixed-nc/gen/fxw test/tables/FXW.schema
	@sed 's|../../build/dart-fixed/|$(CURDIR)/build/dart-fixed-nc/gen/|g' \
		test/dart-tables/fixedform.dart > build/dart-fixed-nc/fixedform.dart
	@if $(DART) build/dart-fixed-nc/fixedform.dart build/fixedform-corpus build/fixedform-bench-corpus \
			> build/dart-fixed-nc/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a text length one byte off left the fixed form green"; \
		cat build/dart-fixed-nc/log; exit 1; \
	fi
	@grep -q "first byte differing from the C++ reference" build/dart-fixed-nc/log || \
		{ echo "NEGATIVE CONTROL FAILED: the fixed form went red for another reason"; \
		  cat build/dart-fixed-nc/log; exit 1; }
	@grep -m1 "first byte differing from the C++ reference" build/dart-fixed-nc/log
	@echo "negative control: one byte off the Dart write template reds the reference byte match"

# ITS NEGATIVE CONTROL (§5.9 #11: a gate nobody has watched fail may be
# comparing a file with itself). The sabotage replaces the ONE name that carries
# §5's whole direction — a hash no lineage entry holds is `layout_newer`, ship
# the reader — with `layout_malformed`, and the OLD-REFUSES-NEW column must go
# red naming it. `go test -overlay` is the same trick the other controls use on
# `go build`: the emitter is replaced for one run and the tree is never touched.
.PHONY: tables-dart-versioning-negative-control
tables-dart-versioning-negative-control: tables-fixedform-corpus
	@rm -rf build/dart-versioning-nc && mkdir -p build/dart-versioning-nc
	@sed 's|report.refused = TableFixedRefusal.layoutNewer;|report.refused = TableFixedRefusal.layoutMalformed; // SABOTAGED|' \
		internal/codegen/darttable/fixedmodule.go > build/dart-versioning-nc/fixedmodule.go.txt
	@grep -q SABOTAGED build/dart-versioning-nc/fixedmodule.go.txt || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage matched nothing — the emitter moved"; exit 1; }
	@printf '{"Replace":{"%s/internal/codegen/darttable/fixedmodule.go":"%s/build/dart-versioning-nc/fixedmodule.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/dart-versioning-nc/overlay.json
	@if SCHEMA_REQUIRE_CORPUS=1 DART=$(DART) go test -overlay build/dart-versioning-nc/overlay.json \
			./internal/codegen/darttable/ -count=1 -run 'TestFixedVersioningOldRefusesNew' \
			> build/dart-versioning-nc/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the wrong refusal name left the versioning gate green"; \
		cat build/dart-versioning-nc/log; exit 1; \
	fi
	@grep -q 'owes layout_newer' build/dart-versioning-nc/log || \
		{ echo "NEGATIVE CONTROL FAILED: the versioning gate went red for another reason"; \
		  cat build/dart-versioning-nc/log; exit 1; }
	@echo 'negative control: the wrong name for a hash outside the lineage reds the Dart versioning gate'

# THE KEYED-ARRAY EXTENT GATE'S NEGATIVE CONTROL (issue 407): Dart indexes keyed
# arrays by SLOT — a keyed array holds exactly one slot per named variant, key k
# rides at slot k-1, and key None and key Max+1 are refused by the typed list's
# own RangeError (docs/PORTING.md M5). The extent is the whole contract, so
# TestDartKeyedArraysAreHeldAtBothEnds holds it. One extra slot planted in a COPY
# of the emitter (the array extent Max becomes Max+1) must turn that test red
# naming the extent. No tracked file is written to.
.PHONY: tables-dart-keyed-negative-control
tables-dart-keyed-negative-control:
	@rm -rf build/dart-keyed-nc && mkdir -p build/dart-keyed-nc
	@sed 's|g.emitListStorage(f, ind, name, f.KeyEnumRef.Max)|g.emitListStorage(f, ind, name, f.KeyEnumRef.Max + 1) // SABOTAGED|' \
		internal/codegen/darttable/fixeddart.go > build/dart-keyed-nc/fixeddart.go.txt
	@grep -q SABOTAGED build/dart-keyed-nc/fixeddart.go.txt || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage matched nothing — the emitter moved"; exit 1; }
	@printf '{"Replace":{"%s/internal/codegen/darttable/fixeddart.go":"%s/build/dart-keyed-nc/fixeddart.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/dart-keyed-nc/overlay.json
	@if SCHEMA_REQUIRE_CORPUS=1 DART=$(DART) go test -count=1 -overlay build/dart-keyed-nc/overlay.json -run TestDartKeyedArraysAreHeldAtBothEnds \
			./internal/codegen/dart/ > build/dart-keyed-nc/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: an extra keyed slot left the extent test green"; \
		cat build/dart-keyed-nc/log; exit 1; \
	fi
	@grep -q "must hold exactly the enum extent" build/dart-keyed-nc/log || \
		{ echo "NEGATIVE CONTROL FAILED: the extent test went red for some other reason"; \
		  cat build/dart-keyed-nc/log; exit 1; }
	@grep -m1 "enum extent" build/dart-keyed-nc/log
	@echo "negative control: one extra keyed slot turns the Dart keyed-array extent test RED"

# reached from make test like every other Dart control (TestPortingRegisterHoldsTheTree)
test-dart: tables-dart-keyed-negative-control

# THE VARIABLE CLASS (docs/SPEC-TABLES.md §2.5, §2.9, §3, §3.1, §16.7): pointers,
# by-value nesting, []*T, blobs, the remaining scalar kinds and unbounded
# scalar/enum arrays, byte for byte against the oracle. A bare go test skips
# when the SDK is absent; SCHEMA_REQUIRE_CORPUS=1 makes that skip a failure.
.PHONY: tables-dart-variable-class
tables-dart-variable-class:
	SCHEMA_REQUIRE_CORPUS=1 DART=$(DART) go test ./internal/codegen/darttable/ -count=1 -timeout 600s -run 'TestVariableClassBeyondCardConstructsDart|TestOtherVariableConstructsAreRefusedByName'
	@echo 'tables Dart variable class: pointers, blobs, lists and the scalar kinds against the oracle bytes'

test-dart: tables-dart-variable-class

# THE JIT FLOAT INSTRUMENT (issue 404, PORTING M3): a double crossing an
# out-of-line conversion call boxes under the JIT, and whether the call is
# inlined is the inliner's budget, not the codec's. test/dart/floatjit.dart
# drives float32 (Input) and float64 (Vec3) fields through the generated
# packet codec in three shapes — as generated, hand-inlined, and from two
# callers — and the VM prints one line per scavenge under --verbose_gc. The
# program brackets its steady loop with two marker lines on stderr, after a
# warm-up of many short calls, and only the main isolate's scavenges between
# the markers are counted; each shape runs at N and at 2N steady passes, and a
# steady state that allocates shows in both, in proportion. Measured on the
# pinned SDK (3.13.2, linux x64) against the codec as it was before the
# emitters inlined the float32 conversion: generated 76 (152 at 2N), inlined
# 0, twocallers 76 (153 at 2N) per 2,000,000 passes; after: 0, 0, 0. Any
# scavenge in either window is red. A functional test (the VM).
#
# A count is only read off a run that is proven whole. Each measured run's
# stdout and stderr go to their own files under build/floatjit/, and the run is
# REFUSED, by name, when the Dart process exits nonzero, when its stdout lacks
# the completion line floatjit.dart prints last, when its stderr lacks the
# main isolate's --verbose_gc header (`main: ... reason=initial`), which is how
# an absent or unsupported GC trace is told from a valid trace holding zero
# scavenges, or when either window marker is missing. No failure is
# swallowed. THE BLADE (tables-dart-float-jit-negative-control) runs first,
# every time: a planted run (floatjit.dart's `planted-exit` shape, which warms
# up and exits 3 with no completion line) and a run with the GC trace switched
# off must both be refused by the same check, each for its own reason, or the
# target is red with BLADE LOST.
#
# A count is only certified on the pinned SDK. The JIT inlining budget is the
# SDK's, so a zero measured on another SDK proves nothing about 3.13.2. Before
# any measured run the recipe reads `$(DART) --version` and REFUSES, by name,
# unless it exits zero and begins `Dart SDK version: $(DART_SDK_PIN) ` (the
# trailing space keeps 3.13.20 out). THE OFF-PIN CONTROL
# (tables-dart-float-jit-sdk-negative-control) runs this very recipe against
# three stand-in SDKs (3.12.0, 3.13.20, and one whose --version fails) and is
# red unless each is refused before anything is measured.
#
# A zero is only read off a window in which the JIT did not tier. MEASURED
# CAUSE of the stray inlined-window scavenge (2026-10-16, hetzner2, 3.13.2):
# with the VM's background compiler, the optimizing compile of the pass is
# queued during warm-up and can be installed after the steady call has
# already entered the unoptimized pass, which boxes every double until
# on-stack replacement swaps it in. Under --trace_compiler --trace_osr
# --trace_deoptimization every traced stray (4 of 4, of 300 runs) showed
# `--> 'passInlined' entry:` (compiles of 26 to 48 ms on the loaded bench)
# and then `Attempting OSR for passInlined` INSIDE the window, each
# scavenge collecting a full 1.5-2.0 MB new space; the pass run unoptimized
# throughout reads 43 scavenges per 200,000 passes (86 per 400,000). The
# inlined control is hit most because its callees deopt during warm-up
# (`Deoptimizing (reason 0 'BinarySmiOp')`, a 64-bit word leaving Smi
# range, as the generated writeInput and readVec3 also do), which pushes the
# pass's final optimized compile late into warm-up. Neither is the steady
# code under test: its optimized code allocates nothing. Excluded by
# construction: every measured run passes --no-background-compilation, so an
# optimizing compile completes on the mutator before warm-up goes on, and the
# window starts only on installed optimized code. A/B at the same load, 4
# runs at once: the background-compiler program 8 stray windows of 400, this
# one 0 of 400; serial, 300 runs, 0 strays and no trace line in any window.
# The same runs also pass --trace_compiler --trace_deoptimization
# --trace_osr, and any optimizing compile, OSR or deopt line between the
# markers is red (`tiered inside the window`), so the exclusion is checked
# on every run, never assumed; a run whose stderr holds no compiler trace is
# REFUSED. (A first non-empty stderr write before warm-up also moves dart:io's
# first-write bookkeeping, seen compiled inside the window, out of it.) THE
# BOXING BLADE (tables-dart-float-jit-boxing-negative-control) runs first:
# floatjit.dart's `planted-box` shape converts through vm:never-inline
# helpers, so a double crosses a call every pass, and the same run and
# count must read it nonzero in both windows (measured 91 per 2,000,000, 183
# per 4,000,000) or the target is red with BLADE LOST.
FLOATJIT_N ?= 2000000
FLOATJIT_FLAGS := --verbose_gc --no-background-compilation --trace_compiler --trace_deoptimization --trace_osr
FLOATJIT_DIR := build/floatjit
define FLOATJIT_RUN
pin() { \
	v=$$($$1 --version 2>&1); prc=$$?; \
	if [ $$prc -ne 0 ]; then echo "tables-dart-float-jit: REFUSED: the measurement requires Dart SDK $(DART_SDK_PIN) (pinned in make/dart.mk), and $$1 --version exited $$prc"; return 1; fi; \
	case "$$v" in "Dart SDK version: $(DART_SDK_PIN) "*) ;; \
	*) echo "tables-dart-float-jit: REFUSED: the measurement requires Dart SDK $(DART_SDK_PIN) (pinned in make/dart.mk), running: $$v"; return 1;; esac; \
}; \
run() { \
	log=$(FLOATJIT_DIR)/$$1-$$2$$4; rm -f $$log.count; \
	$(DART) $$3 test/dart/floatjit.dart $$1 $$2 >$$log.out 2>$$log.err; rc=$$?; \
	if [ $$rc -ne 0 ]; then echo "tables-dart-float-jit: REFUSED shape=$$1 passes=$$2: dart exited $$rc ($$log.err)"; return 1; fi; \
	if ! grep -Eqx "floatjit shape=$$1 steady=$$2 check=-?[0-9]+" $$log.out; then echo "tables-dart-float-jit: REFUSED shape=$$1 passes=$$2: no completion line ($$log.out)"; return 1; fi; \
	if ! grep -Eq '^main: .*reason=initial' $$log.err; then echo "tables-dart-float-jit: REFUSED shape=$$1 passes=$$2: no GC trace from the main isolate, --verbose_gc absent or unsupported ($$log.err)"; return 1; fi; \
	if [ "$$(grep -Ec '^floatjit: steady window (begins|ends)$$' $$log.err)" != 2 ]; then echo "tables-dart-float-jit: REFUSED shape=$$1 passes=$$2: the steady window markers are not both present ($$log.err)"; return 1; fi; \
	if ! grep -Eq '^Compiling (osr )?optimized function' $$log.err; then echo "tables-dart-float-jit: REFUSED shape=$$1 passes=$$2: no compiler trace, --trace_compiler absent or unsupported ($$log.err)"; return 1; fi; \
	awk '/^floatjit: steady window begins$$/ { w = 1; next } /^floatjit: steady window ends$$/ { w = 0 } w && /^\[ main *, *Scavenge/ { n++ } END { print n + 0 }' $$log.err > $$log.count; \
	awk '/^floatjit: steady window begins$$/ { w = 1; next } /^floatjit: steady window ends$$/ { w = 0 } w && /^(Compiling (osr )?optimized function|Attempting OSR|Deoptimizing|== Deoptimizing)/ { n++ } END { print n + 0 }' $$log.err > $$log.tier; \
}
endef

.PHONY: tables-dart-float-jit-negative-control
tables-dart-float-jit-negative-control: toolchain-dart generated/dart/.stamp
	@mkdir -p $(FLOATJIT_DIR)
	@$(FLOATJIT_RUN); \
	if run planted-exit 1 --verbose_gc -blade >$(FLOATJIT_DIR)/blade-exit.log; then \
		echo "tables-dart-float-jit: BLADE LOST: a planted run that exits 3 was accepted"; exit 1; fi; \
	grep -q 'REFUSED shape=planted-exit passes=1: dart exited 3 ' $(FLOATJIT_DIR)/blade-exit.log || \
		{ echo "tables-dart-float-jit: BLADE LOST: the planted run was refused for another reason"; cat $(FLOATJIT_DIR)/blade-exit.log; exit 1; }; \
	if run generated 1 '' -blade >$(FLOATJIT_DIR)/blade-trace.log; then \
		echo "tables-dart-float-jit: BLADE LOST: a run with no GC trace was accepted"; exit 1; fi; \
	grep -q 'REFUSED shape=generated passes=1: no GC trace' $(FLOATJIT_DIR)/blade-trace.log || \
		{ echo "tables-dart-float-jit: BLADE LOST: the untraced run was refused for another reason"; cat $(FLOATJIT_DIR)/blade-trace.log; exit 1; }; \
	echo "tables-dart-float-jit blade: a planted exit-3 run and an untraced run are both refused"

.PHONY: tables-dart-float-jit-boxing-negative-control
tables-dart-float-jit-boxing-negative-control: toolchain-dart generated/dart/.stamp
	@mkdir -p $(FLOATJIT_DIR)
	@$(FLOATJIT_RUN); \
	pin $(DART) || exit 1; \
	run planted-box $(FLOATJIT_N) '$(FLOATJIT_FLAGS)' '' || { echo "tables-dart-float-jit: BLADE LOST: the planted boxing run was refused"; exit 1; }; \
	run planted-box $$((2 * $(FLOATJIT_N))) '$(FLOATJIT_FLAGS)' '' || { echo "tables-dart-float-jit: BLADE LOST: the planted boxing run was refused"; exit 1; }; \
	a=$$(cat $(FLOATJIT_DIR)/planted-box-$(FLOATJIT_N).count); \
	b=$$(cat $(FLOATJIT_DIR)/planted-box-$$((2 * $(FLOATJIT_N))).count); \
	if [ $$a -eq 0 ] || [ $$b -eq 0 ]; then \
		echo "tables-dart-float-jit: BLADE LOST: a double crossing a vm:never-inline call read steady_scavenges=$$a per $(FLOATJIT_N) passes ($$b per $$((2 * $(FLOATJIT_N)))), not nonzero in both windows"; exit 1; fi; \
	echo "tables-dart-float-jit boxing control: planted shape=planted-box steady_scavenges=$$a per $(FLOATJIT_N) passes ($$b per $$((2 * $(FLOATJIT_N)))) is red: a double crossing a vm:never-inline call is read as boxing in both windows"

.PHONY: tables-dart-float-jit-sdk-negative-control
tables-dart-float-jit-sdk-negative-control:
	@mkdir -p $(FLOATJIT_DIR)/offpin
	@d=$(CURDIR)/$(FLOATJIT_DIR)/offpin; \
	for v in 3.12.0 3.13.20 broken; do \
		f=$$d/dart-$$v; rm -f $$f.measured $$f.log; \
		{ echo '#!/bin/sh'; \
		  if [ $$v = broken ]; then echo 'if [ "$$1" = --version ]; then exit 7; fi'; \
		  else echo "if [ \"\$$1\" = --version ]; then echo 'Dart SDK version: $$v (stable) (Thu Jan 1 00:00:00 2026 +0000) on \"linux_x64\"'; exit 0; fi"; fi; \
		  echo ": > $$f.measured"; echo 'exit 0'; } > $$f; chmod +x $$f; \
		if $(MAKE) --no-print-directory -o toolchain-dart -o generated/dart/.stamp \
				-o tables-dart-float-jit-negative-control -o tables-dart-float-jit-sdk-negative-control \
				-o tables-dart-float-jit-boxing-negative-control \
				tables-dart-float-jit DART=$$f FLOATJIT_N=1 > $$f.log 2>&1; then \
			echo "tables-dart-float-jit: OFF-PIN CONTROL LOST: stand-in SDK $$v was accepted"; cat $$f.log; exit 1; fi; \
		if [ $$v = broken ]; then want="pinned in make/dart.mk), and $$f --version exited 7"; \
		else want="pinned in make/dart.mk), running: Dart SDK version: $$v (stable)"; fi; \
		grep -Fq "tables-dart-float-jit: REFUSED: the measurement requires Dart SDK $(DART_SDK_PIN) ($$want" $$f.log || \
			{ echo "tables-dart-float-jit: OFF-PIN CONTROL LOST: stand-in SDK $$v was refused for another reason"; cat $$f.log; exit 1; }; \
		if [ -e $$f.measured ]; then echo "tables-dart-float-jit: OFF-PIN CONTROL LOST: stand-in SDK $$v was refused only after it was run"; exit 1; fi; \
		if grep -q steady_scavenges $$f.log; then echo "tables-dart-float-jit: OFF-PIN CONTROL LOST: stand-in SDK $$v printed a count"; exit 1; fi; \
	done; \
	echo "tables-dart-float-jit off-pin control: stand-in SDKs 3.12.0, 3.13.20 and a failing --version are each refused before anything is measured"

.PHONY: tables-dart-float-jit
tables-dart-float-jit: toolchain-dart generated/dart/.stamp tables-dart-float-jit-negative-control tables-dart-float-jit-sdk-negative-control tables-dart-float-jit-boxing-negative-control
	@mkdir -p $(FLOATJIT_DIR)
	@$(FLOATJIT_RUN); \
	pin $(DART) || exit 1; \
	fail=0; tier=0; for s in generated inlined twocallers; do \
		run $$s $(FLOATJIT_N) '$(FLOATJIT_FLAGS)' '' || exit 1; \
		run $$s $$((2 * $(FLOATJIT_N))) '$(FLOATJIT_FLAGS)' '' || exit 1; \
		a=$$(cat $(FLOATJIT_DIR)/$$s-$(FLOATJIT_N).count); \
		b=$$(cat $(FLOATJIT_DIR)/$$s-$$((2 * $(FLOATJIT_N))).count); \
		ta=$$(cat $(FLOATJIT_DIR)/$$s-$(FLOATJIT_N).tier); \
		tb=$$(cat $(FLOATJIT_DIR)/$$s-$$((2 * $(FLOATJIT_N))).tier); \
		echo "tables-dart-float-jit shape=$$s steady_scavenges=$$a per $(FLOATJIT_N) passes ($$b per $$((2 * $(FLOATJIT_N)))) tiering_in_window=$$ta ($$tb)"; \
		[ $$a -eq 0 ] && [ $$b -eq 0 ] || fail=1; \
		[ $$ta -eq 0 ] && [ $$tb -eq 0 ] || tier=1; \
	done; \
	echo "tables-dart-float-jit sdk: $$($(DART) --version 2>&1)"; \
	if [ $$tier -ne 0 ]; then echo "tables-dart-float-jit: FAIL (the JIT tiered inside the window: an optimizing compile, OSR or deopt between the markers)"; exit 1; fi; \
	if [ $$fail -ne 0 ]; then echo "tables-dart-float-jit: FAIL (a float boxes under the JIT)"; exit 1; fi; \
	echo "tables-dart-float-jit: OK"

test-dart: tables-dart-float-jit tables-dart-float-jit-negative-control tables-dart-float-jit-sdk-negative-control tables-dart-float-jit-boxing-negative-control

# THE COOK OF A MAP FIELD (docs/SPEC-TABLES.md §2.8, §7.2): the Go cook writes a
# Squad (tables/lists) with a known roster, an absent map and an emptied one,
# and a unit holding every key kind §2.8 admits; the Dart programs in
# test/dart-tables read EVERY entry, by index and by Find, and must print the
# oracle's lines entry for entry. A bare go test skips by name without the SDK;
# SCHEMA_REQUIRE_CORPUS=1 makes that skip a failure.
.PHONY: tables-dart-cook-map-field
tables-dart-cook-map-field:
	@mkdir -p build/dart-cook-map-field
	SCHEMA_REQUIRE_CORPUS=1 DART=$(DART) go test ./internal/codegen/darttable/ -count=1 -timeout 600s -v -run 'TestCookMapFieldExposesItsEntriesLikeGo' > build/dart-cook-map-field/log 2>&1 || { cat build/dart-cook-map-field/log; exit 1; }
	@grep 'lines agree' build/dart-cook-map-field/log | sed 's/^ *//'
	@echo 'tables Dart cook: every map entry read as the Go cook reads it'

test-dart: tables-dart-cook-map-field

# I13 (docs/PORTING.md): the Dart leg's text differential against a third
# implementation. A Dart probe builds N random float-only instances of a
# variable root with the generated setters and the Go cell's splitmix64, writes
# each as (wire, text); inside Go the engine (internal/tabletext) writes its own
# text from the same wire, the two are byte-compared, and the engine packs the
# leg's text back to wire the other way. The control caps a float32 at six
# significant digits in the emitted Dart, in memory, and requires red on the
# text comparison. The probe builds its own unit into a temp directory, so no
# corpus stamp is a prerequisite.
.PHONY: tables-dart-text-differential tables-dart-text-differential-negative-control
tables-dart-text-differential:
	SCHEMA_SLOW=1 SCHEMA_REQUIRE_CORPUS=1 DART=$(DART) go test ./internal/codegen/darttable/ -count=1 -timeout 600s -run '^TestDartTextDifferentialAgainstTheGoEngine$$'
	@echo 'tables Dart text differential: the generated text writer agrees with the engine byte for byte, both directions'
tables-dart-text-differential-negative-control:
	SCHEMA_SLOW=1 SCHEMA_REQUIRE_CORPUS=1 DART=$(DART) go test ./internal/codegen/darttable/ -count=1 -timeout 600s -run '^TestDartTextDifferentialNegativeControl$$'
	@echo 'tables Dart text differential negative control: a float32 capped at six digits reds the text comparison'

test-dart: tables-dart-text-differential tables-dart-text-differential-negative-control

# THE BLOCK BUILD AND COOK WRITE GATE (docs/SPEC-TABLES.md §19.1, §7.6): the
# block fill path's begin against §19.1's layout facts, the fixed-root cook
# writer and a guarded and a text-array root against `schema cook` in both byte
# orders, and every `cook-write` row
# of testdata/conformance/tables/MANIFEST.txt through the conformance driver's
# own surface — a byte-identical cook in both orders, or the named absence the
# test's ratchet holds. The row test runs the driver over the generated corpus,
# so the corpus is built first; SCHEMA_REQUIRE_CORPUS=1 makes a missing SDK or
# corpus a failure rather than a skip.
.PHONY: tables-dart-block-build
tables-dart-block-build: build/tables-generated-dart/.stamp cards-dart-generate
	SCHEMA_REQUIRE_CORPUS=1 DART=$(DART) go test ./internal/codegen/darttable/ -count=1 -timeout 600s -run '^(TestBlockBuildAndCookWriteDart|TestCookWriteFixedDart|TestCookWriteGuardedAndTextArrayRootsDart|TestCookWriteAnswersEveryCookWriteRowDart)$$'
	@echo 'tables Dart block build and cook write: begin, the fixed root, the guarded and text-array roots and every cook-write row'

test-dart: tables-dart-block-build
# MAPS AND UNION ARMS (docs/SPEC-TABLES.md §2.4, §2.6, §2.8, §3, §3.2): maps,
# unions whose arms are tables, arrays of unions, arms of any carried field
# type, enum-keyed and fixed positional arrays and wstring(N) on form 1, byte
# for byte against the oracle rows of tables/maps and tables/arms, and of the
# fixed-root units tables/messages, tables/scalars and examples-wide (optional
# fields and arrays, flags, `type` bodies, fixed(I,F) and the 128-bit scalars).
# SCHEMA_REQUIRE_CORPUS=1 makes a missing SDK a failure.
.PHONY: tables-dart-variable-arms
tables-dart-variable-arms:
	SCHEMA_REQUIRE_CORPUS=1 DART=$(DART) go test ./internal/codegen/darttable/ -count=1 -timeout 600s -run 'TestMapsAndUnionArmsOnTheTableWireDart|TestOtherVariableConstructsAreRefusedByName'
	@echo 'tables Dart variable arms: maps, unions, keyed arrays, optionals and the wide scalars against the oracle bytes'

test-dart: tables-dart-variable-arms
# THE MESSAGE FORM'S BATCH (docs/SPEC-TABLES.md §3.3): every manifest `message`
# row of a fixed root loads, measures and saves the reference's bytes exactly,
# and every unit's fixed home defines the table-arm unions it names (§2.6,
# §3.4), so no row is skipped but the §3.3 plain-table one. A bare go test
# skips when the SDK is absent; SCHEMA_REQUIRE_CORPUS=1 makes that a failure.
.PHONY: tables-dart-message-batch
tables-dart-message-batch:
	SCHEMA_REQUIRE_CORPUS=1 DART=$(DART) go test ./internal/codegen/darttable/ -count=1 -timeout 600s -run 'TestMessageFormBatchDart|TestFixedHomeDefinesItsTableArmUnionsDart|TestNestedDefaultMessageBytesMatchGoEmitter'
	@echo 'tables Dart message batch: every fixed-root message row against the reference bytes, and the default nested body against the Go emitter'

test-dart: tables-dart-message-batch

# WIDENING ON READ, AND THE REFUSAL REASONS (docs/SPEC-TABLES.md §3, §4, §7,
# §19.2): every narrower integer kind into every wider field on its ladder and
# f32 into f64, each read equal to the Go engine's; the wire report's reason;
# the cook's and the block's Open reason, one forgery per clause; and the
# TableRefuseReason words held to internal/codegen/gotable/refusal.go's. The
# conformance driver's report, cook-reason and block-reason surfaces answer the
# manifest's rows under `make conformance`. A bare go test skips when the SDK
# is absent; SCHEMA_REQUIRE_CORPUS=1 makes that a failure.
.PHONY: tables-dart-widening
tables-dart-widening:
	SCHEMA_REQUIRE_CORPUS=1 DART=$(DART) go test ./internal/codegen/darttable/ -count=1 -timeout 600s -run '^TestWideningReasonsDart$$'
	@echo 'tables Dart widening: every narrower kind widens as the Go engine reads it, and every refusal names the reference leg reason'

test-dart: tables-dart-widening


# The fixed class also speaks form 1. The W/R pairs pin defaults and `was`
# aliases against the Go oracle (docs/SPEC-TABLES.md §3, §5).
.PHONY: tables-dart-variable-fixed-class
tables-dart-variable-fixed-class:
	SCHEMA_REQUIRE_CORPUS=1 DART=$(DART) go test ./internal/codegen/darttable/ -count=1 -timeout 600s -run TestFixedClassOnTableWireDart

test-dart: tables-dart-variable-fixed-class

# FINITE TABLE FLOATS ON THE DART LEG (docs/SPEC-TABLES.md §3.3; Glenn,
# 2026-10-27: "Tables go finite-only too, same rule as packets"): every read
# refuses a NaN or an infinity on its raw bits as non_finite_float, naming the
# field — the variable form (scalars, list and [N] elements, union arms and a
# retained unknown float), the message form, the fixed form, and the block and
# cook accessors, which never return one and latch the refusal on the view —
# every writer refuses one, and -0 rides by its bits. The reversed witness
# takes every read-side bit test out of the generated source and requires
# EXACTLY the read vectors to go red, the writers and -0 staying green.
.PHONY: tables-dart-finite-float tables-dart-finite-float-negative-control
tables-dart-finite-float:
	SCHEMA_SLOW=1 SCHEMA_REQUIRE_CORPUS=1 DART=$(DART) go test ./internal/codegen/darttable/ -count=1 -timeout 600s -run '^TestDartFiniteFloatsEveryForm$$'
	@echo 'tables Dart finite floats: every read and every writer refuses a NaN or an infinity, and -0 rides'
tables-dart-finite-float-negative-control:
	SCHEMA_SLOW=1 SCHEMA_REQUIRE_CORPUS=1 DART=$(DART) go test ./internal/codegen/darttable/ -count=1 -timeout 600s -run '^TestDartFiniteFloatNegativeControl$$'
	@echo 'negative control: with the read-side bit tests removed, exactly the Dart read vectors go red'

test-dart: tables-dart-finite-float tables-dart-finite-float-negative-control

# DESCRIPTORS ARE CONSTANTS (docs/PORTING.md M13): the card registry, the unit
# view, the extent scan's tables and the retained-id set are `const`, so a read
# allocates nothing, runs no lazy initializer and cannot write. The probe
# requires each registry identical across reads and unmodifiable; its reversed
# witness turns the card registry and the unit view back into finals in a copy
# of the generated source and requires the probe red, naming both.
.PHONY: tables-dart-descriptors
tables-dart-descriptors:
	SCHEMA_SLOW=1 SCHEMA_REQUIRE_CORPUS=1 DART=$(DART) go test ./internal/codegen/darttable/ -count=1 -timeout 600s -run '^TestDartDescriptorsAreConstants$$'
	@echo 'tables Dart descriptors: every registry is const, identical across reads and unmodifiable, and the finals witness goes red'

test-dart: tables-dart-descriptors

# THE IDENTITY MAP (docs/PORTING.md M6, docs/SPEC-TABLES.md §3.1): a node two
# pointers reach is one record and one object on the read, two equal nodes stay
# two, and a cycle is refused at its OPEN entry by name. The reversed witness
# takes the OPEN-entry test out of the generated runtime and requires the probe
# red on the cycle alone.
.PHONY: tables-dart-identity tables-dart-identity-negative-control
tables-dart-identity:
	SCHEMA_SLOW=1 SCHEMA_REQUIRE_CORPUS=1 DART=$(DART) go test ./internal/codegen/darttable/ -count=1 -timeout 600s -run '^TestWireIdentityMapDart$$'
	@echo 'tables Dart identity map: a shared node is one record, a cycle is refused, and the blinded witness goes red'

tables-dart-identity-negative-control:
	SCHEMA_SLOW=1 SCHEMA_REQUIRE_CORPUS=1 DART=$(DART) go test ./internal/codegen/darttable/ -count=1 -timeout 600s -run '^TestWireIdentityMapDartNegativeControl$$'
	@echo 'negative control: with the OPEN-entry test out of the generated runtime, the identity probe goes red on the cycle alone'

test-dart: tables-dart-identity tables-dart-identity-negative-control

# BYTE ORDER IS THE READER'S (docs/PORTING.md M12): every multi-byte ByteData
# call in every corpus unit's generated Dart names its Endian, so the host's
# order is never consulted. The control strips one call's Endian.little and
# requires the scan to name exactly that call.
.PHONY: tables-dart-byte-order tables-dart-byte-order-negative-control
tables-dart-byte-order:
	SCHEMA_SLOW=1 go test ./internal/codegen/darttable/ -count=1 -run '^TestDartByteOrderIsExplicit$$'
	@echo 'tables Dart byte order: every multi-byte ByteData call names its byte order'

tables-dart-byte-order-negative-control:
	SCHEMA_SLOW=1 go test ./internal/codegen/darttable/ -count=1 -run '^TestDartByteOrderIsExplicitNegativeControl$$'
	@echo 'negative control: one generated call stripped of Endian.little, and the scan names it'

test-dart: tables-dart-byte-order tables-dart-byte-order-negative-control

# THE WRITER HOLDS THE MAP'S SORT (docs/PORTING.md M19, docs/SPEC-TABLES.md
# §2.8): a map built out of key order saves and writes text exactly as the
# ascending one does, and reads back ascending, for a string and a signed key.
# The reversed witness leaves the entries in stored order and requires red.
.PHONY: tables-dart-map-sort tables-dart-map-sort-negative-control
tables-dart-map-sort:
	SCHEMA_SLOW=1 SCHEMA_REQUIRE_CORPUS=1 DART=$(DART) go test ./internal/codegen/darttable/ -count=1 -timeout 600s -run '^TestMapWriterSortDart$$'
	@echo 'tables Dart map sort: the writer sorts a map built out of order, and the unsorted witness goes red'

tables-dart-map-sort-negative-control:
	SCHEMA_SLOW=1 SCHEMA_REQUIRE_CORPUS=1 DART=$(DART) go test ./internal/codegen/darttable/ -count=1 -timeout 600s -run '^TestMapWriterSortDartNegativeControl$$'
	@echo 'negative control: with the writer sort a no-op in the generated runtime, the map-sort probe goes red on the saved bytes'

test-dart: tables-dart-map-sort tables-dart-map-sort-negative-control

# THE DART LEG of `make test`. THE DART PORT's own instruments
# (docs/SPEC-TABLES.md): the emitted block and cook sources held to what `dart
# format` writes and what the analyzer accepts, the name-claim control, the
# standalone gate and its control, the forgery fuzzer and its control — the
# long fuzz is `make tables-dart-release` — then the analyzer and the formatter
# over every generated tree, and the packet tests, checked and compiled.
.PHONY: test-dart
test-dart: toolchain-dart generated/dart/.stamp generated/dart-ludicrous/.stamp generated/bench/dart/.stamp
	$(MAKE) tables-dart-clean
	$(MAKE) tables-dart-names-negative-control
	$(MAKE) tables-dart-standalone
	$(MAKE) tables-dart-standalone-negative-control
	$(MAKE) tables-dart-fuzz DART_FUZZ_MUTANTS=1500
	$(MAKE) tables-dart-fuzz-negative-control
	$(MAKE) tables-dart-alloc
	$(MAKE) tables-dart-alloc-negative-control
	$(MAKE) tables-dart-alloc-runtime-negative-control
	$(MAKE) tables-dart-order
	$(MAKE) tables-dart-order-negative-control
	# THE FIXED FORM against the C++ reference's own bytes (§3.4)
	$(MAKE) tables-dart-fixed-form
	$(MAKE) tables-dart-fixed-form-negative-control
	# AND IT READS BACKWARD (§5): every versioning row, both columns
	$(MAKE) tables-dart-versioning
	$(MAKE) tables-dart-versioning-negative-control
	$(DART) analyze generated/dart generated/dart-ludicrous generated/bench/dart test/dart test/dart-ludicrous bench/dart
	$(DART) format --set-exit-if-changed --output=none generated/dart generated/dart-ludicrous generated/bench/dart
	cd test/dart && $(DART) --enable-asserts main.dart
	@mkdir -p build
	cd test/dart && $(DART) compile exe -o ../../build/schema_test_dart main.dart >/dev/null && ../../build/schema_test_dart
	cd test/dart-ludicrous && $(DART) --enable-asserts main.dart
	cd test/dart-ludicrous && $(DART) compile exe -o ../../build/schema_test_dart_ludicrous main.dart >/dev/null && ../../build/schema_test_dart_ludicrous

TEST_LEGS         += test-dart
TOOLCHAIN_LEGS    += dart
TOOLCHAIN_PINS_dart := DART
CONFORMANCE_LEGS  += $(call unless_skipped,dart,build/conformance-dart)
# Packet UTF-8 content validation, including a compiled mutation control.
build/packet-text/dart/.stamp: bin/schema test/packet-text/Narrow.schema
	./bin/schema generate --lang dart --out build/packet-text/dart test/packet-text/Narrow.schema
	@touch $@

.PHONY: packet-utf8-dart packet-utf8-dart-negative-control
packet-utf8-dart: build/packet-text/dart/.stamp build/packet-text/cpp/driver build/packet-text/harness
	$(DART) analyze build/packet-text/dart test/packet-text/dart
	$(DART) format --set-exit-if-changed --output=none build/packet-text/dart test/packet-text/dart
	./build/packet-text/harness $(DART) --enable-asserts test/packet-text/dart/main.dart
	$(DART) compile exe -o build/packet-text/dart/driver test/packet-text/dart/main.dart
	./build/packet-text/harness ./build/packet-text/dart/driver

packet-utf8-dart-negative-control: packet-utf8-dart
	@mkdir -p build/packet-text/dart-negative
	go run ./tools/sabotage -name packet-utf8-dart-read -out build/packet-text/dart-negative/utf8.gotext internal/codegen/dart/utf8.go
	@printf '{"Replace":{"%s/internal/codegen/dart/utf8.go":"%s/build/packet-text/dart-negative/utf8.gotext"}}\n' "$(CURDIR)" "$(CURDIR)" > build/packet-text/dart-negative/overlay.json
	go run -overlay=build/packet-text/dart-negative/overlay.json ./cmd/schema generate --lang dart --out build/packet-text/dart-negative test/packet-text/Narrow.schema
	@sed 's|../../../build/packet-text/dart|$(CURDIR)/build/packet-text/dart-negative|' test/packet-text/dart/main.dart > build/packet-text/dart-negative/main.dart
	$(DART) compile exe -o build/packet-text/dart-negative/driver build/packet-text/dart-negative/main.dart
	@if ./build/packet-text/harness -mutations-only ./build/packet-text/dart-negative/driver > build/packet-text/dart-negative/log 2>&1; then echo 'NEGATIVE CONTROL FAILED: Dart UTF-8 removal passed'; exit 1; fi
	@grep -Fq 'FAILED: packet-text verdict on ' build/packet-text/dart-negative/log || { cat build/packet-text/dart-negative/log; exit 1; }
	@echo 'packet UTF-8 Dart negative control: removed read validation fails bit-flip agreement'

test-dart: packet-utf8-dart packet-utf8-dart-negative-control

# Packet floats are finite (SPEC §4.3): write refusals in the checked twin, read
# refusals from crafted bytes and bit-exact -0/subnormals in both twins, and a
# control that removes the read check and must red exactly the read refusals.
build/packet-finite/dart/.stamp: bin/schema test/packet-finite/dart/Finite.schema
	./bin/schema generate --lang dart --out build/packet-finite/dart test/packet-finite/dart/Finite.schema
	@touch $@

.PHONY: packet-finite-dart packet-finite-dart-negative-control
packet-finite-dart: build/packet-finite/dart/.stamp
	$(DART) analyze build/packet-finite/dart test/packet-finite/dart
	$(DART) format --set-exit-if-changed --output=none build/packet-finite/dart test/packet-finite/dart
	$(DART) --enable-asserts test/packet-finite/dart/main.dart
	$(DART) compile exe -o build/packet-finite/dart/checker test/packet-finite/dart/main.dart >/dev/null
	./build/packet-finite/dart/checker

packet-finite-dart-negative-control: packet-finite-dart
	@rm -rf build/packet-finite/dart-negative && mkdir -p build/packet-finite/dart-negative
	go run ./tools/sabotage -name packet-finite-dart-read -out build/packet-finite/dart-negative/functions.gotext internal/codegen/dart/functions.go
	@printf '{"Replace":{"%s/internal/codegen/dart/functions.go":"%s/build/packet-finite/dart-negative/functions.gotext"}}\n' "$(CURDIR)" "$(CURDIR)" > build/packet-finite/dart-negative/overlay.json
	go run -overlay=build/packet-finite/dart-negative/overlay.json ./cmd/schema generate --lang dart --out build/packet-finite/dart-negative/generated test/packet-finite/dart/Finite.schema
	@sed 's|../../../build/packet-finite/dart/|$(CURDIR)/build/packet-finite/dart-negative/generated/|' test/packet-finite/dart/main.dart > build/packet-finite/dart-negative/main.dart
	@if $(DART) --enable-asserts build/packet-finite/dart-negative/main.dart > build/packet-finite/dart-negative/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: Dart packet floats with no read check passed'; exit 1; fi
	@grep -Fq 'FAILED: non_finite_float read refusal: ' build/packet-finite/dart-negative/log || \
		{ echo 'NEGATIVE CONTROL FAILED: Dart failed for another reason'; cat build/packet-finite/dart-negative/log; exit 1; }
	@if grep -v 'FAILED: non_finite_float read refusal: ' build/packet-finite/dart-negative/log | grep -Fq 'FAILED:'; then \
		echo 'NEGATIVE CONTROL FAILED: a check other than the read refusals went red'; cat build/packet-finite/dart-negative/log; exit 1; fi
	@echo "packet finite Dart negative control: removed read check reds $$(grep -c 'FAILED: non_finite_float read refusal: ' build/packet-finite/dart-negative/log) read refusals and nothing else"

test-dart: packet-finite-dart packet-finite-dart-negative-control


build/packet-wide/dart/.stamp: bin/schema build/packet-wide/source/WideText.schema test/packet-wide/Shapes.schema
	./bin/schema generate --lang dart --out build/packet-wide/dart build/packet-wide/source/WideText.schema
	./bin/schema generate --lang dart --out build/packet-wide/dart/shapes test/packet-wide/Shapes.schema
	@touch $@

.PHONY: packet-wide-dart packet-wide-dart-negative-control
packet-wide-dart: build/packet-wide/dart/.stamp build/packet-wide/cpp/driver build/packet-text/harness
	$(DART) analyze build/packet-wide/dart test/packet-wide/dart
	$(DART) format --set-exit-if-changed --output=none build/packet-wide/dart test/packet-wide/dart
	$(DART) --enable-asserts test/packet-wide/dart/main.dart --contracts
	./build/packet-text/harness -wide -corpus testdata/conformance/text/wstring.txt -oracle build/packet-wide/cpp/driver $(DART) --enable-asserts test/packet-wide/dart/main.dart
	$(DART) compile exe -o build/packet-wide/dart/driver test/packet-wide/dart/main.dart
	./build/packet-wide/dart/driver --contracts
	./build/packet-text/harness -wide -corpus testdata/conformance/text/wstring.txt -oracle build/packet-wide/cpp/driver ./build/packet-wide/dart/driver

packet-wide-dart-negative-control: packet-wide-dart
	@mkdir -p build/packet-wide/dart-negative
	go run ./tools/sabotage -name packet-wide-dart-pairing -out build/packet-wide/dart-negative/wstring.gotext internal/codegen/dart/wstring.go
	@printf '{"Replace":{"%s/internal/codegen/dart/wstring.go":"%s/build/packet-wide/dart-negative/wstring.gotext"}}\n' "$(CURDIR)" "$(CURDIR)" > build/packet-wide/dart-negative/overlay.json
	go run -overlay=build/packet-wide/dart-negative/overlay.json ./cmd/schema generate --lang dart --out build/packet-wide/dart-negative build/packet-wide/source/WideText.schema
	@sed -e 's|../../../build/packet-wide/dart/WideText.dart|$(CURDIR)/build/packet-wide/dart-negative/WideText.dart|' -e 's|../../../build/packet-wide/dart/shapes/Shapes.dart|$(CURDIR)/build/packet-wide/dart/shapes/Shapes.dart|' test/packet-wide/dart/main.dart > build/packet-wide/dart-negative/main.dart
	$(DART) compile exe -o build/packet-wide/dart-negative/driver build/packet-wide/dart-negative/main.dart
	@if ./build/packet-text/harness -wide -corpus testdata/conformance/text/wstring.txt -oracle build/packet-wide/cpp/driver -mutations-only ./build/packet-wide/dart-negative/driver > build/packet-wide/dart-negative/log 2>&1; then echo 'NEGATIVE CONTROL FAILED: Dart wide pairing removal passed'; exit 1; fi
	@grep -Fq 'FAILED: packet-text verdict on ' build/packet-wide/dart-negative/log || { cat build/packet-wide/dart-negative/log; exit 1; }
	@echo 'packet wide Dart negative control: removed pairing fails bit-flip agreement'

test-dart: packet-wide-dart packet-wide-dart-negative-control

# THE CARDS UNIT'S DART LEG (docs/SPEC-TABLES.md §2). These targets are the
# Dart side of the cards wire-fuzz gate: they mirror the Go leg's
# cards-generate and cards-wire-fuzz (make/go.mk) against the same oracle
# harness and the same seed, so the Dart Card codec can be held to the
# identical byte-for-byte contract. They live in make/dart.mk (NOT a separate
# make/cards-dart.mk or make/cards-dart-retain.mk) because the wide-scalar
# refusal gate (tables-ports-refuse-wide-scalars) discovers every make/*.mk as
# a live port, and a fragment named cards-dart.mk or cards-dart-retain.mk
# would be refused there as a phantom port.
CARDS_DART_FUZZ_TIME ?= 30s

# cards-dart-generate produces ONLY build/tables-generated-dart/cards, cleaning
# that owned output first so a deleted symbol cannot survive a regeneration.
.PHONY: cards-dart-generate
cards-dart-generate: bin/schema
	@rm -rf build/tables-generated-dart/cards
	@mkdir -p build/tables-generated-dart/cards
	./bin/schema generate --lang dart --out build/tables-generated-dart/cards tables/cards

# cards-dart-wire-fuzz is the oracle harness over the Card root only. --vectors
# '' keeps the default pinned vectors out of this Card-only manifest. The greps
# hold the nonzero seed/root/mutant claim and the zero-divergence claim, as
# make/go.mk's cards-wire-fuzz does.
.PHONY: cards-dart-wire-fuzz
cards-dart-wire-fuzz: cards-dart-generate build/conformance-harness build/conformance-dart
	@timeout $(CARDS_DART_FUZZ_TIME) ./build/conformance-harness wire-fuzz \
		--manifest tables/cards/fuzz/MANIFEST.txt --json tables/cards/fuzz --vectors '' \
		--driver 'build/conformance-dart wire-fuzz' --seed 24845619678 --n 100 \
		> build/cards-dart-wire-fuzz.log 2>&1; \
		status=$$?; cat build/cards-dart-wire-fuzz.log; \
		if [ $$status -ne 0 ]; then exit $$status; fi
	@grep -q '0 divergences' build/cards-dart-wire-fuzz.log
	@grep -qE '[1-9][0-9]* seeds over [1-9][0-9]* roots' build/cards-dart-wire-fuzz.log
	@grep -qE '[1-9][0-9]* mutants' build/cards-dart-wire-fuzz.log

# THE CARD WIRE-FUZZ RIDES THE DART LEG'S GATE (§4.2): the same oracle harness,
# the same seed, over the Card root only, so the Dart Card codec is held to the
# identical byte-for-byte contract the Go leg's cards-wire-fuzz pins.
test-dart: cards-dart-wire-fuzz

# cards-dart-retain-fuzz is the SAME fuzzer through the RETENTION ARM
# (docs/SPEC-TABLES.md §6.6): every mutant is read and written back through the
# Card codec's retaining pair, and the two retention counters ride beside the
# six. The seed, count and coverage greps are cards-dart-wire-fuzz's, plus
# --retain, so a retained save is held to the Go reference leg byte for byte.
.PHONY: cards-dart-retain-fuzz
cards-dart-retain-fuzz: cards-dart-generate build/conformance-harness build/conformance-dart
	@timeout $(CARDS_DART_FUZZ_TIME) ./build/conformance-harness wire-fuzz \
		--manifest tables/cards/fuzz/MANIFEST.txt --json tables/cards/fuzz --vectors '' \
		--driver 'build/conformance-dart wire-fuzz' --seed 24845619678 --n 100 --retain \
		> build/cards-dart-retain-fuzz.log 2>&1; \
		status=$$?; cat build/cards-dart-retain-fuzz.log; \
		if [ $$status -ne 0 ]; then exit $$status; fi
	@grep -q '0 divergences' build/cards-dart-retain-fuzz.log
	@grep -qE '[1-9][0-9]* seeds over [1-9][0-9]* roots' build/cards-dart-retain-fuzz.log
	@grep -qE '[1-9][0-9]* mutants' build/cards-dart-retain-fuzz.log

# THE CARD WIRE-FUZZ'S RETAIN ARM RIDES THE DART LEG'S GATE (§6.6): the same
# oracle harness, the same seed, over the Card root only, so the Dart Card
# codec's retained save is held to the identical byte-for-byte contract the Go
# leg's cards-retain-fuzz pins.
test-dart: cards-dart-retain-fuzz

# THE CARDS UNIT'S DART LEG, L1 and L2 (docs/SPEC-TABLES.md §2): the owning
# facade (tables/cards/dart/lib/cards.dart) over the generated Card codec, the
# brief text parsed once and rendered many times, and the driver that answers
# the card conformance check (tables/cards/conformance/FORMAT.md). These targets
# live in make/dart.mk (NOT a separate make/cards-dart.mk) for the same reason
# cards-dart-wire-fuzz does: the wide-scalar refusal gate discovers every
# make/*.mk as a live port, and a fragment named cards-dart.mk would be refused
# there as a phantom port.

# cards-dart-test builds the generated unit and the facade, holds them to the
# analyzer and the formatter, and runs the plain main test runner (one check per
# named test, nonzero exit on any failure; no package:test dependency).
.PHONY: cards-dart-test
cards-dart-test: cards-dart-generate
	$(DART) analyze tables/cards/dart
	$(DART) format --set-exit-if-changed --output=none tables/cards/dart
	$(DART) --enable-asserts tables/cards/dart/test/cards_test.dart

# cards-dart-driver compiles the facade driver to one AOT executable, the way
# build/conformance-dart does, so the conformance check pays no JIT start-up per
# surface.
.PHONY: cards-dart-driver
cards-dart-driver: cards-dart-generate
	@mkdir -p build
	$(DART) compile exe -o build/cards-driver-dart tables/cards/dart/bin/driver.dart >/dev/null

# cards-dart-conformance runs the harness `cards` subcommand over the Dart
# driver with all nine surfaces required and no absence accepted.
.PHONY: cards-dart-conformance
cards-dart-conformance: build/conformance-harness cards-dart-driver
	./build/conformance-harness cards --corpus tables/cards/conformance --driver tables/cards/dart/driver --require load,save,render,parse,bounds,upgrade,edit,taskhash,view

# cards-dart-conformance-upgrade-negative-control: a test-only switch drops
# one retained field; upgrade alone must go red.
.PHONY: cards-dart-conformance-upgrade-negative-control
cards-dart-conformance-upgrade-negative-control: build/conformance-harness cards-dart-driver
	@if CARDS_DART_SABOTAGE_UPGRADE=1 ./build/conformance-harness cards --corpus tables/cards/conformance --driver tables/cards/dart/driver --require load,save,render,parse,bounds,upgrade,edit,taskhash,view > build/cards-dart-upgrade-neg.log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: the card check stayed green with a retained field dropped'; exit 1; fi
	@grep -qE 'cards upgrade .*FAIL' build/cards-dart-upgrade-neg.log || { echo 'NEGATIVE CONTROL FAILED: upgrade did not go red'; cat build/cards-dart-upgrade-neg.log; exit 1; }
	@echo 'cards Dart upgrade negative control: one dropped retained field turns exactly upgrade red'

# cards-dart-conformance-hash-negative-control: a test-only switch adds needs
# to the hash; taskhash alone must go red.
.PHONY: cards-dart-conformance-hash-negative-control
cards-dart-conformance-hash-negative-control: build/conformance-harness cards-dart-driver
	@if CARDS_DART_SABOTAGE_HASH=1 ./build/conformance-harness cards --corpus tables/cards/conformance --driver tables/cards/dart/driver --require load,save,render,parse,bounds,upgrade,edit,taskhash,view > build/cards-dart-hash-neg.log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: the card check stayed green with needs in the hash'; exit 1; fi
	@grep -qE 'cards taskhash .*FAIL' build/cards-dart-hash-neg.log || { echo 'NEGATIVE CONTROL FAILED: taskhash did not go red'; cat build/cards-dart-hash-neg.log; exit 1; }
	@echo 'cards Dart hash negative control: needs in the hash turns exactly taskhash red'

# cards-dart-conformance-negative-control: the same check against the driver
# with a test-only environment switch that changes one rendered byte, required
# to FAIL on render while the other four surfaces pass.
.PHONY: cards-dart-conformance-negative-control
cards-dart-conformance-negative-control: build/conformance-harness cards-dart-driver
	@if CARDS_DART_SABOTAGE=1 ./build/conformance-harness cards --corpus tables/cards/conformance --driver tables/cards/dart/driver --require load,save,render,parse,bounds > build/cards-dart-conformance-neg.log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: the card check stayed green with one rendered byte changed'; exit 1; fi
	@grep -qE 'cards render .*FAIL' build/cards-dart-conformance-neg.log || { echo 'NEGATIVE CONTROL FAILED: render did not go red'; cat build/cards-dart-conformance-neg.log; exit 1; }
	@grep -qE 'cards (load|save|parse|bounds) pass' build/cards-dart-conformance-neg.log || { echo 'NEGATIVE CONTROL FAILED: the other surfaces did not stay green'; cat build/cards-dart-conformance-neg.log; exit 1; }
	@echo 'cards Dart negative control: one changed rendered byte turns exactly render red'

test-dart: cards-dart-test cards-dart-conformance cards-dart-conformance-negative-control cards-dart-conformance-upgrade-negative-control cards-dart-conformance-hash-negative-control

# THE ACCESSOR/DESCRIPTOR AGREEMENT GATE (docs/PORTING.md J1; issue #421). Every
# row of the block images, every node of the cook images and every record type
# either descriptor graph reaches is read through the generated accessor and
# through the descriptor's offset, kind, width and signedness, and the two must
# agree. It prints the counts it read: a gate that read nothing says so.
.PHONY: tables-dart-agree
tables-dart-agree: build/tables-generated-dart/.stamp build/cook-fuzz/.stamp
	$(DART) test/dart-tables/agree.dart

# ITS TWO NEGATIVE CONTROLS, each through go build -overlay on a COPY of an
# emitter, so no tracked file is written to. Each must turn the gate red ON THE
# NAMED FIELD and on nothing else, which is also the proof that the other
# derivation still passes: a gate that went red on both would be red for some
# other reason.
#
# THE ACCESSOR control moves one scalar accessor four bytes in the row emitter
# (block.go, which writes the cook's rows too): RenderCamera.camera_id reads
# the next field's bytes, the descriptor still says where it is. The anchor is
# the offset argument of the plain scalar getter's scalarGetter call (the
# getter body is a statement since issue 404, so the offset is on its own line).
.PHONY: tables-dart-accessor-negative-control
tables-dart-accessor-negative-control: build/cook-fuzz/.stamp
	@rm -rf build/dart-agree-acc-nc && mkdir -p build/dart-agree-acc-nc
	@sed -e 's|fmt.Sprintf("%s + %d", origin, fl.Offset))$$|fmt.Sprintf("%s + %d", origin, fl.Offset+accessorMoved(f.Name)))|' \
		internal/codegen/darttable/block.go > build/dart-agree-acc-nc/block.go.txt
	@grep -q 'accessorMoved(f.Name)' build/dart-agree-acc-nc/block.go.txt || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage matched nothing - the emitter moved"; exit 1; }
	@printf '\nfunc accessorMoved(name string) int64 {\n\tif name == "camera_id" {\n\t\treturn 4\n\t}\n\treturn 0\n}\n' >> build/dart-agree-acc-nc/block.go.txt
	@printf '{"Replace":{"%s/internal/codegen/darttable/block.go":"%s/build/dart-agree-acc-nc/block.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/dart-agree-acc-nc/overlay.json
	go build -overlay build/dart-agree-acc-nc/overlay.json -o build/dart-agree-acc-nc/schema ./cmd/schema
	./build/dart-agree-acc-nc/schema generate --lang dart --out build/dart-agree-acc-nc/generated/block tables/block
	./build/dart-agree-acc-nc/schema generate --lang dart --out build/dart-agree-acc-nc/generated/pointers tables/pointers
	@sed -e "s|../../build/tables-generated-dart/block/|$(CURDIR)/build/dart-agree-acc-nc/generated/block/|g" \
	     -e "s|../../build/tables-generated-dart/pointers/|$(CURDIR)/build/dart-agree-acc-nc/generated/pointers/|g" \
		test/dart-tables/agree.dart > build/dart-agree-acc-nc/agree.dart
	@if $(DART) build/dart-agree-acc-nc/agree.dart > build/dart-agree-acc-nc/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the gate stayed green with an accessor four bytes off"; \
		tail -5 build/dart-agree-acc-nc/log; exit 1; \
	fi
	@grep -q '^DISAGREE  RenderCamera .*\.cameraId: accessor' build/dart-agree-acc-nc/log || \
		{ echo "NEGATIVE CONTROL FAILED: the gate went red, but not on RenderCamera.cameraId"; \
		  tail -20 build/dart-agree-acc-nc/log; exit 1; }
	@if grep '^DISAGREE' build/dart-agree-acc-nc/log | grep -v '^DISAGREE  RenderCamera .*\.cameraId: accessor'; then \
		echo "NEGATIVE CONTROL FAILED: the gate went red on a field other than the one moved"; exit 1; \
	fi
	@grep -m1 '^DISAGREE  RenderCamera .*\.cameraId: accessor' build/dart-agree-acc-nc/log
	@echo "negative control: an accessor four bytes off turns the Dart agreement gate RED on RenderCamera.cameraId and on nothing else"

# THE SLOT control moves one pointer slot's offset eight in the cook emitter's
# DESCRIPTOR (cook.go): TreeNode.left is described at the slot after its own,
# while the accessor still reads where it always did.
.PHONY: tables-dart-slot-negative-control
tables-dart-slot-negative-control: build/cook-fuzz/.stamp
	@rm -rf build/dart-agree-slot-nc && mkdir -p build/dart-agree-slot-nc
	@sed -e 's|g.pf("        offset: %d,\n", fl.Offset)|g.pf("        offset: %d,\n", fl.Offset+slotMoved(record, f.Name))|' \
		internal/codegen/darttable/cook.go > build/dart-agree-slot-nc/cook.go.txt
	@grep -q 'slotMoved(record, f.Name)' build/dart-agree-slot-nc/cook.go.txt || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage matched nothing - the emitter moved"; exit 1; }
	@printf '\nfunc slotMoved(record, name string) int64 {\n\tif record == "TreeNode" && name == "left" {\n\t\treturn 8\n\t}\n\treturn 0\n}\n' >> build/dart-agree-slot-nc/cook.go.txt
	@printf '{"Replace":{"%s/internal/codegen/darttable/cook.go":"%s/build/dart-agree-slot-nc/cook.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/dart-agree-slot-nc/overlay.json
	go build -overlay build/dart-agree-slot-nc/overlay.json -o build/dart-agree-slot-nc/schema ./cmd/schema
	./build/dart-agree-slot-nc/schema generate --lang dart --out build/dart-agree-slot-nc/generated/block tables/block
	./build/dart-agree-slot-nc/schema generate --lang dart --out build/dart-agree-slot-nc/generated/pointers tables/pointers
	@sed -e "s|../../build/tables-generated-dart/block/|$(CURDIR)/build/dart-agree-slot-nc/generated/block/|g" \
	     -e "s|../../build/tables-generated-dart/pointers/|$(CURDIR)/build/dart-agree-slot-nc/generated/pointers/|g" \
		test/dart-tables/agree.dart > build/dart-agree-slot-nc/agree.dart
	@if $(DART) build/dart-agree-slot-nc/agree.dart > build/dart-agree-slot-nc/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the gate stayed green with a pointer slot eight bytes off"; \
		tail -5 build/dart-agree-slot-nc/log; exit 1; \
	fi
	@grep -q '^DISAGREE  TreeNode .*\.left: accessor' build/dart-agree-slot-nc/log || \
		{ echo "NEGATIVE CONTROL FAILED: the gate went red, but not on TreeNode.left"; \
		  tail -20 build/dart-agree-slot-nc/log; exit 1; }
	@if grep '^DISAGREE' build/dart-agree-slot-nc/log | grep -v '^DISAGREE  TreeNode .*\.left: accessor'; then \
		echo "NEGATIVE CONTROL FAILED: the gate went red on a field other than the one moved"; exit 1; \
	fi
	@grep -m1 '^DISAGREE  TreeNode .*\.left: accessor' build/dart-agree-slot-nc/log
	@echo "negative control: a pointer slot eight bytes off turns the Dart agreement gate RED on TreeNode.left and on nothing else"

test-dart: tables-dart-agree tables-dart-accessor-negative-control tables-dart-slot-negative-control

# THE STRICT ANALYZER LENS (ideas roadmap group schema-per-language-lenses): a
# card-scoped gate, not a port. Its file stem has a dash, so the canonical port
# list (SCHEMA_PORTS) and the current Makefile's `schema-*` skip both leave it
# out of the wide-scalar registry. It depends on this file's generated trees;
# see the header of the included file for the corpus, the pinned SDK and the
# negative control.
include make/schema-dart-generated-strict-analyze-gate.mk

# THE DART LEG's EMITTER-SIDE CONFORMANCE NEGATIVE CONTROLS (docs/PORTING.md
# I11). Each sabotages internal/codegen/darttable through a Go overlay
# (tools/sabotage), generates every unit the driver reads afresh into
# build/conformance-dart-negative-<mode>, compiles the driver against that tree
# and requires the matrix red on exactly one surface: `walk` negates the text
# reader's bool arm (dart / json-read red, json-write and wire green); `zero`
# removes the text_holds_zero refusal (docs/SPEC-TABLES.md §16.3) and turns
# exactly the two carried json-hostile zero rows red. Nothing tracked is written.
.PHONY: conformance-negative-control-dart-walk conformance-negative-control-dart-zero
conformance-negative-control-dart-walk: build/conformance-harness
	sh test/conformance/dart/negative-control walk "$(DART)"

conformance-negative-control-dart-zero: build/conformance-harness
	sh test/conformance/dart/negative-control zero "$(DART)"

# THE FRAMING SURFACE'S (docs/LAYERS/1-framing.md F6): the 4096-entry id-table
# maximum out of the reader, and `framing` goes red on frame_entries_4097 alone.
.PHONY: conformance-negative-control-dart-framing
conformance-negative-control-dart-framing: build/conformance-harness
	sh test/conformance/dart/negative-control framing "$(DART)"

# THE SHARED-NODE LABEL'S (M14): _writePtr writes every node in place, so the
# &node label is gone and `json-write` goes red on graph_shared alone.
.PHONY: conformance-negative-control-dart-share
conformance-negative-control-dart-share: build/conformance-harness
	sh test/conformance/dart/negative-control share "$(DART)"

# PER-CASE ABSENT (I4): the driver's `<case>.absent` writes renamed away, so
# the rows the leg cannot carry go red as "wrote nothing" and no other row does.
.PHONY: conformance-negative-control-dart-absent
conformance-negative-control-dart-absent: build/conformance-harness build/conformance-dart
	sh test/conformance/dart/negative-control absent "$(DART)"

# THE BLOCK FORM'S OWN (I11): the pitch check out of the block Open, the forged
# image block_pitch open, `forgery` red on that row alone.
.PHONY: conformance-negative-control-dart-block
conformance-negative-control-dart-block: build/conformance-harness
	sh test/conformance/dart/negative-control block "$(DART)"

test-dart: conformance-negative-control-dart-block

test-dart: conformance-negative-control-dart-walk conformance-negative-control-dart-zero conformance-negative-control-dart-share conformance-negative-control-dart-absent conformance-negative-control-dart-framing

# I12 (docs/PORTING.md) — THE DOCUMENTED SURFACE COMPILES AND RUNS, Dart side:
# docs/USAGE.md's dart-table-usage fragment, generated against tables/pointers
# afresh, analyzed with infos fatal and run over the graph_tree corpus wire
# (test/dart-tables/usage). Its control renames the page's entry point in the
# generated library and requires the gate red, naming it; the control runs the
# gate first, so the pull-request negative-control row carries both halves.
.PHONY: tables-dart-usage tables-dart-usage-negative-control
tables-dart-usage: bin/schema docs/USAGE.md
	sh test/dart-tables/usage "$(DART)"

tables-dart-usage-negative-control: tables-dart-usage
	sh test/dart-tables/usage "$(DART)" control

test-dart: tables-dart-usage tables-dart-usage-negative-control

# I15 (docs/PORTING.md) — THE TOLERANT WIRE'S DIFFERENTIAL FUZZER, Dart side.
# The harness owns the mutators and the oracle (docs/SPEC-TABLES.md §4.2); this
# leg is the thin reader test/conformance/dart/main.dart's `wire-fuzz` mode:
# every file root the wire surface carries, bound by (unit, root) in fuzzRoot,
# read on a pipe through the generated tolerant Load and written back through
# Save, and the form-2 roots of the message arm through the message codecs. A
# Save the value cannot honour (a cycle, a blob two slots name) is the
# StateError the driver answers as a refusal. The default N and SEED are the
# Makefile's. The driver also binds LoadRetain/SaveRetain for every root
# (`wire-fuzz --retain`, docs/SPEC-TABLES.md §6.6), but that arm is NOT gated:
# the Dart leg carries no retain surface yet and the arm is red at its first
# graph_tree mutant (docs/PORTING.md I15).
.PHONY: tables-dart-wire-fuzz tables-dart-wire-fuzz-negative-control
tables-dart-wire-fuzz: build/conformance-harness build/conformance-dart
	./build/conformance-harness wire-fuzz --finite-owed tables-finite/dart --driver 'build/conformance-dart wire-fuzz' --seed $(SEED) --n $(N)

# Its control: the emitter's counted-list `widened` count is removed
# (tools/sabotage table-dart-wire-widen), every unit the driver reads is
# regenerated from that compiler and the driver rebuilt on the copy, and the
# fuzzer must go red on the report comparison, at seed 1 with no random pass.
tables-dart-wire-fuzz-negative-control: build/conformance-harness
	sh test/conformance/dart/negative-control wire "$(DART)"

test-dart: tables-dart-wire-fuzz tables-dart-wire-fuzz-negative-control

# DAMAGE DISCARDS THE WHOLE LOAD on the Dart leg (docs/SPEC-TABLES.md §4):
# damage inside a nested body makes WireLoad hand back the type's defaults
# (fixed and variable roots), WireLoadRetain hand back defaults with the
# retention store empty, and LoadMessages answer no body with every body at
# its defaults. The control removes each discard guard in turn from a COPY of
# its emitter, reached through a Go build overlay, and requires the gate to go
# red on exactly that read's line. Nothing tracked is written to.
DART_DISCARD_NC := build/dart-discard-nc
.PHONY: tables-dart-discard tables-dart-discard-negative-control
tables-dart-discard:
	DART=$(DART) go test ./internal/codegen/darttable/ -count=1 -timeout 600s -run '^TestDamageDiscardsTheWholeLoadDart$$'
	@echo 'tables Dart discard: every damaged load hands back nothing it decoded'
tables-dart-discard-negative-control:
	@rm -rf $(DART_DISCARD_NC) && mkdir -p $(DART_DISCARD_NC)
	@set -e; for c in \
		'wireform.go|\tif (r.report.malformed) return %s();|\tif (false) return %s(); // NEGATIVE CONTROL: the discard is gone|fixed: WireLoad handed back' \
		'retainwire.go|\tif (r.report.malformed) {|\tif (false) { // NEGATIVE CONTROL: the retain discard is gone|retain: WireLoadRetain handed back' \
		'messageform_batch.go|  if (!tableMessageBatchClose(r)) {\n|  if (!tableMessageBatchClose(r) && false) { // NEGATIVE CONTROL: the batch discard is gone\n|messages: LoadMessages'; do \
		file=$${c%%|*}; rest=$${c#*|}; from=$${rest%%|*}; rest=$${rest#*|}; to=$${rest%%|*}; want=$${rest#*|}; \
		d=$(DART_DISCARD_NC)/$${file%.go}; mkdir -p $$d; \
		FROM="$$from" TO="$$to" perl -pe 'BEGIN{$$f=$$ENV{FROM};$$t=$$ENV{TO}} s/\Q$$f\E/$$t/' \
			internal/codegen/darttable/$$file > $$d/$$file.txt; \
		if cmp -s internal/codegen/darttable/$$file $$d/$$file.txt; then echo "NEGATIVE CONTROL: the $$file sabotage patched nothing"; exit 1; fi; \
		printf '{"Replace":{"%s/internal/codegen/darttable/%s":"%s/%s/%s.txt"}}\n' "$(CURDIR)" "$$file" "$(CURDIR)" "$$d" "$$file" > $$d/overlay.json; \
		if DART=$(DART) go test -count=1 -overlay $$d/overlay.json -run '^TestDamageDiscardsTheWholeLoadDart$$' \
				./internal/codegen/darttable > $$d/log 2>&1; then \
			echo "NEGATIVE CONTROL FAILED: the $$file discard is gone and the gate stayed green"; cat $$d/log; exit 1; \
		fi; \
		grep -q "$$want" $$d/log || { echo "NEGATIVE CONTROL FAILED: the gate went red for some other reason ($$file)"; cat $$d/log; exit 1; }; \
		grep -m1 "$$want" $$d/log; \
	done
	@echo 'negative control: removing each Dart discard (load, retain, message batch) turns the whole-load gate RED'

test-dart: tables-dart-discard tables-dart-discard-negative-control
