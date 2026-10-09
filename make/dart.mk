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

# THE ACCESSOR/DESCRIPTOR AGREEMENT GATE (docs/PORTING.md, schema#421; J1). The
# generated ACCESSORS and the generated DESCRIPTORS are two independent
# derivations of one layout, and a reading tier that only walks the descriptors
# (the fuzzer above) could read them twice and never know. This reads both ways
# and requires agreement, per field, on a PLANTED fresh copy so a byte comparison
# on a live fixture cannot pass by luck. A scalar additionally gets a straight
# two-way value read; a pointer slot is held on its OFFSET only, because the
# emitter's pointer accessor reads one unsigned byte where the descriptor names
# eight signed (block.go:360-362, block.go:413-437).
.PHONY: tables-dart-accessor-descriptor-agreement
tables-dart-accessor-descriptor-agreement: build/tables-generated-dart/.stamp build/cook-fuzz/.stamp
	$(DART) test/dart-tables/accessor_descriptor.dart

# Its NEGATIVE CONTROLS. The scalar half moves one generated block accessor four
# bytes, and the pointer half moves one cooked pointer SLOT's descriptor offset
# eight bytes; each must turn the gate red on its own message family, or the
# accessor half could be reading the descriptors twice and nobody would know.
.PHONY: tables-dart-accessor-negative-control
tables-dart-accessor-negative-control: build/tables-generated-dart/.stamp build/cook-fuzz/.stamp
	@rm -rf build/dart-accessor-nc && mkdir -p build/dart-accessor-nc
	@sed 's|g.readAt(f, fmt.Sprintf("%s + %d", origin, fl.Offset)))|g.readAt(f, fmt.Sprintf("%s + %d", origin, fl.Offset+4))) // SABOTAGED|' \
		internal/codegen/darttable/block.go > build/dart-accessor-nc/block.go.txt
	@cmp -s internal/codegen/darttable/block.go build/dart-accessor-nc/block.go.txt && \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage matched nothing — the emitter moved"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/codegen/darttable/block.go":"%s/build/dart-accessor-nc/block.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/dart-accessor-nc/overlay.json
	go build -overlay build/dart-accessor-nc/overlay.json -o build/dart-accessor-nc/schema ./cmd/schema
	./build/dart-accessor-nc/schema generate --lang dart --out build/dart-accessor-nc/generated/block tables/block
	./build/dart-accessor-nc/schema generate --lang dart --out build/dart-accessor-nc/generated/pointers tables/pointers
	@sed -e "s|../../build/tables-generated-dart/block/|$(CURDIR)/build/dart-accessor-nc/generated/block/|g" \
	     -e "s|../../build/tables-generated-dart/pointers/|$(CURDIR)/build/dart-accessor-nc/generated/pointers/|g" \
		test/dart-tables/accessor_descriptor.dart > build/dart-accessor-nc/accessor_descriptor.dart
	@if $(DART) build/dart-accessor-nc/accessor_descriptor.dart > build/dart-accessor-nc/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a generated accessor four bytes off left the Dart leg green"; \
		cat build/dart-accessor-nc/log; exit 1; \
	fi
	@grep -q "the accessor and the descriptor disagree" build/dart-accessor-nc/log || \
		{ echo "NEGATIVE CONTROL FAILED: the leg went red, but not on the accessor/descriptor disagreement"; \
		  cat build/dart-accessor-nc/log; exit 1; }
	@grep -m1 "the accessor and the descriptor disagree" build/dart-accessor-nc/log
	@echo "negative control: one generated accessor four bytes off turns the Dart leg RED on the accessor/descriptor agreement"

