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
# generated (packet .dart + <Base>Table.dart), because a table's closure decodes
# into the packet emitter's own classes.
build/tables-generated-dart/.stamp: bin/schema $(SCHEMAS_TABLES) $(SCHEMAS_TABLES_POINTERS) $(SCHEMAS_TABLES_BLOCK) test/tables/V1.schema test/tables/V2.schema test/tables/P1.schema test/tables/P3.schema
	@mkdir -p build/tables-generated-dart
	./bin/schema generate --lang dart --out build/tables-generated-dart/examples tables/examples
	# the POINTERED unit: its Dart surface is refused BY NAME (§11) — the
	# reading tier has no arena, no builder and no node-table codec, so one
	# file remains and it carries the reason.
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
# emitter that drifts; this gate holds the <Base>Table.dart half of the corpus
# to what the formatter would write, and the analyzer holds it to what the
# language accepts.
.PHONY: tables-dart-clean
tables-dart-clean: build/tables-generated-dart/.stamp
	$(DART) analyze build/tables-generated-dart
	@rm -rf build/tables-dart-fmt && cp -r build/tables-generated-dart build/tables-dart-fmt
	@rm -f build/tables-dart-fmt/.stamp
	@$(DART) format build/tables-dart-fmt >/dev/null
	@for f in build/tables-generated-dart/*/*Table.dart \
		  build/tables-generated-dart/*/*Block.dart \
		  build/tables-generated-dart/*/*Cook.dart; do \
		test -e $$f || continue; \
		cmp -s $$f build/tables-dart-fmt/$${f#build/tables-generated-dart/} || \
			{ echo "dart format drift in $$f"; exit 1; }; \
	done
	@echo "tables Dart: analyzer clean and format-canonical"

# THE HARNESS'S DART CONTROL, on the EMITTER: the §16 READ walk is sabotaged
# in a copy of internal/codegen/darttable/jsonruntime.go — an enum token lands
# as the wrong variant — the compiler is rebuilt over it with `go build
# -overlay`, the Dart units are regenerated from that compiler, the driver is
# compiled against them, and the harness must go RED on `json-read` while
# `json-write` and `wire` stay GREEN: the break is the reader's and nothing
# else's. No tracked file is written to.
CONFORMANCE_NEGATIVE_DART = build/conformance-negative-dart
.PHONY: conformance-negative-control-dart
conformance-negative-control-dart:
	@echo "conformance-negative-control-dart: dormant — the surface it turns red is absent while this port writes the wire's previous form (docs/SPEC-TABLES.md §3, schema#514)"

# ---- THE DART TABLES GATES beside the conformance matrix ----
#
# THE SOAK gates on CORRECTNESS under reuse: every record round-tripped through
# the wire and through the text for the whole run, byte-compared, into storage
# reused every iteration. The allocation floor is the gate below it.
.PHONY: tables-dart-soak
tables-dart-soak:
	@echo "tables-dart-soak: dormant — the corpus it gates against is absent while this port writes the wire's previous form (docs/SPEC-TABLES.md §3, schema#514)"

DART_SOAK_SECONDS ?= 20

# THE SOAK'S NEGATIVE CONTROL. A gate that has never gone red is watching
# nothing: SOAK_SABOTAGE=1 corrupts ONE BYTE of one re-saved record, and the
# byte comparison must refuse it.
.PHONY: tables-dart-soak-negative-control
tables-dart-soak-negative-control:
	@echo "tables-dart-soak-negative-control: dormant — the surface it turns red is absent while this port writes the wire's previous form (docs/SPEC-TABLES.md §3, schema#514)"

# THE ALLOCATION GATE (test/dart-tables/gcgate.dart). The claim is that the
# wire path — loadBody, measure, saveBody through a caller-owned reader, writer
# and report — allocates NOTHING per record, and the gate is a MEASUREMENT: the
# VM's own new-space scavenge count over a steady phase, read from --verbose_gc
# between two marker lines. A loop that allocates nothing triggers no scavenge
# however long it runs, so the floor is a true zero; a planted allocation per
# record turns it red (tables-dart-alloc-negative-control). The semi-space is
# pinned at ONE MEGABYTE for the measurement, because the VM grows it on its
# own — under the JIT to 32 MB — and a plant has to fill it to be seen: with it
# pinned, one small object per record is a scavenge every ~30,000 records, so
# a plant is loud at `make test`'s DART_ALLOC_ITERATIONS=20000 (160,000
# records) and a zero is a zero at any length.
#
# THE GATE IS AOT's — `dart compile`, the language's release configuration and
# the one a shipping consumer runs — and the JIT's count is PRINTED beside it,
# not gated, because what the JIT boxes is the inliner's decision and not the
# codec's: a double crossing a conversion call the inlining budget left out of
# line is boxed, and the budget runs out at different places depending on what
# the loop around the codec looks like (one boxed double per pass of the eight
# records on the wire phase here; up to three per record with one codec inlined
# into a monomorphic caller). AOT inlines every one of them and reads zero.
#
# `text` is measured and printed, never gated: the §16 walk's floor is not zero
# and the page prices it. It runs DART_ALLOC_TEXT_ITERATIONS passes, its own
# count, because a text round trip is a hundred times a wire one — 200 passes
# is seconds, and the number is a price per record either way.
DART_ALLOC_ITERATIONS ?= 400000
DART_ALLOC_TEXT_ITERATIONS ?= 200

.PHONY: tables-dart-alloc
tables-dart-alloc:
	@echo "tables-dart-alloc: dormant — the corpus it gates against is absent while this port writes the wire's previous form (docs/SPEC-TABLES.md §3, schema#514)"

# THE ALLOCATION GATE'S NEGATIVE CONTROL: two plants, one object per record
# each — a TableReport, the class the code under test could most plausibly
# construct, and a Uint8List(8) — and the same count must go RED on both,
# under AOT and under the JIT, so the instrument is shown to see a plant in
# both builds. The plants live in gcgate.dart as phases, so the control runs
# the very instrument the gate runs. The gate holds AOT's count at exactly
# zero, so the control asks for its complement — any count at all — which a
# plant is at every length over the pinned one-megabyte semi-space: dozens of
# scavenges at the release length, a handful at `make test`'s tenth of it.
#
# The `make test` length is a tenth of the release length for the loop's sake:
# one record of the corpus is 210 KB, so a phase over it is seconds, not
# milliseconds, and ten phases run here.
.PHONY: tables-dart-alloc-negative-control
tables-dart-alloc-negative-control:
	@echo "tables-dart-alloc-negative-control: dormant — the surface it turns red is absent while this port writes the wire's previous form (docs/SPEC-TABLES.md §3, schema#514)"

# THE DART NAME-CLAIM NEGATIVE CONTROL (docs/SPEC-TABLES.md §11). Every
# library-scope spelling of the Dart table runtime is registered in
# internal/tablenames and refused as a schema declaration, and compiler's
# TestDartTableRuntimeNamesAreClaimed holds the registry to the emitted source.
# This plants an unregistered class in the runtime through `go test -overlay`
# and requires that test to go RED naming it. No tracked file is written to.
.PHONY: tables-dart-names-negative-control
tables-dart-names-negative-control:
	@rm -rf build/dart-names-nc && mkdir -p build/dart-names-nc
	@sed 's|^final class TableReport {$$|final class TableBogusUnregistered {}\n\nfinal class TableReport {|' \
		internal/codegen/darttable/runtime.go > build/dart-names-nc/runtime.go.txt
	@cmp -s internal/codegen/darttable/runtime.go build/dart-names-nc/runtime.go.txt && \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage matched nothing — the runtime moved"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/codegen/darttable/runtime.go":"%s/build/dart-names-nc/runtime.go.txt"}}\n' \
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

# THE DOCUMENTED SURFACE RUNS. docs/USAGE.md's Dart table examples are
# extracted VERBATIM from the page — every ```dart block of its table section —
# wrapped in a main() and run over the corpus, so the page goes red with the
# code. test/dart-tables/usage-prelude.dart is the wrapper's head.
.PHONY: tables-dart-usage
tables-dart-usage:
	@echo "tables-dart-usage: dormant — the corpus it gates against is absent while this port writes the wire's previous form (docs/SPEC-TABLES.md §3, schema#514)"

# THE GENERIC-WALK GATE, Dart: the §16 walker is emitted ONCE per unit, into
# <Package>Table.dart, between two marker comments — and it is the SAME walker
# in every unit, byte for byte, because a walker that varied with the unit
# would be a per-type codec wearing a walker's name. A pointered unit carries
# none, since its wire is refused (§11) and the walk is the wire's. Go's gate,
# in Dart's spelling.
.PHONY: tables-dart-json-walk
tables-dart-json-walk: build/tables-generated-dart/.stamp
	@rm -rf build/json-walk-dart && mkdir -p build/json-walk-dart
	@for d in build/tables-generated-dart/*/; do \
		unit=$$(basename $$d); n=0; \
		for f in $$d*Table.dart; do \
			[ -e "$$f" ] || continue; \
			out=build/json-walk-dart/$$unit.$$(basename $$f).txt; \
			awk '/---- json walk: begin ----/,/---- json walk: end ----/' $$f > $$out; \
			if [ -s $$out ]; then n=$$((n+1)); else rm -f $$out; fi; \
		done; \
		if [ $$n -gt 1 ]; then \
			echo "GENERIC-WALK GATE FAILED: unit $$unit carries $$n walkers, not one"; exit 1; \
		fi; \
	done
	@if [ -z "$$(ls build/json-walk-dart 2>/dev/null)" ]; then \
		echo "GENERIC-WALK GATE FAILED: no walker in any generated .dart"; exit 1; fi
	@first=""; for f in build/json-walk-dart/*; do \
		if [ -z "$$first" ]; then first=$$f; else \
			cmp -s $$first $$f || { echo "GENERIC-WALK GATE FAILED: the walker in $$f is not the walker in $$first"; exit 1; }; \
		fi; \
	done
	@echo "tables Dart generic-walk gate: one walker per unit, byte-identical across $$(ls build/json-walk-dart | wc -l | tr -d ' ') units"

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
		internal/codegen/darttable/darttable.go > build/dart-standalone-nc/darttable.go.txt
	@cmp -s internal/codegen/darttable/darttable.go build/dart-standalone-nc/darttable.go.txt && \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage matched nothing — the emitter moved"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/codegen/darttable/darttable.go":"%s/build/dart-standalone-nc/darttable.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/dart-standalone-nc/overlay.json
	go build -overlay build/dart-standalone-nc/overlay.json -o build/dart-standalone-nc/schema ./cmd/schema
	./build/dart-standalone-nc/schema generate --lang dart --out build/dart-standalone-nc/generated/examples tables/examples
	@grep -lq SABOTAGED build/dart-standalone-nc/generated/examples/*.dart || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotaged emitter emitted no package import"; exit 1; }
	@if $(MAKE) -s tables-dart-standalone STANDALONE_DART_DIR=build/dart-standalone-nc/generated > build/dart-standalone-nc/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the standalone gate stayed green with a package import planted"; exit 1; \
	fi
	@grep -q "outside the unit" build/dart-standalone-nc/log || \
		{ echo "NEGATIVE CONTROL FAILED: the gate went red for some other reason"; cat build/dart-standalone-nc/log; exit 1; }
	@grep -m1 "outside the unit" build/dart-standalone-nc/log
	@echo "negative control: one planted package import turns the Dart standalone gate RED"

# THE DART PORT'S RELEASE GATE. certify.yml DERIVES this target by name, so a
# port lands its expensive half by adding the target and nothing else: ten
# minutes of the soak — every record through the wire and the text, byte-
# compared, into storage reused for the whole run — and two hundred and eighty
# thousand fuzz mutants under two seeds (20,000 per fixture, seven fixtures,
# two seeds). Both are measured in minutes and both answer a question about
# the runtime under load rather than about the diff, which is what makes them
# certification instruments and not iteration ones.
.PHONY: tables-dart-release
tables-dart-release:
	$(MAKE) tables-dart-soak DART_SOAK_SECONDS=600
	$(MAKE) tables-dart-fuzz SEED=1 DART_FUZZ_MUTANTS=20000
	$(MAKE) tables-dart-fuzz SEED=2 DART_FUZZ_MUTANTS=20000
	$(MAKE) tables-dart-soak-negative-control DART_SOAK_SECONDS=5
	$(MAKE) tables-dart-fuzz-negative-control
	$(MAKE) tables-dart-alloc-negative-control
	$(MAKE) tables-dart-names-negative-control
	$(MAKE) tables-dart-standalone-negative-control
	$(MAKE) conformance-negative-control-dart

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
.PHONY: tables-dart-fuzz-negative-control tables-dart-release conformance-negative-control-dart
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

generated/bench/tables/dart/.stamp: bin/schema bench/corpus/BenchTable.schema
	@mkdir -p generated/bench/tables/dart
	./bin/schema generate --lang dart --out generated/bench/tables/dart bench/corpus/BenchTable.schema
	@touch $@

# The DART leg's driver: one AOT executable, because `dart run` would pay a JIT
# start-up per surface and the two-minute rule is measured across every leg.
build/conformance-dart: build/tables-generated-dart/.stamp test/conformance/dart/main.dart
	@mkdir -p build
	$(DART) compile exe -o $@ test/conformance/dart/main.dart >/dev/null

# The Dart half of the block zero-cost gate (docs/SPEC-TABLES.md): a Table
# library of a unit with no block or cook carries no symbol of either
# accelerator.
.PHONY: tables-dart-zero-cost
tables-dart-zero-cost: build/tables-generated-dart/.stamp
	@for f in build/tables-generated-dart/*/*Table.dart; do \
		if grep -nE "TableBlock|TableCook|tableBuildVersion|[A-Za-z0-9_]Block\\b" $$f; then \
			echo "BLOCK ZERO-COST GATE FAILED: an accelerator leaked into $$f"; exit 1; \
		fi; \
	done
	@echo "block zero-cost gate: no Dart Table library carries one symbol of either accelerator"

