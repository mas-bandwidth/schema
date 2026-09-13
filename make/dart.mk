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

 
# The same corpus through the DART table backend (docs/SPEC-TABLES.md): the tables
# corpus plus the evolution pair, generated at build time into build/ —
# test-only, never part of the committed generated/ tree. The full unit is
# generated (packet .dart + <Base>Block.dart + <Base>Cook.dart); Dart emits no
# table wire (the previous-form port was removed; schema#514 brings the
# id-table form), so only the two accelerators ride here.
build/tables-generated-dart/.stamp: bin/schema $(SCHEMAS_TABLES) $(SCHEMAS_TABLES_POINTERS) $(SCHEMAS_TABLES_BLOCK) test/tables/V1.schema test/tables/V2.schema test/tables/P1.schema test/tables/P3.schema
	@mkdir -p build/tables-generated-dart
	./bin/schema generate --lang dart --out build/tables-generated-dart/examples tables/examples
	# the POINTERED unit: its cook readers are the reason it is here.
	./bin/schema generate --lang dart --out build/tables-generated-dart/pointers tables/pointers
	./bin/schema generate --lang dart --out build/tables-generated-dart/block tables/block
	./bin/schema generate --lang dart --out build/tables-generated-dart/blockhome tables/blockhome
	./bin/schema generate --lang dart --out build/tables-generated-dart/v1 test/tables/V1.schema
	./bin/schema generate --lang dart --out build/tables-generated-dart/v2 test/tables/V2.schema
	./bin/schema generate --lang dart --out build/tables-generated-dart/p1 test/tables/P1.schema
	./bin/schema generate --lang dart --out build/tables-generated-dart/p3 test/tables/P3.schema
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
	@sed 's|h.WriteString("import '"'"'dart:typed_data'"'"';\\n\\n")|h.WriteString("import '"'"'dart:typed_data'"'"';\\nimport '"'"'package:collection/collection.dart'"'"'; // SABOTAGED\\n\\n")|' \
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

# THE DART PORT'S RELEASE GATE. certify.yml DERIVES this target by name, so a
# port lands its expensive half by adding the target and nothing else: two
# hundred and eighty thousand forgery-fuzz mutants over the block and cook
# readers under two seeds (20,000 per fixture, seven fixtures, two seeds), and
# the three planted controls. The fuzz is measured in minutes and answers a
# question about the runtime under hostile input rather than about the diff,
# which is what makes it a certification instrument and not an iteration one.
.PHONY: tables-dart-release
tables-dart-release:
	$(MAKE) tables-dart-fuzz SEED=1 DART_FUZZ_MUTANTS=20000
	$(MAKE) tables-dart-fuzz SEED=2 DART_FUZZ_MUTANTS=20000
	$(MAKE) tables-dart-fuzz-negative-control
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
# sabotaged compiler, and the oracle must find it. No tracked file is written
# to, so an interrupt cannot leave a sabotaged working tree.
.PHONY: tables-dart-fuzz-negative-control tables-dart-release
tables-dart-fuzz-negative-control: build/cook-fuzz/.stamp
	@rm -rf build/dart-fuzz-nc && mkdir -p build/dart-fuzz-nc
	@sed 's|g.pf("      if (count > %sMax) {\\n        return null;\\n      }\\n", field)|_ = field // SABOTAGED: the count bound is gone|' \
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
	@if $(DART) build/dart-fuzz-nc/fuzz.dart 4000 > build/dart-fuzz-nc/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the fuzzer stayed green with the count bound removed"; \
		tail -5 build/dart-fuzz-nc/log; exit 1; \
	fi
	@grep -q "past the declared" build/dart-fuzz-nc/log || \
		{ echo "NEGATIVE CONTROL FAILED: the fuzzer went red for some other reason"; \
		  tail -20 build/dart-fuzz-nc/log; exit 1; }
	@grep -m1 "past the declared" build/dart-fuzz-nc/log
	@echo "negative control: one check removed from the Dart block Open turns the fuzzer RED"

# The DART leg's driver: one AOT executable, because `dart run` would pay a JIT
# start-up per surface and the two-minute rule is measured across every leg.
build/conformance-dart: build/tables-generated-dart/.stamp test/conformance/dart/main.dart
	@mkdir -p build
	$(DART) compile exe -o $@ test/conformance/dart/main.dart >/dev/null

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
	@touch $@

.PHONY: tables-dart-fixed-form
tables-dart-fixed-form: build/dart-fixed/.stamp build/fixedform-corpus/.stamp build/fixedform-bench-corpus/.stamp
	$(DART) analyze build/dart-fixed test/dart-tables/fixedform.dart
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
	@sed 's|val + "." + name + "Length", "Endian.little"}, ";")|val + "." + name + "Length + 1", "Endian.little"}, ";") // SABOTAGED|' \
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