.PHONY: tables-dart-slot-negative-control
tables-dart-slot-negative-control: build/tables-generated-dart/.stamp build/cook-fuzz/.stamp
	@rm -rf build/dart-slot-nc && mkdir -p build/dart-slot-nc
	@sed 's|g.pf("        offset: %d,\\n", fl.Offset)|g.pf("        offset: %d,\\n", func() int64 { if f.Type.Pointer { return fl.Offset + 8 }; return fl.Offset }()) // SABOTAGED|' \
		internal/codegen/darttable/cook.go > build/dart-slot-nc/cook.go.txt
	@cmp -s internal/codegen/darttable/cook.go build/dart-slot-nc/cook.go.txt && \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage matched nothing — the emitter moved"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/codegen/darttable/cook.go":"%s/build/dart-slot-nc/cook.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/dart-slot-nc/overlay.json
	go build -overlay build/dart-slot-nc/overlay.json -o build/dart-slot-nc/schema ./cmd/schema
	./build/dart-slot-nc/schema generate --lang dart --out build/dart-slot-nc/generated/block tables/block
	./build/dart-slot-nc/schema generate --lang dart --out build/dart-slot-nc/generated/pointers tables/pointers
	@sed -e "s|../../build/tables-generated-dart/block/|$(CURDIR)/build/dart-slot-nc/generated/block/|g" \
	     -e "s|../../build/tables-generated-dart/pointers/|$(CURDIR)/build/dart-slot-nc/generated/pointers/|g" \
		test/dart-tables/accessor_descriptor.dart > build/dart-slot-nc/accessor_descriptor.dart
	@if $(DART) build/dart-slot-nc/accessor_descriptor.dart > build/dart-slot-nc/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a pointer slot eight bytes off left the Dart leg green"; \
		cat build/dart-slot-nc/log; exit 1; \
	fi
	@grep -q "the slot accessor's offset is not the descriptor's" build/dart-slot-nc/log || \
		{ echo "NEGATIVE CONTROL FAILED: the leg went red, but not on the pointer slot"; \
		  cat build/dart-slot-nc/log; exit 1; }
	@grep -m1 "the slot accessor's offset is not the descriptor's" build/dart-slot-nc/log
	@echo "negative control: a pointer slot eight bytes off turns the Dart leg RED on the cooked slot's offset"