# THE DART LEG of `make test`. THE DART PORT's own instruments
# (docs/SPEC-TABLES.md): the emitted sources held to what `dart format` writes
# and what the analyzer accepts, the allocation gate at a tenth of its release
# length and its two planted controls, the name-claim control, the page's own
# example run verbatim, the forgery fuzzer, a few seconds of the soak and the
# byte its control corrupts, and the harness's own Dart control — the long
# soak and the full-length allocation gate are `make tables-dart-release` —
# then the analyzer and the formatter over every generated tree, and the
# packet tests, checked and compiled.
.PHONY: test-dart
test-dart: generated/dart/.stamp generated/dart-ludicrous/.stamp generated/bench/dart/.stamp generated/bench/tables/dart/.stamp
	$(MAKE) tables-dart-clean
	$(MAKE) tables-dart-zero-cost
	$(MAKE) tables-dart-alloc DART_ALLOC_ITERATIONS=20000
	$(MAKE) tables-dart-alloc-negative-control DART_ALLOC_ITERATIONS=20000
	$(MAKE) tables-dart-names-negative-control
	$(MAKE) tables-dart-usage
	$(MAKE) tables-dart-json-walk
	$(MAKE) tables-dart-standalone
	$(MAKE) tables-dart-standalone-negative-control
	$(MAKE) tables-dart-fuzz DART_FUZZ_MUTANTS=1500
	$(MAKE) tables-dart-fuzz-negative-control
	$(MAKE) tables-dart-soak DART_SOAK_SECONDS=3 DART_ALLOC_ITERATIONS=20000
	$(MAKE) tables-dart-soak-negative-control DART_SOAK_SECONDS=3
	$(MAKE) conformance-negative-control-dart
	$(DART) analyze generated/dart generated/dart-ludicrous generated/bench/dart test/dart test/dart-ludicrous bench/dart bench/tables/dart
	$(DART) format --set-exit-if-changed --output=none generated/dart generated/dart-ludicrous generated/bench/dart
	cd test/dart && $(DART) --enable-asserts main.dart
	@mkdir -p build
	cd test/dart && $(DART) compile exe -o ../../build/schema_test_dart main.dart >/dev/null && ../../build/schema_test_dart
	cd test/dart-ludicrous && $(DART) --enable-asserts main.dart
	cd test/dart-ludicrous && $(DART) compile exe -o ../../build/schema_test_dart_ludicrous main.dart >/dev/null && ../../build/schema_test_dart_ludicrous

