# make/cs.mk — the C# leg (docs/CONTRIBUTING.md, "Adding a language"). Included
# by the Makefile's wildcard include; the Makefile names no language. The leg
# registers itself at the end of this file.

# the serialize.cs runtime the generated C# targets, a sibling checkout;
# test/cs/schematest.csproj and its ludicrous twin carry the same relative path
SERIALIZE_CS ?= ../serialize.cs

# The .NET SDK, pinned per project the way every other leg's toolchain is. The
# SDK VERSION lives in .github/dotnet-version, which both workflows read and
# internal/ci gates; there is no unpacked copy under dist/ because the SDK
# installs itself into the machine, so this pin names the COMMAND and the
# toolchain gate holds it to resolving. Point it at another SDK with
# DOTNET=/path/to/dotnet.
DOTNET ?= dotnet

# THE TOOLCHAIN GATE, this leg's half (issue #599; the Makefile's header and
# docs/CONTRIBUTING.md, "Adding a language"). The C# half of the block gates in
# the Makefile reads the same pin, so a dotnet that does not resolve stops the
# chain here rather than in the middle of a two-language control.
.PHONY: toolchain-cs
toolchain-cs:
	@$(call toolchain_probe,cs,DOTNET,$(DOTNET))
# Packet defaults consume the shared C++ oracle in both build modes.
build/packet-defaults/cs/.stamp: bin/schema test/packet-defaults/Defaults.schema test/packet-defaults/Plain.schema make/cs.mk
	./bin/schema generate --lang cs --out build/packet-defaults/cs/defaults test/packet-defaults/Defaults.schema
	./bin/schema generate --lang cs --out build/packet-defaults/cs/plain test/packet-defaults/Plain.schema
	@touch $@

.PHONY: packet-defaults-cs packet-defaults-cs-negative-control
packet-defaults-cs: build/packet-defaults/cs/.stamp packet-defaults-cpp
	$(DOTNET) run --project test/packet-defaults/cs/packet-defaults.csproj -- testdata/wire/packet-defaults
	$(DOTNET) run --configuration Release --project test/packet-defaults/cs/packet-defaults.csproj -- testdata/wire/packet-defaults

packet-defaults-cs-negative-control: packet-defaults-cs
	@mkdir -p build/packet-defaults/cs-negative
	go run ./tools/sabotage -name packet-defaults-cs-constructor-bytes \
		-out build/packet-defaults/cs-negative/csharp.gotext internal/codegen/csharp/csharp.go
	@printf '{"Replace":{"%s/internal/codegen/csharp/csharp.go":"%s/build/packet-defaults/cs-negative/csharp.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/packet-defaults/cs-negative/overlay.json
	go build -overlay=build/packet-defaults/cs-negative/overlay.json -o build/packet-defaults/cs-negative/schema ./cmd/schema
	./build/packet-defaults/cs-negative/schema generate --lang cs --out build/packet-defaults/cs-negative/generated test/packet-defaults/Defaults.schema
	$(DOTNET) build test/packet-defaults/cs/packet-defaults.csproj \
		-p:DefaultsDir="$(CURDIR)/build/packet-defaults/cs-negative/generated" -o build/packet-defaults/cs-negative/checker
	@if $(DOTNET) build/packet-defaults/cs-negative/checker/packet-defaults.dll testdata/wire/packet-defaults > build/packet-defaults/cs-negative/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: missing constructor bytes passed in C#'; exit 1; fi
	@grep -Fq 'FAILED: packet-default constructor bytes' build/packet-defaults/cs-negative/log || \
		{ echo 'NEGATIVE CONTROL FAILED: C# failed for another reason'; cat build/packet-defaults/cs-negative/log; exit 1; }
	@echo 'packet defaults C# negative control: missing constructor bytes fail the runtime check'

test-cs: packet-defaults-cs packet-defaults-cs-negative-control

generated/cs-ludicrous/.stamp: bin/schema $(SCHEMAS128)
	./bin/schema generate --lang cs --out generated/cs-ludicrous examples128
	@touch $@

# the C# target: generated sources only — test/cs/schematest.csproj compiles
# them beside the serialize.cs runtime via <Compile Include> items
generated/cs/.stamp: bin/schema $(SCHEMAS)
	./bin/schema generate --lang cs --out generated/cs examples
	@touch $@

