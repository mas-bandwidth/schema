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

 
# The same corpus through the DART table backend (docs/SPEC-TABLES.md): the tables
# corpus plus the evolution pair, generated at build time into build/ —
# test-only, never part of the committed generated/ tree. The full unit is
# generated (packet .dart + <Base>Block.dart + <Base>Cook.dart, and the wire
# forms where the unit owns them). Fixed-class units own form 3. tables/pointers,
# tables/blobs and tables/lists own form 1 beside it (the variable class).
build/tables-generated-dart/.stamp: bin/schema $(SCHEMAS_TABLES) $(SCHEMAS_TABLES_POINTERS) $(SCHEMAS_TABLES_BLOCK) $(SCHEMAS_TABLES_BLOBS) $(SCHEMAS_TABLES_LISTS) test/tables/V1.schema test/tables/V2.schema test/tables/P1.schema test/tables/P3.schema
	@mkdir -p build/tables-generated-dart
	./bin/schema generate --lang dart --out build/tables-generated-dart/examples tables/examples
	# the POINTERED unit: form 1 for Scene, form 3 and cook beside it.
	./bin/schema generate --lang dart --out build/tables-generated-dart/pointers tables/pointers
	./bin/schema generate --lang dart --out build/tables-generated-dart/blobs tables/blobs
	./bin/schema generate --lang dart --out build/tables-generated-dart/lists tables/lists
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
# the three planted controls. The fuzz is measured in minutes and answers a
# question about the runtime under hostile input rather than about the diff,
# which is what makes it a certification instrument and not an iteration one.
.PHONY: tables-dart-release
tables-dart-release:
	$(MAKE) tables-dart-fuzz SEED=1 DART_FUZZ_MUTANTS=20000
	$(MAKE) tables-dart-fuzz SEED=2 DART_FUZZ_MUTANTS=20000
	$(MAKE) tables-dart-fuzz-negative-control
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
	@sed -e 's|g.pf("      if (rows > extent - offsetOf) {\\n        return null;\\n      }\\n")|_ = field // SABOTAGED: the rows-inside-extent bound is gone|' \
	     -e 's|g.pf("    if (padding > extent - used) {\\n      return null;\\n    }\\n")|// SABOTAGED: the padding bound is gone|' \
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
# block Open in a copy, and the enumerated lead pass must go red — every middle
# lead 1..63 opens when the check that should refuse them is gone (I5).
.PHONY: tables-dart-fuzz-lead-negative-control
tables-dart-fuzz-lead-negative-control: build/cook-fuzz/.stamp
	@rm -rf build/dart-fuzz-lead-nc && mkdir -p build/dart-fuzz-lead-nc
	@sed -e 's|g.pf("    if (base %% %d != 0) {\\n      return null; // the base'"'"'s alignment\\n    }\\n", ir.BlockAlign)|// SABOTAGED: the base-alignment check is gone|' \
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
	@if go test -count=1 -overlay build/dart-keyed-nc/overlay.json -run TestDartKeyedArraysAreHeldAtBothEnds \
			./internal/codegen/dart/ > build/dart-keyed-nc/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: an extra keyed slot left the extent test green"; \
		cat build/dart-keyed-nc/log; exit 1; \
	fi
	@grep -q "must hold exactly the enum extent" build/dart-keyed-nc/log || \
		{ echo "NEGATIVE CONTROL FAILED: the extent test went red for some other reason"; \
		  cat build/dart-keyed-nc/log; exit 1; }
	@grep -m1 "enum extent" build/dart-keyed-nc/log
	@echo "negative control: one extra keyed slot turns the Dart keyed-array extent test RED"

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
# swallowed. THE BLADE runs first, every time: a planted run (floatjit.dart's
# `planted-exit` shape, which warms up and exits 3 with no completion line)
# and a run with the GC trace switched off must both be refused by the same
# check, each for its own reason, or the target is red with BLADE LOST.
FLOATJIT_N ?= 2000000
FLOATJIT_DIR := build/floatjit
.PHONY: tables-dart-float-jit
tables-dart-float-jit: toolchain-dart generated/dart/.stamp
	@mkdir -p $(FLOATJIT_DIR)
	@run() { \
		log=$(FLOATJIT_DIR)/$$1-$$2$$4; rm -f $$log.count; \
		$(DART) $$3 test/dart/floatjit.dart $$1 $$2 >$$log.out 2>$$log.err; rc=$$?; \
		if [ $$rc -ne 0 ]; then echo "tables-dart-float-jit: REFUSED shape=$$1 passes=$$2: dart exited $$rc ($$log.err)"; return 1; fi; \
		if ! grep -Eqx "floatjit shape=$$1 steady=$$2 check=-?[0-9]+" $$log.out; then echo "tables-dart-float-jit: REFUSED shape=$$1 passes=$$2: no completion line ($$log.out)"; return 1; fi; \
		if ! grep -Eq '^main: .*reason=initial' $$log.err; then echo "tables-dart-float-jit: REFUSED shape=$$1 passes=$$2: no GC trace from the main isolate, --verbose_gc absent or unsupported ($$log.err)"; return 1; fi; \
		if [ "$$(grep -Ec '^floatjit: steady window (begins|ends)$$' $$log.err)" != 2 ]; then echo "tables-dart-float-jit: REFUSED shape=$$1 passes=$$2: the steady window markers are not both present ($$log.err)"; return 1; fi; \
		awk '/^floatjit: steady window begins$$/ { w = 1; next } /^floatjit: steady window ends$$/ { w = 0 } w && /^\[ main *, *Scavenge/ { n++ } END { print n + 0 }' $$log.err > $$log.count; \
	}; \
	if run planted-exit 1 --verbose_gc -blade >$(FLOATJIT_DIR)/blade-exit.log; then \
		echo "tables-dart-float-jit: BLADE LOST: a planted run that exits 3 was accepted"; exit 1; fi; \
	grep -q 'REFUSED shape=planted-exit passes=1: dart exited 3 ' $(FLOATJIT_DIR)/blade-exit.log || \
		{ echo "tables-dart-float-jit: BLADE LOST: the planted run was refused for another reason"; cat $(FLOATJIT_DIR)/blade-exit.log; exit 1; }; \
	if run generated 1 '' -blade >$(FLOATJIT_DIR)/blade-trace.log; then \
		echo "tables-dart-float-jit: BLADE LOST: a run with no GC trace was accepted"; exit 1; fi; \
	grep -q 'REFUSED shape=generated passes=1: no GC trace' $(FLOATJIT_DIR)/blade-trace.log || \
		{ echo "tables-dart-float-jit: BLADE LOST: the untraced run was refused for another reason"; cat $(FLOATJIT_DIR)/blade-trace.log; exit 1; }; \
	echo "tables-dart-float-jit blade: a planted exit-3 run and an untraced run are both refused"; \
	fail=0; for s in generated inlined twocallers; do \
		run $$s $(FLOATJIT_N) --verbose_gc '' || exit 1; \
		run $$s $$((2 * $(FLOATJIT_N))) --verbose_gc '' || exit 1; \
		a=$$(cat $(FLOATJIT_DIR)/$$s-$(FLOATJIT_N).count); \
		b=$$(cat $(FLOATJIT_DIR)/$$s-$$((2 * $(FLOATJIT_N))).count); \
		echo "tables-dart-float-jit shape=$$s steady_scavenges=$$a per $(FLOATJIT_N) passes ($$b per $$((2 * $(FLOATJIT_N))))"; \
		[ $$a -eq 0 ] && [ $$b -eq 0 ] || fail=1; \
	done; \
	echo "tables-dart-float-jit sdk: $$($(DART) --version 2>&1)"; \
	if [ $$fail -ne 0 ]; then echo "tables-dart-float-jit: FAIL (a float boxes under the JIT)"; exit 1; fi; \
	echo "tables-dart-float-jit: OK"

test-dart: tables-dart-float-jit

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
	@sed -e 's|g.pf("        offset: %d,\\n", fl.Offset)|g.pf("        offset: %d,\\n", fl.Offset+slotMoved(record, f.Name))|' \
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