TEST_LEGS         += test-dart
CONFORMANCE_LEGS  += build/conformance-dart
BENCH_TABLES_LEGS += generated/bench/tables/dart/.stamp

# THE DART NATIVE GATE (issue #547). `dart analyze` and `dart format` are the
# language's own two instruments, and this holds BOTH corpora to both of them,
# both halves before the verdict.
#
# It extends tables-dart-clean rather than replacing it: that target holds the
# tables corpus, and its format half covers the <Base>Table/Block/Cook files
# alone. This one adds the examples corpus and takes the format check over
# every generated Dart file of both.
.PHONY: native-dart
native-dart: generated/dart/.stamp generated/dart-ludicrous/.stamp build/tables-generated-dart/.stamp
	@fail=0; \
	echo "==== dart analyze"; \
	$(DART) analyze generated/dart generated/dart-ludicrous build/tables-generated-dart || fail=1; \
	echo "==== dart format"; \
	$(DART) format --set-exit-if-changed --output=none generated/dart generated/dart-ludicrous build/tables-generated-dart || fail=1; \
	if [ $$fail -ne 0 ]; then echo "native Dart: the findings above are the emitter's"; exit 1; fi; \
	echo "native Dart: analyzer clean and format-canonical over the examples and tables corpora"

NATIVE_LEGS       += native-dart