# The GENERIC-WALK GATE, C# side (docs/SPEC-TABLES.md §16). The same property
# the C++ gate above holds, over the shape C# forces: a unit's files compile
# into ONE assembly, so the walk is emitted ONCE PER UNIT — into the file that
# already carries the unit's shared table runtime — rather than once per
# translation unit behind a guard. So the gate asserts two things: exactly one
# file per unit directory carries a walker, and every walker in the corpus is
# the same bytes. The package name never enters the markers, so nothing is
# normalised away here either.
.PHONY: tables-cs-json-walk
tables-cs-json-walk: build/tables-generated-cs/.stamp
	@rm -rf build/json-walk-cs && mkdir -p build/json-walk-cs
	@for d in build/tables-generated-cs/*/; do \
		unit=$$(basename $$d); n=0; \
		for f in $$d*Table.cs; do \
			[ -e "$$f" ] || continue; \
			out=build/json-walk-cs/$$unit.$$(basename $$f); \
			awk '/---- json walk: begin ----/,/---- json walk: end ----/' $$f > $$out; \
			if [ -s $$out ]; then n=$$((n+1)); else rm -f $$out; fi; \
		done; \
		if [ -n "$$(ls $$d*Table.cs 2>/dev/null)" ] && [ $$n -ne 1 ]; then \
			echo "GENERIC-WALK GATE FAILED: unit $$unit carries $$n walkers, not one"; exit 1; \
		fi; \
	done
	@if [ -z "$$(ls build/json-walk-cs 2>/dev/null)" ]; then \
		echo "GENERIC-WALK GATE FAILED: no walker in any generated .cs"; exit 1; fi
	@first=""; for f in build/json-walk-cs/*; do \
		if [ -z "$$first" ]; then first=$$f; else \
			cmp -s $$first $$f || { echo "GENERIC-WALK GATE FAILED: the walker in $$f is not the walker in $$first"; exit 1; }; \
		fi; \
	done
	@echo "tables C# generic-walk gate: one walker per unit, byte-identical across $$(ls build/json-walk-cs | wc -l | tr -d ' ') units"

build/tables-generated-cs/.stamp: bin/schema $(SCHEMAS_TABLES) $(SCHEMAS_TABLES_POINTERS) $(SCHEMAS_TABLES_BLOCK) test/tables/V1.schema test/tables/V2.schema test/tables/P1.schema test/tables/P3.schema test/tables/K1.schema test/tables/K2.schema test/tables/CsIds.schema test/tables/CsUnions.schema test/tables/CsView.schema $(SCHEMAS_TABLES_MESSAGES) test/tables/M1.schema test/tables/M2.schema test/tables/A1.schema test/tables/A2.schema tables/scalars test/tables/Scalars2.schema examples-wide/Caption.schema tables/pointers tables/blobs test/tables/P2.schema test/tables/W1.schema test/tables/W2.schema tables/lists tables/maps tables/stream test/tables/G1.schema tables/backend tables/vocab tables/vocab9 test/tables/R1.schema test/tables/R2.schema test/tables/RT1.schema test/tables/RT2.schema test/tables/RT3.schema test/tables/CsRetain1.schema test/tables/CsRetain2.schema test/tables/CsCollections1.schema test/tables/CsCollections2.schema
	@mkdir -p build/tables-generated-cs
	./bin/schema generate --lang cs --out build/tables-generated-cs/examples tables/examples
	# The pointered unit carries managed wire storage and native cooked readers.
	./bin/schema generate --lang cs --out build/tables-generated-cs/pointers tables/pointers
	./bin/schema generate --lang cs --out build/tables-generated-cs/block tables/block
	./bin/schema generate --lang cs --out build/tables-generated-cs/blockhome tables/blockhome
	./bin/schema generate --lang cs --out build/tables-generated-cs/v1 test/tables/V1.schema
	./bin/schema generate --lang cs --out build/tables-generated-cs/v2 test/tables/V2.schema
	./bin/schema generate --lang cs --out build/tables-generated-cs/p1 test/tables/P1.schema
	./bin/schema generate --lang cs --out build/tables-generated-cs/p3 test/tables/P3.schema
	./bin/schema generate --lang cs --out build/tables-generated-cs/k1 test/tables/K1.schema
	./bin/schema generate --lang cs --out build/tables-generated-cs/k2 test/tables/K2.schema
	./bin/schema generate --lang cs --out build/tables-generated-cs/csids test/tables/CsIds.schema
	./bin/schema generate --lang cs --out build/tables-generated-cs/csunions test/tables/CsUnions.schema
	./bin/schema generate --lang cs --out build/tables-generated-cs/csview test/tables/CsView.schema
	./bin/schema generate --lang cs --out build/tables-generated-cs/messages tables/messages
	./bin/schema generate --lang cs --out build/tables-generated-cs/m1 test/tables/M1.schema
	./bin/schema generate --lang cs --out build/tables-generated-cs/m2 test/tables/M2.schema
	./bin/schema generate --lang cs --out build/tables-generated-cs/a1 test/tables/A1.schema
	./bin/schema generate --lang cs --out build/tables-generated-cs/a2 test/tables/A2.schema
	./bin/schema generate --lang cs --out build/tables-generated-cs/scalars tables/scalars
	./bin/schema generate --lang cs --out build/tables-generated-cs/scalars2 test/tables/Scalars2.schema
	./bin/schema generate --lang cs --out build/tables-generated-cs/caption examples-wide/Caption.schema
	./bin/schema generate --lang cs --out build/tables-generated-cs/blobs tables/blobs
	./bin/schema generate --lang cs --out build/tables-generated-cs/p2 test/tables/P2.schema
	./bin/schema generate --lang cs --out build/tables-generated-cs/w1 test/tables/W1.schema
	./bin/schema generate --lang cs --out build/tables-generated-cs/w2 test/tables/W2.schema
	./bin/schema generate --lang cs --out build/tables-generated-cs/lists tables/lists
	./bin/schema generate --lang cs --out build/tables-generated-cs/maps tables/maps
	./bin/schema generate --lang cs --out build/tables-generated-cs/stream tables/stream
	./bin/schema generate --lang cs --out build/tables-generated-cs/g1 test/tables/G1.schema
	./bin/schema generate --lang cs --out build/tables-generated-cs/backend tables/backend
	./bin/schema generate --lang cs --out build/tables-generated-cs/vocab tables/vocab
	./bin/schema generate --lang cs --out build/tables-generated-cs/vocab9 tables/vocab9
	./bin/schema generate --lang cs --out build/tables-generated-cs/r1 test/tables/R1.schema
	./bin/schema generate --lang cs --out build/tables-generated-cs/r2 test/tables/R2.schema
	./bin/schema generate --lang cs --out build/tables-generated-cs/rt1 test/tables/RT1.schema
	./bin/schema generate --lang cs --out build/tables-generated-cs/rt2 test/tables/RT2.schema
	./bin/schema generate --lang cs --out build/tables-generated-cs/rt3 test/tables/RT3.schema
	./bin/schema generate --lang cs --out build/tables-generated-cs/csretain1 test/tables/CsRetain1.schema
	./bin/schema generate --lang cs --out build/tables-generated-cs/csretain2 test/tables/CsRetain2.schema
	./bin/schema generate --lang cs --out build/tables-generated-cs/cscollections1 test/tables/CsCollections1.schema
	./bin/schema generate --lang cs --out build/tables-generated-cs/cscollections2 test/tables/CsCollections2.schema

	@touch $@

# The C# twin of the C++ "no serialize include path" build: a generated
# <Base>Table.cs must stand alone on the BCL, so nothing in it may name the
# serialize runtime. (C# has no include paths to withhold, so the property is
# gated by inspection rather than by the compiler.)
.PHONY: tables-cs-standalone
tables-cs-standalone: build/tables-generated-cs/.stamp
	@n=$$(ls build/tables-generated-cs/*/*Table.cs 2>/dev/null | wc -l | tr -d ' '); \
		if [ "$$n" -lt 8 ]; then \
			echo "STANDALONE GATE FAILED: found $$n generated Table sources, expected 8 — the glob, not the property, is what broke"; exit 1; \
		fi
	@for f in build/tables-generated-cs/*/*Table.cs; do \
		if grep -n "Serialize" $$f; then \
			echo "STANDALONE GATE FAILED: the serialize runtime leaked into $$f"; exit 1; \
		fi; \
	done
	@echo "tables C# standalone gate: generated Table sources name no runtime"

.PHONY: tables-cs-variable-surface
tables-cs-variable-surface: build/tables-generated-cs/.stamp
	@for verb in LoadMeasure Load Save Measure LoadMessages SaveMessages Cook CookMeasure; do \
		grep -Fq "Scene$$verb(" build/tables-generated-cs/pointers/*Table.cs || \
			{ echo "VARIABLE SURFACE GATE FAILED: Scene$$verb is absent"; exit 1; }; \
	done
	@echo "tables C# variable surface: pointered roots carry file, message and cook verbs"

# ---------------------------------------------------------------------------
# THE COOK's C# READ SIDE (docs/SPEC-TABLES.md §7) --------------------------------
#
# The third implementation of one page: `schema cook` writes the file in Go,
# the C++ <Root>Open points at it, and the C# <Root>Cook.Open points at the very
# same bytes — and none of the three was written from either of the others. Two
# gates come out of that and neither could exist with one implementation:
#
#   THE DIRECTORY LOCK, which the C++ leg also holds: every node the C# side
#   reaches through its OWN derefs is a node the cook's ATTRIBUTION part names,
#   at that offset, with that type id, and the two SETS are equal.
#
#   THE DUMP, which is new here and is what the directory lock cannot do: both
#   readers write their walk out as canonical text — one line per leaf, with the
#   value read at that offset — and the two files are BYTE-COMPARED. A record
#   laid out one byte differently INSIDE a node moves no node offset and no
#   directory entry, so the lock above cannot see it and this can.
#
# The fixtures are the C++ leg's own, deliberately: the same files, opened by
# two runtimes, is what makes the comparison mean anything.
#
# COOK_CS_N is this leg's fuzz budget, set by MEASUREMENT rather than by
# inheritance: the C# fuzzer runs without a sanitizer under it, so a mutant
# costs ~11.6 us here against the C++ leg's ASan-slowed pass, and two roots at a
# million mutants each is ~23 s — inside the 60 s this gate is allowed and ten
# times the shared N. `make ... N=<n>` still overrides it.
COOK_CS_N ?= 1000000
COOK_CS := cd test/cs-cook && $(DOTNET) run --no-build --

.PHONY: build-cs-cook
build-cs-cook: build/tables-generated-cs/.stamp
	cd test/cs-cook && $(DOTNET) build -v q --nologo

.PHONY: tables-cook-open-cs
tables-cook-open-cs: build-cs-cook build/schema_test_cook build/cook-open/.stamp build/cook-open-fixed/.stamp
	# §20.3's C# half, at START-UP rather than at the first open: every cooked
	# record's size and every field's offset against the compiler's own model
	$(COOK_CS) layout
	@for r in $(COOK_ROOTS) $(COOK_FIXED_ROOTS); do \
		d=build/cook-open; \
		case " $(COOK_FIXED_ROOTS) " in *" $$r "*) d=build/cook-open-fixed;; esac; \
		( $(COOK_CS) golden $$r ../../$$d/$$r.cook ) || exit 1; \
		./build/schema_test_cook dump $$r $$d/$$r.cook > build/$$r.cpp.dump || exit 1; \
		( $(COOK_CS) dump $$r ../../$$d/$$r.cook ) > build/$$r.cs.dump || exit 1; \
		cmp build/$$r.cpp.dump build/$$r.cs.dump || \
			{ echo "CROSS-IMPLEMENTATION DUMP FAILED: the C++ and C# walks of $$r differ"; exit 1; }; \
		echo "cook dump lock: $$r — the C++ and C# walks are byte-identical ($$(wc -l < build/$$r.cs.dump | tr -d ' ') lines)"; \
	done
	@for r in $(COOK_FIXED_ROOTS); do \
		( $(COOK_CS) fixedvalues $$r ../../build/cook-open-fixed/$$r.cook ) || exit 1; \
		( $(COOK_CS) forge $$r ../../build/cook-open-fixed/$$r.cook ) || exit 1; \
	done
	$(COOK_CS) usage Scene ../../build/cook-open/Scene.cook
	$(COOK_CS) forge Scene ../../build/cook-open/Scene.cook
	$(COOK_CS) forge Depot ../../build/cook-open/Depot.cook
	cd test/cs-cook && SEED=$(SEED) N=$(if $(filter-out 100000,$(N)),$(N),$(COOK_CS_N)) $(DOTNET) run --no-build -- fuzz Scene ../../build/cook-open/Scene.cook
	cd test/cs-cook && SEED=$(SEED) N=$(if $(filter-out 100000,$(N)),$(N),$(COOK_CS_N)) $(DOTNET) run --no-build -- fuzz TreeNode ../../build/cook-open/TreeNode.cook
	$(COOK_CS) accept Scene ../../build/cook-open/Scene.cook
	# THE BYTE-ORDER LEG's C# half, and it is HALF: a cook written --byte-order
	# big is refused by the MAGIC here, which is the refusal the page promises.
	# The other half — a big-endian consumer opening a big-endian cook natively —
	# is UNPROVEN in C# and stays so until a big-endian .NET exists; the C++ leg
	# proves it on s390x, and the page says which half each leg holds (§7.5).
	$(COOK_CS) refuse Scene ../../build/cook-open/Scene-be.cook
	$(COOK_CS) time Scene ../../build/cook-open/1mb.cook ../../build/cook-open/100mb.cook

# THE GIGABYTE ARM, by hand (§7.5).
.PHONY: tables-cook-open-cs-1gb
tables-cook-open-cs-1gb: build-cs-cook build/cook-open/.stamp
	go build -o build/cookgen ./test/cookgen
	./build/cookgen --bytes 1073741824 --root Scene --out build/cook-open/1gb.cook
	$(COOK_CS) time Scene ../../build/cook-open/1mb.cook ../../build/cook-open/1gb.cook

# THE NEGATIVE CONTROLS for the C# Open, and a battery that has never gone red
# is watching nothing. Each removes ONE clause of the generated check through a
# `go build -overlay` on the emitter — so no tracked file is ever written to,
# an interrupt cannot leave a sabotaged working tree, and a parallel `make -j`
# cannot compile the sabotage into something else — regenerates the unit from
# the sabotaged emitter, and requires the C# battery to go RED.
#
# The two clauses are the C++ leg's two, for the same reason: they are the two
# that decide whether Open can hand back storage the caller never gave it.
#
# The sabotage keeps every local it defeats IN USE, because a generated C# file
# with an unused local does not compile under TreatWarningsAsErrors and a
# control that fails to BUILD is not a control that went red.
define cook_open_cs_sabotage
	@rm -rf build/cook-open-cs-$(1) && mkdir -p build/cook-open-cs-$(1)
	@sed 's|^.*$(2).*$$|	g.hf("        $(3)\\n")|' \
		internal/codegen/cstable/cook.go > build/cook-open-cs-$(1)/cook.gotext
	@cmp -s build/cook-open-cs-$(1)/cook.gotext internal/codegen/cstable/cook.go && \
		{ echo "NEGATIVE CONTROL FAILED: the $(1) sabotage patched nothing"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/codegen/cstable/cook.go":"%s/build/cook-open-cs-$(1)/cook.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/cook-open-cs-$(1)/overlay.json
	@go build -overlay=build/cook-open-cs-$(1)/overlay.json -o build/cook-open-cs-$(1)/schema ./cmd/schema
	./build/cook-open-cs-$(1)/schema generate --lang cs --out build/cook-open-cs-$(1)/gen tables/pointers
	@if ( cd test/cs-cook && $(DOTNET) build -v q --nologo \
			-p:CookGeneratedDir=../../build/cook-open-cs-$(1)/gen \
			-p:BaseOutputPath=../../build/cook-open-cs-$(1)/bin/ \
			-p:BaseIntermediateOutputPath=../../build/cook-open-cs-$(1)/obj/ \
			> ../../build/cook-open-cs-$(1)/build.log 2>&1 ); then :; else \
		echo "NEGATIVE CONTROL FAILED: the sabotaged emitter's output does not compile"; \
		cat build/cook-open-cs-$(1)/build.log; exit 1; \
	fi
	@if ( cd test/cs-cook && $(DOTNET) run --no-build \
			-p:CookGeneratedDir=../../build/cook-open-cs-$(1)/gen \
			-p:BaseOutputPath=../../build/cook-open-cs-$(1)/bin/ \
			-p:BaseIntermediateOutputPath=../../build/cook-open-cs-$(1)/obj/ \
			-- forge Scene ../../build/cook-open/Scene.cook > ../../build/cook-open-cs-$(1)/log 2>&1 ); then \
		echo "NEGATIVE CONTROL FAILED: the battery stayed green with the $(1) check removed"; exit 1; \
	fi
	@grep -q "FAILED" build/cook-open-cs-$(1)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the battery went red, but not on a forgery"; cat build/cook-open-cs-$(1)/log; exit 1; }
	@grep -m1 -A2 "FAILED" build/cook-open-cs-$(1)/log
	@echo "negative control: removing the $(1) check turns the C# cook forgery battery RED"
endef

.PHONY: tables-cook-open-cs-lengths-negative-control
tables-cook-open-cs-lengths-negative-control: build/cook-open/.stamp
	$(call cook_open_cs_sabotage,lengths,dataOffset + dataLength + attribution != bytes,if (dataOffset == ulong.MaxValue \&\& attribution == ulong.MaxValue) { return false; } // NEGATIVE CONTROL)

.PHONY: tables-cook-open-cs-root-negative-control
tables-cook-open-cs-root-negative-control: build/cook-open/.stamp
	$(call cook_open_cs_sabotage,root,if (dataLength < %d),if (dataLength == ulong.MaxValue) { return false; } // NEGATIVE CONTROL)

# THE WALK CONTROL, the C# half of the Makefile's tables-cook-open-walk-negative-control:
# the sabotage (tools/sabotage, cook-open-walk-cs) leaves every check in place
# and makes Open sum every word of the region before it returns, and the
# open-cost gate must go RED on its band over the two fixtures the certification
# run times. ITERATIONS is low for the reason given there.
.PHONY: tables-cook-open-cs-walk-negative-control
tables-cook-open-cs-walk-negative-control: build/cook-open/.stamp
	@rm -rf build/cook-open-cs-walk && mkdir -p build/cook-open-cs-walk
	@go run ./tools/sabotage -name cook-open-walk-cs -out build/cook-open-cs-walk/cook.gotext internal/codegen/cstable/cook.go
	@printf '{"Replace":{"%s/internal/codegen/cstable/cook.go":"%s/build/cook-open-cs-walk/cook.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/cook-open-cs-walk/overlay.json
	@go build -overlay=build/cook-open-cs-walk/overlay.json -o build/cook-open-cs-walk/schema ./cmd/schema
	./build/cook-open-cs-walk/schema generate --lang cs --out build/cook-open-cs-walk/gen tables/pointers
	@if ( cd test/cs-cook && $(DOTNET) build -v q --nologo \
			-p:CookGeneratedDir=../../build/cook-open-cs-walk/gen \
			-p:BaseOutputPath=../../build/cook-open-cs-walk/bin/ \
			-p:BaseIntermediateOutputPath=../../build/cook-open-cs-walk/obj/ \
			> ../../build/cook-open-cs-walk/build.log 2>&1 ); then :; else \
		echo "NEGATIVE CONTROL FAILED: the sabotaged emitter's output does not compile"; \
		cat build/cook-open-cs-walk/build.log; exit 1; \
	fi
	@if ( cd test/cs-cook && ITERATIONS=10 $(DOTNET) run --no-build \
			-p:CookGeneratedDir=../../build/cook-open-cs-walk/gen \
			-p:BaseOutputPath=../../build/cook-open-cs-walk/bin/ \
			-p:BaseIntermediateOutputPath=../../build/cook-open-cs-walk/obj/ \
			-- time Scene ../../build/cook-open/1mb.cook ../../build/cook-open/100mb.cook > ../../build/cook-open-cs-walk/log 2>&1 ); then \
		echo "NEGATIVE CONTROL FAILED: the open-cost gate stayed green with a walk in Open"; cat build/cook-open-cs-walk/log; exit 1; \
	fi
	@grep -q "FAILED: open time is not flat" build/cook-open-cs-walk/log || \
		{ echo "NEGATIVE CONTROL FAILED: the gate went red, but not on its band"; cat build/cook-open-cs-walk/log; exit 1; }
	@grep "cook open is O(1)" build/cook-open-cs-walk/log
	@grep -m1 "FAILED" build/cook-open-cs-walk/log
	@echo "negative control: a walk in Open turns the C# open-cost gate RED"

generated/bench/tables/cs/.stamp: bin/schema bench/corpus/BenchTable.schema
	@mkdir -p generated/bench/tables/cs
	./bin/schema generate --lang cs --out generated/bench/tables/cs bench/corpus/BenchTable.schema
	@touch $@

generated/bench/cs/.stamp: bin/schema $(SCHEMAS_BENCH)
	./bin/schema generate --lang cs --out generated/bench/cs bench/corpus/Bench.schema
	./bin/schema generate --lang cs --out generated/bench/cs/realworld bench/corpus/RealWorld.schema
	@touch $@

.PHONY: build-conformance-cs
build-conformance-cs: build/tables-generated-cs/.stamp
	cd test/conformance/cs && $(DOTNET) build -v q --nologo

# THE NEGATIVE CONTROL FOR THE C# WALK (docs/SPEC-TABLES.md §16.5), and it is a
# different sabotage from the C++ one above on purpose. That one flips a byte of
# a DUMP and proves the harness can see a wrong ANSWER; this one breaks the
# WALKER and proves the harness can see a wrong WALK.
#
# It is sabotaged IN THE EMITTER and generated afresh, the way the block fuzz's
# controls are (block_fuzz_sabotage above), because the walker IS emitter
# source — one constant in internal/codegen/cstable/json.go — so patching the
# emitter is patching the walk itself rather than an artifact of it. No tracked
# file is written to: the sabotage lands in build/, a Go build overlay points the
# compiler at it, and the csproj's TablesGeneratedDir points the leg at what
# that compiler generated.
#
# THE SABOTAGE. C++ shifts a field's STORAGE OFFSET by one field width
# (tables-json-negative-control). A C# field has no offset — the descriptor
# carries accessors instead (§8.1) — so the twin of that arithmetic is the FIELD
# INDEX the read path looks a descriptor up by: one key's value lands in its
# neighbour's field. It is bounded on purpose, so a table with an odd field
# count cannot turn the control into an exception rather than a wrong answer,
# and it touches the READ path only.
#
# The second half is the point, as it is for every control here: json-read must
# go RED and the wire, report and write surfaces must stay GREEN. A matrix
# whose every cell went red would be saying "something broke" rather than "the C# text form broke" —
# and json-write staying green is what says the break is the READER's.
.PHONY: conformance-negative-control-cs
conformance-negative-control-cs: build-conformance-cs build/conformance-harness
	sh test/conformance/cs/negative-control "$(DOTNET)"

# The C# half of `make update-goldens`: the committed generated table sources
# (testdata/golden/tables/*-cs).
.PHONY: update-goldens-cs
update-goldens-cs: build/tables-generated-cs/.stamp
	@mkdir -p testdata/golden/tables/examples-cs testdata/golden/tables/block-cs testdata/golden/tables/blockhome-cs
	@cp build/tables-generated-cs/examples/*Table.cs testdata/golden/tables/examples-cs/
	@cp build/tables-generated-cs/block/*Table.cs testdata/golden/tables/block-cs/
	@cp build/tables-generated-cs/blockhome/*Table.cs testdata/golden/tables/blockhome-cs/

# THE C# TABLES LEG (test/cs-tables): the corpus in C#, every instance loaded
# from its wire golden, re-saved and byte-compared, and every §16 text read and
# written beside it. It is the C# twin of tables-js-leg.
#
.PHONY: tables-cs-view tables-cs-leg tables-cs-wire-fuzz tables-cs-region-fuzz tables-cs-builder-fuzz tables-cs-retain-fuzz
tables-cs-view: build/tables-generated-cs/.stamp
	@mkdir -p build/view-cs
	@set -e; for entry in $(VIEW_CORPUS); do \
		dir=$${entry%%:*}; pkg=$${entry##*:}; \
		cap=$$(printf '%s' "$$pkg" | cut -c1 | tr 'a-z' 'A-Z')$$(printf '%s' "$$pkg" | cut -c2-); \
		source=tables/$$dir; [ "$$dir" != wide ] || source=examples-wide; \
		./bin/schema generate --lang cs --out build/view-cs/generated/$$dir "$$source"; \
		printf 'global using U = %s;\n' "$$cap" > build/view-cs/Unit.cs; \
		$(DOTNET) build test/cs-view -v q --nologo \
			-p:ViewAliasFile="$$PWD/build/view-cs/Unit.cs" -p:ViewGeneratedDir="$$PWD/build/view-cs/generated/$$dir" > build/view-cs/$$pkg.log 2>&1 || { cat build/view-cs/$$pkg.log; exit 1; }; \
		$(DOTNET) test/cs-view/bin/Debug/net10.0/schema-view.dll > build/view-cs/$$pkg.listing; \
		if [ "$$pkg" = tabledemo ]; then $(DOTNET) test/cs-view/bin/Debug/net10.0/schema-view.dll unflattened > build/view-cs/unflattened.listing; fi; \
	done
	SCHEMA_VIEW_LISTING_DIR=$$PWD/build/view-cs go test ./internal/viewlisting -run TestUnitViewListingMatchesTheIR
	@mkdir -p build/view-cs/negative
	@cp build/view-cs/unflattened.listing build/view-cs/negative/tabledemo.listing
	@if SCHEMA_VIEW_LISTING_DIR=$$PWD/build/view-cs/negative SCHEMA_VIEW_LISTING_UNITS=tabledemo go test ./internal/viewlisting -run TestUnitViewListingMatchesTheIR > build/view-cs/negative.log 2>&1; then echo "C# UnitView negative control escaped"; exit 1; fi
	@grep -q "listing is not the compiler's" build/view-cs/negative.log
	@echo "C# UnitView: $(words $(VIEW_CORPUS)) generated registries match the IR; damaged documentation is detected"

tables-cs-leg: build/tables-generated-cs/.stamp
	cd test/cs-tables && $(DOTNET) run
	cd test/cs-tables && $(DOTNET) run -c Release

tables-cs-wire-fuzz: build-conformance-cs build/conformance-harness
	./build/conformance-harness wire-fuzz --driver "$(DOTNET) test/conformance/cs/bin/Debug/net10.0/schemaconformance.dll wire-fuzz" --seed $(SEED) --n $(N)

tables-cs-region-fuzz: build-conformance-cs build/conformance-harness
	./build/conformance-harness wire-fuzz --driver "$(DOTNET) test/conformance/cs/bin/Debug/net10.0/schemaconformance.dll wire-fuzz-region" --seed $(SEED) --n $(N)

tables-cs-builder-fuzz: build-conformance-cs build/conformance-harness
	./build/conformance-harness wire-fuzz --driver "$(DOTNET) test/conformance/cs/bin/Debug/net10.0/schemaconformance.dll wire-fuzz-builder" --seed $(SEED) --n $(N) --builder --failed build/wire-fuzz/failed-builder-cs.bin

tables-cs-retain-fuzz: build-conformance-cs build/conformance-harness
	./build/conformance-harness wire-fuzz --retain --driver "$(DOTNET) test/conformance/cs/bin/Debug/net10.0/schemaconformance.dll wire-fuzz" --seed $(SEED) --n $(N) --failed build/wire-fuzz/failed-retain-cs.bin

.PHONY: tables-cs-retain-negative-control
tables-cs-retain-negative-control: bin/schema
	sh test/cs-tables/retain-negative-control "$(DOTNET)"

.PHONY: tables-cs-pack-negative-control
tables-cs-pack-negative-control: bin/schema
	sh test/cs-tables/pack-negative-control "$(DOTNET)"

.PHONY: tables-cs-message-blob-endian-negative-control
tables-cs-message-blob-endian-negative-control: bin/schema
	sh test/cs-tables/message-blob-endian-control "$(DOTNET)"

# THE C# LEG of `make test`: the table gates and the C# conformance negative
# control, the cook-open gates on the C# side, the bench units' compile gates
# (a unit that generates but does not compile is issue #80's lesson), and the
# packet tests.
.PHONY: test-cs
test-cs: toolchain-cs build/tables-generated-cs/.stamp generated/bench/tables/cs/.stamp generated/cs/.stamp generated/cs-ludicrous/.stamp generated/bench/cs/.stamp
	$(MAKE) tables-cs-json-walk
	$(MAKE) tables-cs-standalone
	$(MAKE) tables-cs-variable-surface
	$(MAKE) tables-cs-view
	$(MAKE) tables-cs-leg
	$(MAKE) tables-cs-wire-fuzz
	$(MAKE) tables-cs-region-fuzz
	$(MAKE) tables-cs-builder-fuzz
	$(MAKE) tables-cs-retain-fuzz
	$(MAKE) tables-cs-retain-negative-control
	$(MAKE) tables-cs-pack-negative-control
	$(MAKE) tables-cs-message-blob-endian-negative-control
	$(MAKE) conformance-negative-control-cs
	$(MAKE) tables-cook-open-cs
	$(MAKE) tables-cook-open-cs-lengths-negative-control
	$(MAKE) tables-cook-open-cs-root-negative-control
	$(MAKE) tables-cook-open-cs-walk-negative-control
	$(DOTNET) build bench/tables/cs -c Release --nologo -v quiet
	cd bench/cs && $(DOTNET) build -c Release --nologo -v quiet
	cd test/cs && $(DOTNET) run
	cd test/cs && $(DOTNET) run -c Release
	cd test/cs-ludicrous && $(DOTNET) run

TEST_LEGS         += test-cs
TOOLCHAIN_LEGS    += cs
TOOLCHAIN_PINS_cs  := DOTNET
CONFORMANCE_LEGS  += $(call unless_skipped,cs,build-conformance-cs build-cs-cook)
BENCH_TABLES_LEGS += generated/bench/tables/cs/.stamp
GOLDENS_LEGS      += update-goldens-cs
# Packet UTF-8 content validation, including a compiled mutation control.
build/packet-text/cs/.stamp: bin/schema test/packet-text/Narrow.schema
	./bin/schema generate --lang cs --out build/packet-text/cs test/packet-text/Narrow.schema
	@touch $@

.PHONY: packet-utf8-cs packet-utf8-cs-negative-control
packet-utf8-cs: build/packet-text/cs/.stamp build/packet-text/cpp/driver build/packet-text/harness
	$(DOTNET) build test/packet-text/cs/packet-text.csproj -c Debug -o build/packet-text/cs/debug --nologo
	./build/packet-text/harness dotnet build/packet-text/cs/debug/packet-text.dll
	$(DOTNET) build test/packet-text/cs/packet-text.csproj -c Release -o build/packet-text/cs/release --nologo
	./build/packet-text/harness dotnet build/packet-text/cs/release/packet-text.dll

packet-utf8-cs-negative-control: packet-utf8-cs
	@mkdir -p build/packet-text/cs-negative
	go run ./tools/sabotage -name packet-utf8-cs-read -out build/packet-text/cs-negative/utf8.gotext internal/codegen/csharp/utf8.go
	@printf '{"Replace":{"%s/internal/codegen/csharp/utf8.go":"%s/build/packet-text/cs-negative/utf8.gotext"}}\n' "$(CURDIR)" "$(CURDIR)" > build/packet-text/cs-negative/overlay.json
	go run -overlay=build/packet-text/cs-negative/overlay.json ./cmd/schema generate --lang cs --out build/packet-text/cs-negative test/packet-text/Narrow.schema
	$(DOTNET) build test/packet-text/cs/packet-text.csproj -c Release -o build/packet-text/cs-negative/bin -p:TextDir="$(CURDIR)/build/packet-text/cs-negative" --nologo
	@if ./build/packet-text/harness -mutations-only dotnet build/packet-text/cs-negative/bin/packet-text.dll > build/packet-text/cs-negative/log 2>&1; then echo 'NEGATIVE CONTROL FAILED: C# UTF-8 removal passed'; exit 1; fi
	@grep -Fq 'FAILED: packet-text verdict on ' build/packet-text/cs-negative/log || { cat build/packet-text/cs-negative/log; exit 1; }
	@echo 'packet UTF-8 C# negative control: removed read validation fails bit-flip agreement'

test-cs: packet-utf8-cs packet-utf8-cs-negative-control


build/packet-wide/cs/.stamp: bin/schema build/packet-wide/source/WideText.schema test/packet-wide/Shapes.schema
	./bin/schema generate --lang cs --out build/packet-wide/cs/wide build/packet-wide/source/WideText.schema
	./bin/schema generate --lang cs --out build/packet-wide/cs/shapes test/packet-wide/Shapes.schema
	@touch $@

.PHONY: packet-wide-cs packet-wide-cs-negative-control
packet-wide-cs: build/packet-wide/cs/.stamp build/packet-wide/cpp/driver build/packet-text/harness
	$(DOTNET) build test/packet-wide/cs/packet-wide.csproj -c Debug -o build/packet-wide/cs/debug --nologo
	$(DOTNET) build/packet-wide/cs/debug/packet-wide.dll --contracts
	./build/packet-text/harness -wide -corpus testdata/conformance/text/wstring.txt -oracle build/packet-wide/cpp/driver dotnet build/packet-wide/cs/debug/packet-wide.dll
	$(DOTNET) build test/packet-wide/cs/packet-wide.csproj -c Release -o build/packet-wide/cs/release --nologo
	$(DOTNET) build/packet-wide/cs/release/packet-wide.dll --contracts
	./build/packet-text/harness -wide -corpus testdata/conformance/text/wstring.txt -oracle build/packet-wide/cpp/driver dotnet build/packet-wide/cs/release/packet-wide.dll

packet-wide-cs-negative-control: packet-wide-cs
	@mkdir -p build/packet-wide/cs-negative
	go run ./tools/sabotage -name packet-wide-cs-pairing -out build/packet-wide/cs-negative/wstring.gotext internal/codegen/csharp/wstring.go
	@printf '{"Replace":{"%s/internal/codegen/csharp/wstring.go":"%s/build/packet-wide/cs-negative/wstring.gotext"}}\n' "$(CURDIR)" "$(CURDIR)" > build/packet-wide/cs-negative/overlay.json
	go run -overlay=build/packet-wide/cs-negative/overlay.json ./cmd/schema generate --lang cs --out build/packet-wide/cs-negative build/packet-wide/source/WideText.schema
	$(DOTNET) build test/packet-wide/cs/packet-wide.csproj -c Release -o build/packet-wide/cs-negative/bin -p:TextDir="$(CURDIR)/build/packet-wide/cs-negative" --nologo
	@if ./build/packet-text/harness -wide -corpus testdata/conformance/text/wstring.txt -oracle build/packet-wide/cpp/driver -mutations-only dotnet build/packet-wide/cs-negative/bin/packet-wide.dll > build/packet-wide/cs-negative/log 2>&1; then echo 'NEGATIVE CONTROL FAILED: C# wide pairing removal passed'; exit 1; fi
	@grep -Fq 'FAILED: packet-text verdict on ' build/packet-wide/cs-negative/log || { cat build/packet-wide/cs-negative/log; exit 1; }
	@echo 'packet wide C# negative control: removed pairing fails bit-flip agreement'

test-cs: packet-wide-cs packet-wide-cs-negative-control