# THE RUNTIME-HOME GATE, Dart (docs/PORTING.md §J2). A unit's shared block and
# cook runtimes live in one file each named by the PACKAGE — <Package>Block.dart
# and <Package>Cook.dart — never by the file that happens to sort first, so a
# schema file that sorts earlier relocates nothing and the two runtimes are
# byte-identical across the two trees. Dart emits no runtime-home marker comment
# (cstable.go and jstable.go carry one; darttable does not), so the home is
# identified by its own declaration at column zero, which nothing else in the
# unit declares: ^const int tableBlockMagic / ^const int tableCookMagic. And
# because a Dart library is a file, the siblings NAME the home in an import
# line — a moved home rewrites import lines across the unit — so the gate also
# asserts the package-named library is the one the unit's other libraries
# actually import.
.PHONY: tables-dart-runtime-home
tables-dart-runtime-home: bin/schema
	@rm -rf build/runtime-home-dart && mkdir -p build/runtime-home-dart/src
	@cp tables/examples/*.schema build/runtime-home-dart/src/
	@printf 'package tabledemo\n\ntable AaaRow\n{\n    tag uint8\n}\n' > build/runtime-home-dart/src/Aaa.schema
	@./bin/schema generate --lang dart --out build/runtime-home-dart/base tables/examples
	@./bin/schema generate --lang dart --out build/runtime-home-dart/added build/runtime-home-dart/src
	@for surface in Block Cook; do \
		base=$$(cd build/runtime-home-dart/base && grep -l "^const int table$${surface}Magic" *$$surface.dart); \
		added=$$(cd build/runtime-home-dart/added && grep -l "^const int table$${surface}Magic" *$$surface.dart); \
		if [ "$$base" != "Tabledemo$$surface.dart" ] || [ "$$added" != "Tabledemo$$surface.dart" ]; then \
			echo "RUNTIME HOME GATE FAILED: the $$surface runtime is in $$base before the added file and $$added after — expected Tabledemo$$surface.dart both times"; exit 1; \
		fi; \
	done
	@for tree in base added; do \
		importers=$$(cd build/runtime-home-dart/$$tree && grep -l "^import 'TabledemoBlock.dart';" *Block.dart); \
		if [ -z "$$importers" ]; then \
			echo "RUNTIME HOME GATE FAILED: no sibling imports the block home in the $$tree tree"; \
			echo "the import lines that were found:"; \
			grep -h "^import '" build/runtime-home-dart/$$tree/*Block.dart | sort -u; exit 1; \
		fi; \
	done
	@grep -v "tableBuildVersion" build/runtime-home-dart/base/TabledemoBlock.dart > build/runtime-home-dart/base.strip
	@grep -v "tableBuildVersion" build/runtime-home-dart/added/TabledemoBlock.dart > build/runtime-home-dart/added.strip
	@cmp -s build/runtime-home-dart/base.strip build/runtime-home-dart/added.strip || \
		{ echo "RUNTIME HOME GATE FAILED: the Dart block runtime's bytes moved when the unit gained a file"; exit 1; }
	@cmp -s build/runtime-home-dart/base/TabledemoCook.dart build/runtime-home-dart/added/TabledemoCook.dart || \
		{ echo "RUNTIME HOME GATE FAILED: the Dart cook runtime's bytes moved when the unit gained a file"; exit 1; }
	@echo "runtime home gate (Dart): the block and cook runtimes stay in <Package><Surface>.dart, the siblings import <Package>Block.dart, and the bytes are identical when an earlier-sorting file joins the unit"

# THE RUNTIME-HOME DRIVER, Dart. One new source under test/dart-tables resolves
# <Package>{Block,Cook}.dart by NAME and reads the runtime's own constants back —
# the half a grep cannot make: under the file-order rule the package-named
# library is not emitted at all, so the import does not resolve and the program
# does not run.
.PHONY: tables-dart-runtime-home-driver
tables-dart-runtime-home-driver: tables-dart-runtime-home
	$(DART) analyze test/dart-tables/runtime_home.dart
	$(DART) test/dart-tables/runtime_home.dart

# THE RUNTIME-HOME GATE'S NEGATIVE CONTROL, Dart. The rule is put back to the
# file order — home := ir.ProtocolIdHome(u) — in a COPY of darttable, the two
# trees are regenerated from the sabotaged compiler, and the gate must go red on
# both the name (the home MOVES when the earlier-sorting file joins) and the
# driver (the package-named library the driver imports is no longer emitted).
.PHONY: tables-dart-runtime-home-negative-control
tables-dart-runtime-home-negative-control: bin/schema tables-dart-runtime-home
	@rm -rf build/dart-runtime-home-nc && mkdir -p build/dart-runtime-home-nc
	@sed 's|home := capitalize(u.Package)|home := ir.ProtocolIdHome(u) // SABOTAGED: back to the file order|' \
		internal/codegen/darttable/darttable.go > build/dart-runtime-home-nc/darttable.go.txt
	@cmp -s internal/codegen/darttable/darttable.go build/dart-runtime-home-nc/darttable.go.txt && \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage matched nothing — the emitter moved"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/codegen/darttable/darttable.go":"%s/build/dart-runtime-home-nc/darttable.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/dart-runtime-home-nc/overlay.json
	go build -overlay build/dart-runtime-home-nc/overlay.json -o build/dart-runtime-home-nc/schema ./cmd/schema
	@rm -rf build/runtime-home-dart/base-sabotage build/runtime-home-dart/added-sabotage
	@./build/dart-runtime-home-nc/schema generate --lang dart --out build/runtime-home-dart/base-sabotage tables/examples
	@./build/dart-runtime-home-nc/schema generate --lang dart --out build/runtime-home-dart/added-sabotage build/runtime-home-dart/src
	@base=$$(cd build/runtime-home-dart/base-sabotage && grep -l "^const int tableBlockMagic" *Block.dart); \
	 added=$$(cd build/runtime-home-dart/added-sabotage && grep -l "^const int tableBlockMagic" *Block.dart); \
	 if [ "$$base" = "$$added" ]; then \
		echo "NEGATIVE CONTROL FAILED: the file-order rule kept the runtime in $$base — the gate is watching nothing"; exit 1; \
	 fi
	@sed -e "s|../../build/runtime-home-dart/base/|$(CURDIR)/build/runtime-home-dart/base-sabotage/|g" \
		test/dart-tables/runtime_home.dart > build/dart-runtime-home-nc/runtime_home.dart
	@if $(DART) build/dart-runtime-home-nc/runtime_home.dart > build/dart-runtime-home-nc/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the driver stayed green with the home named off the file order"; \
		tail -5 build/dart-runtime-home-nc/log; exit 1; \
	fi
	@grep -q "TabledemoBlock.dart" build/dart-runtime-home-nc/log || \
		{ echo "NEGATIVE CONTROL FAILED: the driver went red for some other reason"; \
		  tail -20 build/dart-runtime-home-nc/log; exit 1; }
	@grep -m1 "TabledemoBlock.dart" build/dart-runtime-home-nc/log
	@base=$$(cd build/runtime-home-dart/base-sabotage && grep -l "^const int tableBlockMagic" *Block.dart); \
	 added=$$(cd build/runtime-home-dart/added-sabotage && grep -l "^const int tableBlockMagic" *Block.dart); \
	 echo "negative control: the file-order rule moves the Dart runtime from $$base to $$added and the driver's import no longer resolves"

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
	$(MAKE) tables-dart-accessor-negative-control
	$(MAKE) tables-dart-slot-negative-control

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
# BOTH HALVES OF THE EXTENT BOUND are removed from the block Open — the rows
# against the caller's extent and the padding check behind it — in a COPY of
# the emitter, the corpus is regenerated from the sabotaged compiler, and the
# oracle must find it. Removing the rows bound ALONE leaves the reader correct:
# the padding check downstream computes `extent - used` and refuses on the
# negative slack, which is why the control names two clauses rather than one,
# and why it removes the whole layer rather than the count's declared-maximum
# check beside it. No tracked file is written to, so an interrupt cannot leave
# a sabotaged working tree.
.PHONY: tables-dart-fuzz-negative-control tables-dart-release
tables-dart-fuzz-negative-control: build/cook-fuzz/.stamp
	@rm -rf build/dart-fuzz-nc && mkdir -p build/dart-fuzz-nc
	@sed -e 's|if (rows > extent - offsetOf) {|if (false) { // SABOTAGED: the rows bound is gone|' \
	     -e 's|if (padding > extent - used) {|if (false) { // SABOTAGED: the padding bound is gone|' \
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
		echo "NEGATIVE CONTROL FAILED: the fuzzer stayed green with both extent bounds removed"; \
		tail -5 build/dart-fuzz-nc/log; exit 1; \
	fi
	@grep -q "past the claimed" build/dart-fuzz-nc/log || \
		{ echo "NEGATIVE CONTROL FAILED: the fuzzer went red for some other reason"; \
		  tail -20 build/dart-fuzz-nc/log; exit 1; }
	@grep -m1 "past the claimed" build/dart-fuzz-nc/log
	@echo "negative control: both halves of the Dart block Open's extent bound removed turns the fuzzer RED"

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
	$(MAKE) tables-dart-accessor-descriptor-agreement
	$(MAKE) tables-dart-accessor-negative-control
	$(MAKE) tables-dart-slot-negative-control
	$(MAKE) tables-dart-runtime-home
	$(MAKE) tables-dart-runtime-home-driver
	$(MAKE) tables-dart-runtime-home-negative-control
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

# THE DART ALLOCATION GATE's RUNTIME PIN (docs/PORTING.md §I14, schema#420). The
# gate reads the running SDK and REFUSES to certify on any other than the pin,
# which it reads from test/conformance/dart/ci.json — the same row CI builds its
# matrix from, so the gate cannot drift from the SDK CI installs.
# SCHEMA_DART_ALLOC_ANY_DART=1 reports without certifying.
# THERE IS NO ALLOCATION MEASUREMENT ON THIS LEG YET (schema#420): this target
# holds the refusal only, takes no generated code and costs milliseconds, which
# is why it rides the per-commit leg.
.PHONY: tables-dart-allocator-runtime tables-dart-allocator-runtime-negative-control
tables-dart-allocator-runtime:
	$(DART) analyze test/dart-tables/allocator_runtime_pin.dart
	$(DART) format --set-exit-if-changed --output=none test/dart-tables/allocator_runtime_pin.dart
	$(DART) test/dart-tables/allocator_runtime_pin.dart

# ITS NEGATIVE CONTROL, on the same three moves as the go one
# (test/conformance/go/ownership-negative-control:23-28): run the gate under a
# DIFFERENT runtime, FAIL IF THE RUN SUCCEEDS, and grep the exact refusal back
# out of the log. Dart has no GOTOOLCHAIN, so the off-pin runtime is simulated
# with SCHEMA_DART_ALLOC_VERSION; that seam can only ever force a refusal,
# because an override equal to the pin is itself refused.
tables-dart-allocator-runtime-negative-control:
	@mkdir -p build/dart-allocator-runtime-nc
	@if SCHEMA_DART_ALLOC_VERSION=3.14.1 SCHEMA_DART_ALLOC_ANY_DART=0 \
			$(DART) test/dart-tables/allocator_runtime_pin.dart \
			> build/dart-allocator-runtime-nc/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the gate certified an off-pin runtime"; \
		cat build/dart-allocator-runtime-nc/log; exit 1; \
	fi
	@grep -Fq "allocation certification requires dart 3.13.2, running dart 3.14.1" \
		build/dart-allocator-runtime-nc/log || \
		{ echo "NEGATIVE CONTROL FAILED: the gate went red for some other reason"; \
		  cat build/dart-allocator-runtime-nc/log; exit 1; }
	@grep -m1 -F "allocation certification requires dart" build/dart-allocator-runtime-nc/log
	@echo "negative control: Dart allocation certification refuses SDK 3.14.1"

test-dart: tables-dart-allocator-runtime tables-dart-allocator-runtime-negative-control
