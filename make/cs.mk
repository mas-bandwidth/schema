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

# THE C# CORPUS'S ONE GENERATION LIST, parameterised by generator binary and
# output root for the same reason the C++ tables_generate (Main Makefile) is:
# the zero-cost gate's block-less arm regenerates the whole corpus from a
# sabotaged emitter, and a second copy of these lines would be a second corpus.
define tables_generate_cs
	$(1) generate --lang cs --out $(2)/examples tables/examples
	$(1) generate --lang cs --out $(2)/pointers tables/pointers
	$(1) generate --lang cs --out $(2)/block tables/block
	$(1) generate --lang cs --out $(2)/blockhome tables/blockhome
	$(1) generate --lang cs --out $(2)/v1 test/tables/V1.schema
	$(1) generate --lang cs --out $(2)/v2 test/tables/V2.schema
	$(1) generate --lang cs --out $(2)/p1 test/tables/P1.schema
	$(1) generate --lang cs --out $(2)/p3 test/tables/P3.schema
	$(1) generate --lang cs --out $(2)/fx1 test/tables/FX1.schema
	$(1) generate --lang cs --out $(2)/fx2 test/tables/FX2.schema
	$(1) generate --lang cs --out $(2)/k1 test/tables/K1.schema
	$(1) generate --lang cs --out $(2)/k2 test/tables/K2.schema
	$(1) generate --lang cs --out $(2)/csids test/tables/CsIds.schema
	$(1) generate --lang cs --out $(2)/csunions test/tables/CsUnions.schema
	$(1) generate --lang cs --out $(2)/csview test/tables/CsView.schema
	$(1) generate --lang cs --out $(2)/messages tables/messages
	$(1) generate --lang cs --out $(2)/m1 test/tables/M1.schema
	$(1) generate --lang cs --out $(2)/m2 test/tables/M2.schema
	$(1) generate --lang cs --out $(2)/a1 test/tables/A1.schema
	$(1) generate --lang cs --out $(2)/a2 test/tables/A2.schema
	$(1) generate --lang cs --out $(2)/scalars tables/scalars
	$(1) generate --lang cs --out $(2)/scalars2 test/tables/Scalars2.schema
	$(1) generate --lang cs --out $(2)/blobs tables/blobs
	$(1) generate --lang cs --out $(2)/p2 test/tables/P2.schema
	$(1) generate --lang cs --out $(2)/w1 test/tables/W1.schema
	$(1) generate --lang cs --out $(2)/w2 test/tables/W2.schema
	$(1) generate --lang cs --out $(2)/lists tables/lists
	$(1) generate --lang cs --out $(2)/maps tables/maps
	$(1) generate --lang cs --out $(2)/stream tables/stream
	$(1) generate --lang cs --out $(2)/g1 test/tables/G1.schema
	$(1) generate --lang cs --out $(2)/backend tables/backend
	$(1) generate --lang cs --out $(2)/vocab tables/vocab
	$(1) generate --lang cs --out $(2)/vocab9 tables/vocab9
	$(1) generate --lang cs --out $(2)/r1 test/tables/R1.schema
	$(1) generate --lang cs --out $(2)/r2 test/tables/R2.schema
	$(1) generate --lang cs --out $(2)/rt1 test/tables/RT1.schema
	$(1) generate --lang cs --out $(2)/rt2 test/tables/RT2.schema
	$(1) generate --lang cs --out $(2)/rt3 test/tables/RT3.schema
	$(1) generate --lang cs --out $(2)/csretain1 test/tables/CsRetain1.schema
	$(1) generate --lang cs --out $(2)/csretain2 test/tables/CsRetain2.schema
	$(1) generate --lang cs --out $(2)/cscollections1 test/tables/CsCollections1.schema
	$(1) generate --lang cs --out $(2)/cscollections2 test/tables/CsCollections2.schema
	$(1) generate --lang cs --out $(2)/ut test/tables/UT.schema
endef

build/tables-generated-cs/.stamp: bin/schema $(SCHEMAS_TABLES) $(SCHEMAS_TABLES_POINTERS) $(SCHEMAS_TABLES_BLOCK) test/tables/V1.schema test/tables/V2.schema test/tables/P1.schema test/tables/P3.schema test/tables/FX1.schema test/tables/FX2.schema test/tables/K1.schema test/tables/K2.schema test/tables/CsIds.schema test/tables/CsUnions.schema test/tables/CsView.schema $(SCHEMAS_TABLES_MESSAGES) test/tables/M1.schema test/tables/M2.schema test/tables/A1.schema test/tables/A2.schema tables/scalars test/tables/Scalars2.schema tables/pointers tables/blobs test/tables/P2.schema test/tables/W1.schema test/tables/W2.schema tables/lists tables/maps tables/stream test/tables/G1.schema tables/backend tables/vocab tables/vocab9 test/tables/R1.schema test/tables/R2.schema test/tables/RT1.schema test/tables/RT2.schema test/tables/RT3.schema test/tables/CsRetain1.schema test/tables/CsRetain2.schema test/tables/CsCollections1.schema test/tables/CsCollections2.schema test/tables/UT.schema
	@mkdir -p build/tables-generated-cs
	$(call tables_generate_cs,./bin/schema,build/tables-generated-cs)
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
	@for verb in LoadMeasure Load Save Measure Cook CookMeasure; do \
		grep -Fq "Scene$$verb(" build/tables-generated-cs/pointers/*Table.cs || \
			{ echo "VARIABLE SURFACE GATE FAILED: Scene$$verb is absent"; exit 1; }; \
	done
	@for verb in LoadMessages SaveMessages MeasureMessages LoadRetainMessages SaveRetainMessages; do \
		if grep -Fq "Scene$$verb(" build/tables-generated-cs/pointers/*.cs; then \
			echo "VARIABLE SURFACE GATE FAILED: Scene$$verb is emitted, and the message form is fixed tables only (docs/SPEC-TABLES.md §3.3)"; exit 1; \
		fi; \
	done
	@echo "tables C# variable surface: pointered roots carry file and cook verbs and no message verb"

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

# THE BYTE-ORDER CONTROL (docs/PORTING.md I7, the refuse half): the sabotage
# (tools/sabotage, cook-open-order-cs) makes the magic stop refusing, so a cook
# of the other byte order falls through to a later guard and is refused under
# that guard's name. tables-cook-open-cs's `refuse` verb requires the refusal to
# name the order, foreign_order, and must go RED.
.PHONY: tables-cook-open-cs-order-negative-control
tables-cook-open-cs-order-negative-control: build/cook-open/.stamp
	@rm -rf build/cook-open-cs-order && mkdir -p build/cook-open-cs-order
	@go run ./tools/sabotage -name cook-open-order-cs -out build/cook-open-cs-order/cook.gotext internal/codegen/cstable/cook.go
	@printf '{"Replace":{"%s/internal/codegen/cstable/cook.go":"%s/build/cook-open-cs-order/cook.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/cook-open-cs-order/overlay.json
	@go build -overlay=build/cook-open-cs-order/overlay.json -o build/cook-open-cs-order/schema ./cmd/schema
	./build/cook-open-cs-order/schema generate --lang cs --out build/cook-open-cs-order/gen tables/pointers
	@if ( cd test/cs-cook && $(DOTNET) build -v q --nologo \
			-p:CookGeneratedDir=../../build/cook-open-cs-order/gen \
			-p:BaseOutputPath=../../build/cook-open-cs-order/bin/ \
			-p:BaseIntermediateOutputPath=../../build/cook-open-cs-order/obj/ \
			> ../../build/cook-open-cs-order/build.log 2>&1 ); then :; else \
		echo "NEGATIVE CONTROL FAILED: the sabotaged emitter's output does not compile"; \
		cat build/cook-open-cs-order/build.log; exit 1; \
	fi
	@if ( cd test/cs-cook && $(DOTNET) run --no-build \
			-p:CookGeneratedDir=../../build/cook-open-cs-order/gen \
			-p:BaseOutputPath=../../build/cook-open-cs-order/bin/ \
			-p:BaseIntermediateOutputPath=../../build/cook-open-cs-order/obj/ \
			-- refuse Scene ../../build/cook-open/Scene-be.cook > ../../build/cook-open-cs-order/log 2>&1 ); then \
		echo "NEGATIVE CONTROL FAILED: the refuse verb stayed green with the magic's refusal removed"; cat build/cook-open-cs-order/log; exit 1; \
	fi
	@grep -q "not foreign_order" build/cook-open-cs-order/log || \
		{ echo "NEGATIVE CONTROL FAILED: the refuse verb went red, but not on the order's name"; cat build/cook-open-cs-order/log; exit 1; }
	@grep -m1 "not foreign_order" build/cook-open-cs-order/log
	@echo "negative control: a magic that does not refuse turns the C# cross-endian refusal RED"

# I12 (docs/PORTING.md) — THE DOCUMENTED SURFACE COMPILES AND RUNS, C# side:
# docs/USAGE.md's C# cook example (between the cs-table-usage markers) is
# pulled out of the page, compiled against tables/pointers generated afresh and
# run over the Scene cook fixture. Its control moves SceneCook.Open in the
# generated source and requires the page to stop compiling, naming it.
.PHONY: tables-cs-usage tables-cs-usage-negative-control
tables-cs-usage: bin/schema build/cook-open/.stamp
	sh test/cs-cook/usage "$(DOTNET)"

tables-cs-usage-negative-control: bin/schema build/cook-open/.stamp
	sh test/cs-cook/usage "$(DOTNET)" control

test-cs: tables-cs-usage

# M12 (docs/PORTING.md): every multi-byte wire access in the generated C# is an
# explicit little-endian call or a guarded flat-array copy
# (test/cs-tables/byte-order). Its control puts one fixed-form flat array read
# back to an unguarded host-order bulk copy in the emitter (tools/sabotage,
# table-cs-host-order-read) and requires the scan RED, naming it: every round
# trip on a little-endian host stays green under that sabotage, which is why the
# gate is a scan.
.PHONY: tables-cs-byte-order tables-cs-byte-order-negative-control
tables-cs-byte-order: build/tables-generated-cs/.stamp
	sh test/cs-tables/byte-order build/tables-generated-cs

tables-cs-byte-order-negative-control: bin/schema
	@rm -rf build/cs-byte-order-nc && mkdir -p build/cs-byte-order-nc
	@go run ./tools/sabotage -name table-cs-host-order-read -out build/cs-byte-order-nc/fixedform.gotext internal/codegen/cstable/fixedform.go
	@printf '{"Replace":{"%s/internal/codegen/cstable/fixedform.go":"%s/build/cs-byte-order-nc/fixedform.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/cs-byte-order-nc/overlay.json
	@go build -overlay=build/cs-byte-order-nc/overlay.json -o build/cs-byte-order-nc/schema ./cmd/schema
	./build/cs-byte-order-nc/schema generate --lang cs --out build/cs-byte-order-nc/gen/examples tables/examples
	@if sh test/cs-tables/byte-order build/cs-byte-order-nc/gen > build/cs-byte-order-nc/log 2>&1; then \
		cat build/cs-byte-order-nc/log; echo "NEGATIVE CONTROL FAILED: an unguarded host-order wire copy passed the byte-order scan"; exit 1; \
	fi
	@grep -q "SABOTAGED: host order" build/cs-byte-order-nc/log || \
		{ cat build/cs-byte-order-nc/log; echo "NEGATIVE CONTROL FAILED: the scan went red, but not on the planted copy"; exit 1; }
	@echo "negative control: an unguarded host-order fixed-form copy in the emitter turns tables-cs-byte-order red"

test-cs: tables-cs-byte-order

# I4 (docs/PORTING.md) — PER-CASE ABSENT, C# side. The C# driver answers a case
# it cannot answer with <case>.absent (test/conformance/cs/src/Program.cs:
# SpillAbsent, and the wire surface's blob_shared rows), and the harness counts
# it while the rest of the surface passes: `wire pass N/N +Ma`. The gate runs
# the C# leg's table surfaces alone and requires that line. Its control builds
# the driver from a copy whose per-case absent answers are removed and requires
# the wire surface RED: a case with no answer at all is a failure, never a pass.
CS_ABSENT := build/conformance-cs-absent
.PHONY: conformance-cs-absent conformance-cs-absent-negative-control
conformance-cs-absent: build-conformance-cs build/conformance-harness
	@mkdir -p $(CS_ABSENT)
	@printf 'cs %s test/conformance/cs/bin/Debug/net10.0/schemaconformance.dll\n' "$(DOTNET)" > $(CS_ABSENT)/drivers
	./build/conformance-harness run --drivers $(CS_ABSENT)/drivers --work $(CS_ABSENT)/run > $(CS_ABSENT)/log 2>&1 || { cat $(CS_ABSENT)/log; exit 1; }
	@grep -Eq '^wire +pass [0-9]+/[0-9]+ \+[0-9]+a' $(CS_ABSENT)/log || \
		{ cat $(CS_ABSENT)/log; echo "conformance-cs-absent FAILED: the C# wire surface counts no per-case absent"; exit 1; }
	@grep -E '^wire ' $(CS_ABSENT)/log

conformance-cs-absent-negative-control: build-conformance-cs build/conformance-harness
	@rm -rf $(CS_ABSENT)-nc && mkdir -p $(CS_ABSENT)-nc/proj
	@cp -R test/conformance/cs/src test/conformance/cs/schemaconformance.csproj $(CS_ABSENT)-nc/proj/
	@sed -e 's|File.WriteAllBytes(Path.Combine(outDir, name + ".absent"), new byte\[0\]);|/* SABOTAGED: no per-case answer */|' \
	     -e 's|File.WriteAllText(Path.Combine(outDir, f\[1\] + ".absent"), "blob_shared");|/* SABOTAGED: no per-case answer */|' \
		test/conformance/cs/src/Program.cs > $(CS_ABSENT)-nc/proj/src/Program.cs
	@test $$(grep -c 'SABOTAGED: no per-case answer' $(CS_ABSENT)-nc/proj/src/Program.cs) = 2 || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage did not patch both absent writes"; exit 1; }
	@$(DOTNET) build $(CS_ABSENT)-nc/proj --nologo -v quiet -p:TablesGeneratedDir=$(CURDIR)/build/tables-generated-cs \
		-p:CardsGeneratedDir=$(CURDIR)/build/tables-generated-cs/cards -o $(CS_ABSENT)-nc/checker > $(CS_ABSENT)-nc/build.log 2>&1 || \
		{ cat $(CS_ABSENT)-nc/build.log; echo "NEGATIVE CONTROL FAILED: the sabotaged driver does not compile"; exit 1; }
	@printf 'cs %s %s/checker/schemaconformance.dll\n' "$(DOTNET)" "$(CS_ABSENT)-nc" > $(CS_ABSENT)-nc/drivers
	@if ./build/conformance-harness run --drivers $(CS_ABSENT)-nc/drivers --work $(CS_ABSENT)-nc/run > $(CS_ABSENT)-nc/log 2>&1; then \
		cat $(CS_ABSENT)-nc/log; echo "NEGATIVE CONTROL FAILED: a case with no answer left the C# wire surface green"; exit 1; \
	fi
	@grep -Eq '^wire +FAIL' $(CS_ABSENT)-nc/log || \
		{ cat $(CS_ABSENT)-nc/log; echo "NEGATIVE CONTROL FAILED: the run went red, but not on the wire surface"; exit 1; }
	@grep -E '^wire ' $(CS_ABSENT)-nc/log
	@echo "negative control: removing the C# driver's per-case absent answers turns the wire surface red"

test-cs: conformance-cs-absent

generated/bench/tables/cs/.stamp: bin/schema bench/corpus/BenchTable.schema
	@mkdir -p generated/bench/tables/cs
	./bin/schema generate --lang cs --out generated/bench/tables/cs bench/corpus/BenchTable.schema
	@touch $@

# CI discovers explicit generated/ stamps; make test reaches this through test-cs.
generated/bench/paired/cs/.stamp: bin/schema bench/corpus/Bench.schema bench/corpus/FixedTable.schema
	@mkdir -p generated/bench/paired/cs
	./bin/schema generate --lang cs --out generated/bench/paired/cs bench/corpus/Bench.schema bench/corpus/FixedTable.schema
	@touch $@

generated/bench/cs/.stamp: bin/schema $(SCHEMAS_BENCH)
	./bin/schema generate --lang cs --out generated/bench/cs bench/corpus/Bench.schema
	./bin/schema generate --lang cs --out generated/bench/cs/realworld bench/corpus/RealWorld.schema
	@touch $@

.PHONY: build-conformance-cs
build-conformance-cs: build/tables-generated-cs/.stamp cards-cs-generate
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
.PHONY: tables-cs-view tables-cs-leg tables-cs-leg-debug tables-cs-leg-release tables-cs-wire-fuzz tables-cs-region-fuzz tables-cs-builder-fuzz tables-cs-retain-fuzz
tables-cs-view: build/tables-generated-cs/.stamp
	@mkdir -p build/view-cs
	@set -e; for entry in $(VIEW_CORPUS); do \
		dir=$${entry%%:*}; pkg=$${entry##*:}; \
		cap=$$(printf '%s' "$$pkg" | cut -c1 | tr 'a-z' 'A-Z')$$(printf '%s' "$$pkg" | cut -c2-); \
		source=tables/$$dir; \
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

# TWO CONFIGURATIONS, TWO NAMES, AND A COMBINED ONE — because the pair was 74 s
# and the owner's rule is one to two minutes for anything a child iterates on.
# Debug alone is about half that, so `make tables-cs-leg-debug` is the loop and
# `make tables-cs-leg` remains the full-CI and release gate, running both.
# Fast CI uses Debug with versioning to stay within its two-minute budget.
tables-cs-leg:
	$(MAKE) tables-cs-leg-debug
	$(MAKE) tables-cs-leg-release

tables-cs-leg-debug: build/tables-generated-cs/.stamp
	cd test/cs-tables && $(DOTNET) run

tables-cs-leg-release: build/tables-generated-cs/.stamp
	cd test/cs-tables && $(DOTNET) run -c Release

# THE LEG'S NEGATIVE CONTROLS (docs/PORTING.md M5, M6, M10, M14, M17 and M19,
# and the native region writer's shared-blob refusal, docs/SPEC-TABLES.md §2.5).
# Each takes ONE rule out of the C# emitter (tools/sabotage, through a go build
# -overlay, so no tracked file is written), regenerates every unit the leg
# compiles from that compiler into a tree of its own, builds the leg against it
# in its own obj and bin, and requires the leg to go red ON THAT RULE'S CHECK. A
# sabotage that does not compile is not a control that went red, and the
# control says so rather than passing.
#   $(1) the control's name, $(2) the emitter file, $(3) the sabotage,
#   $(4) the leg's failure text it requires (grep -E)
define cs_leg_sabotage
	@rm -rf build/cs-leg-$(1) && mkdir -p build/cs-leg-$(1)
	@go run ./tools/sabotage -name $(3) -out build/cs-leg-$(1)/source.gotext internal/codegen/cstable/$(2)
	@printf '{"Replace":{"%s/internal/codegen/cstable/$(2)":"%s/build/cs-leg-$(1)/source.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/cs-leg-$(1)/overlay.json
	@go build -overlay=build/cs-leg-$(1)/overlay.json -o build/cs-leg-$(1)/schema ./cmd/schema
	$(call tables_generate_cs,./build/cs-leg-$(1)/schema,build/cs-leg-$(1)/gen)
	@( cd test/cs-tables && $(DOTNET) build -v q --nologo -p:TablesGeneratedDir=$(CURDIR)/build/cs-leg-$(1)/gen \
		-p:BaseOutputPath=$(CURDIR)/build/cs-leg-$(1)/bin/ -p:BaseIntermediateOutputPath=$(CURDIR)/build/cs-leg-$(1)/obj/ ) \
		> build/cs-leg-$(1)/build.log 2>&1 || \
		{ cat build/cs-leg-$(1)/build.log; echo "NEGATIVE CONTROL FAILED: the $(1) sabotage does not compile"; exit 1; }
	@if ( cd test/cs-tables && $(DOTNET) $(CURDIR)/build/cs-leg-$(1)/bin/Debug/net10.0/schematablestest.dll ) \
		> build/cs-leg-$(1)/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: tables-cs-leg stayed green with the $(1) rule removed"; exit 1; \
	fi
	@grep -Eq '$(4)' build/cs-leg-$(1)/log || \
		{ tail -40 build/cs-leg-$(1)/log; echo "NEGATIVE CONTROL FAILED: tables-cs-leg went red, but not on the $(1) rule"; exit 1; }
	@grep -Em3 '$(4)' build/cs-leg-$(1)/log
	@echo "negative control: removing the $(1) rule turns tables-cs-leg red on its own check"
endef

.PHONY: tables-cs-leg-keyed-negative-control tables-cs-leg-identity-negative-control tables-cs-leg-allocator-negative-control \
	tables-cs-leg-label-negative-control tables-cs-leg-blob-span-negative-control tables-cs-leg-map-order-negative-control \
	tables-cs-leg-region-blob-shared-negative-control
tables-cs-leg-keyed-negative-control: bin/schema
	$(call cs_leg_sabotage,keyed,cstable.go,table-cs-keyed-none-only,FAILED: keyed indexer: past Max|IndexOutOfRangeException)
tables-cs-leg-identity-negative-control: bin/schema
	$(call cs_leg_sabotage,identity,variable.go,table-cs-identity-map,FAILED: list_shared (measured wire length|re-encodes reference bytes))
tables-cs-leg-allocator-negative-control: bin/schema
	$(call cs_leg_sabotage,allocator,allocator.go,table-cs-allocator-free,FAILED: native file numbering uses balanced caller hooks)
tables-cs-leg-label-negative-control: bin/schema
	$(call cs_leg_sabotage,label,variablejson.go,table-cs-text-label,FAILED: list_shared text)
tables-cs-leg-blob-span-negative-control: bin/schema
	$(call cs_leg_sabotage,blob-span,builder.go,table-cs-blob-span,FAILED: large blob keeps its bytes after later allocations)
tables-cs-leg-map-order-negative-control: bin/schema
	$(call cs_leg_sabotage,map-order,buildercollections.go,table-cs-map-insert-order,FAILED: map iteration is ordered by signed key)
tables-cs-leg-region-blob-shared-negative-control: bin/schema
	$(call cs_leg_sabotage,region-blob-shared,regionwrite.go,table-cs-region-blob-shared,FAILED: blob_shared native region writer refuses the shared blob)

# THE C# PER-PATH ALLOCATION GATE (docs/PORTING.md I1, docs/SPEC-TABLES.md
# "What allocates, and what never does"). C# joins Go's testing.AllocsPerRun
# and Java's getCurrentThreadAllocatedBytes with the runtime's own per-thread
# counter, GC.GetAllocatedBytesForCurrentThread. Setup (the golden, the value,
# the save buffer and the caller-supplied TableReport) is separated from steady
# work (the replay loop over that storage); the read with a supplied report,
# the read with NO report, Measure, Save and the round trip must each measure
# exactly zero, and the leg's own sensitivity check plants escapes on the read
# row and on the save row and requires each installed row to move while the
# row beside it stays green. The generator's null-report read caches one
# ignored TableReport rather than constructing one per call.
.PHONY: tables-cs-alloc
tables-cs-alloc: build/tables-generated-cs/.stamp
	cd test/cs-tables && SCHEMA_CS_ALLOC_PIN="$(CS_ALLOC_PIN)" $(DOTNET) run -c Release -- alloc

# THE RUNTIME PIN OF THE ALLOCATION GATE (docs/PORTING.md I14, issue #420): the
# SDK pin in .github/dotnet-version, read here and passed in, so C# holds no
# second copy. The gate refuses to measure on a runtime of another major.
CS_ALLOC_PIN := $(shell cat .github/dotnet-version 2>/dev/null)

# The control: a pin of a major this machine is not running (the running major
# plus one) must be refused with the refusal text, a non-zero exit and no
# passed line; the escape hatch must exit 0, print NOT CERTIFIED and never the
# passed line.
.PHONY: tables-cs-alloc-runtime-negative-control
tables-cs-alloc-runtime-negative-control: build/tables-generated-cs/.stamp
	@set -e; cd test/cs-tables; out=../../build/cs-alloc-runtime-control; mkdir -p $$out; \
	$(DOTNET) build -c Release --nologo -v quiet; \
	run="$(DOTNET) run -c Release --no-build --"; \
	major=`$(DOTNET) --list-runtimes | sed -n 's/^Microsoft.NETCore.App \([0-9]*\)\..*/\1/p' | sort -n | tail -1`; \
	wrong=`expr $$major + 1`; \
	if SCHEMA_CS_ALLOC_PIN=$$wrong.0 $$run alloc > $$out/refused.txt 2>&1; then \
		cat $$out/refused.txt; echo "cs alloc runtime control: a wrong pin ($$wrong.0) exited 0"; exit 1; fi; \
	grep -q 'REFUSED to measure' $$out/refused.txt && grep -q "pin $$wrong.0" $$out/refused.txt && grep -q 'major' $$out/refused.txt || { cat $$out/refused.txt; echo "cs alloc runtime control: no refusal naming both versions"; exit 1; }; \
	if grep -q 'alloc gate passed' $$out/refused.txt; then echo "cs alloc runtime control: the refused run printed the passed line"; exit 1; fi; \
	SCHEMA_CS_ALLOC_PIN=$$wrong.0 SCHEMA_CS_ALLOC_ANY_RUNTIME=1 $$run alloc > $$out/hatch.txt 2>&1 || { cat $$out/hatch.txt; echo "cs alloc runtime control: the escape hatch did not exit 0"; exit 1; }; \
	grep -q 'NOT CERTIFIED' $$out/hatch.txt || { cat $$out/hatch.txt; echo "cs alloc runtime control: the escape hatch did not print NOT CERTIFIED"; exit 1; }; \
	if grep -q 'alloc gate passed' $$out/hatch.txt; then cat $$out/hatch.txt; echo "cs alloc runtime control: the escape hatch printed the passed line"; exit 1; fi; \
	tail -n 2 $$out/refused.txt; tail -n 2 $$out/hatch.txt; \
	echo "tables-cs-alloc-runtime-negative-control: a wrong pin is refused, the escape hatch observes and does not certify"

# THE C# TABLES SOAK (docs/PORTING.md I9, issue #416): loops Load with a
# supplied report, Load with no report, Measure, Save and the round trip over
# the tabledemo root for TABLES_CS_SOAK_ITERATIONS passes (a count, not a
# clock: about an hour at release, the short form in `make test`), reads
# GC.GetAllocatedBytesForCurrentThread at every sample, and fails if the byte
# count moves after warmup, reporting managed-heap and working-set drift beside it as the weaker instrument that sees retention.
# Its negative control plants a matched short-lived allocation per iteration
# (allocated and dropped so the GC reclaims it) and requires the count gate to
# go red while the managed-heap drift line stays green.
.PHONY: tables-cs-soak tables-cs-soak-negative-control
tables-cs-soak: build/tables-generated-cs/.stamp
	cd test/cs-tables && SCHEMA_CS_ALLOC_PIN="$(CS_ALLOC_PIN)" $(DOTNET) run -c Release -- soak $(TABLES_CS_SOAK_ITERATIONS)

tables-cs-soak-negative-control: build/tables-generated-cs/.stamp
	@mkdir -p build/cs-soak-control
	@cd test/cs-tables && $(DOTNET) build -c Release --nologo -v quiet
	@if ( cd test/cs-tables && SCHEMA_CS_ALLOC_PIN="$(CS_ALLOC_PIN)" SCHEMA_CS_SOAK_PLANT=1 $(DOTNET) run -c Release --no-build -- soak 65536 ) > build/cs-soak-control/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a short-lived allocation per iteration left the C# soak green"; \
		cat build/cs-soak-control/log; exit 1; \
	fi
	@grep -Fq "SOAK FAILED: allocated-bytes count moved" build/cs-soak-control/log || \
		{ echo "NEGATIVE CONTROL FAILED: the C# soak went red, but not on the allocated-bytes count"; cat build/cs-soak-control/log; exit 1; }
	@grep -Fq "drift gate PASS" build/cs-soak-control/log || \
		{ echo "NEGATIVE CONTROL FAILED: the managed-heap drift line did not stay green"; cat build/cs-soak-control/log; exit 1; }
	@grep -E "managed-heap drift|SOAK FAILED" build/cs-soak-control/log
	@echo "negative control: a matched short-lived allocation per iteration is INVISIBLE to managed-heap drift and turns the C# allocated-bytes COUNT red"

# THE FIXED FORM'S VERSIONING SUITE ON THIS LEG (docs/FIXED-FORM-ALGORITHM.md §5,
# docs/FIXED-FORM-VERSIONING-TESTS.md): every row of the law, both read columns,
# against THE C++ REFERENCE'S OWN BYTES.
#
# THIS TARGET EXISTS BECAUSE `go test ./...` ASSERTS NOTHING HERE. The harness
# SKIPS itself when build/fixedform-corpus is absent, which is right for a bare
# `go test ./...` on a tree that never built the oracle — and is exactly how a §5
# regression would ride into CI green. So the target BUILDS THE ORACLE FIRST
# (`tables-fixedform-corpus`, the reference's own dump, about 25 s) and then sets
# SCHEMA_REQUIRE_CORPUS=1, which turns that skip into a FAILURE: under this name a
# missing corpus can never pass silently.
#
# ONE ASSEMBLY, ONE BUILD, ONE RUN. The harness generates fifty-seven probes —
# one per row per COLUMN (§5.9 #18) — into ONE project, each in the namespace its
# own schema's package gives it, and runs the lot once. Fifty-seven `dotnet run`s
# would cost minutes; this costs one build.
#
# EVERY ROW IS GREEN, `array_elem_widen` INCLUDED. It was the one named red here
# (§5.9 #31): the C# leg FOLDED a flat element run into one plan entry, and a fold
# whose two images differ in WIDTH has no element-wise destination to widen into,
# so the element widen was dropped and `widened` stayed zero. 157268a6 stopped
# folding a widened run — §5.2's EMIT has no fold at all — and the row asserts the
# reference's EXACT four since (csVersionRow.widened).
.PHONY: tables-cs-versioning
tables-cs-versioning: tables-fixedform-corpus
	DOTNET="$(DOTNET)" SCHEMA_REQUIRE_CORPUS=1 go test ./internal/codegen/cstable/ -count=1 -timeout 20m -run 'TestFixedVersioning'
	@echo 'tables C# versioning: §5 read both columns of every row against the C++ reference bytes'

.PHONY: tables-cs-fixed-t13
tables-cs-fixed-t13:
	DOTNET="$(DOTNET)" SCHEMA_REQUIRE_CORPUS=1 go test ./internal/codegen/cstable/ -count=1 -timeout 20m -run 'TestFixedRoadmapCSharpT13SecondLayoutForAHeldHash'
	@echo 'tables C# fixed-t13: cs/F12 a second layout for a held hash is refused layout_malformed'

.PHONY: tables-cs-fixed-t18
tables-cs-fixed-t18:
	DOTNET="$(DOTNET)" SCHEMA_REQUIRE_CORPUS=1 go test ./internal/codegen/cstable/ -count=1 -timeout 600s -v -run 'TestFixedRoadmapCSharpT18UnionArms'
	@echo 'tables C# fixed-t18: direct and payload-free union arms round-trip, tag-only arm storage'

test-cs: tables-cs-fixed-t18
test-cs: tables-cs-fixed-rows

# I13 (docs/PORTING.md): the C# leg's text differential against a third
# implementation, slug json-differential. A C# probe (Roslyn csc + dotnet exec,
# the t10 harness's build, with the sibling serialize.cs for the nested plain
# type) fills 200 random float-only instances from a splitmix64 and writes each
# as (wire, text); internal/tabletext reads the wire and its text is
# byte-compared against the leg's, then the leg's text is packed back and
# byte-compared against the leg's wire. The control caps the emitted walker's
# float32 at six significant digits in memory and must go red on the text
# comparison. Its first run found the walker writing a float64 at fifteen
# digits through .NET's custom format cap (json.go FormatG).
.PHONY: tables-cs-json-differential tables-cs-json-differential-negative-control
tables-cs-json-differential:
	DOTNET="$(DOTNET)" SCHEMA_REQUIRE_CORPUS=1 sh test/slowgate/proof tables-cs-json-differential \
		'TestCsTextDifferential' \
		./internal/codegen/cstable -run '^TestCsTextDifferential$$' -count=1 -timeout 600s
	@grep -h 'cs text differential: ' build/slowgate/tables-cs-json-differential.log | sed 's/^ *//'
tables-cs-json-differential-negative-control:
	DOTNET="$(DOTNET)" SCHEMA_REQUIRE_CORPUS=1 sh test/slowgate/proof tables-cs-json-differential-negative-control \
		'TestCsTextDifferentialNegativeControl' \
		./internal/codegen/cstable -run '^TestCsTextDifferentialNegativeControl$$' -count=1 -timeout 600s
	@grep -h -A1 'cs text differential negative control: \|leg text and engine text differ' build/slowgate/tables-cs-json-differential-negative-control.log | sed 's/^ *//' | head -3
test-cs: tables-cs-json-differential tables-cs-json-differential-negative-control

.PHONY: tables-cs-fixed-roadmap
tables-cs-fixed-roadmap:
	DOTNET="$(DOTNET)" SCHEMA_REQUIRE_CORPUS=1 go test ./internal/codegen/cstable/ -count=1 -timeout 20m -run 'TestFixedRoadmapCSharpT12PlansVersions'
	@echo 'tables C# fixed-roadmap: t12 plan selection, closure, retirement, retired runtime and widening'

# THE C# FIXED-ROADMAP ROWS RUN UNDER ONE REQUIRED TARGET, and the held
# message-form ledger steps close against it. Every C# fixed-roadmap test and
# the held message form run once here with DOTNET and
# SCHEMA_REQUIRE_CORPUS=1, so a bare `go test` can never report green over C#
# that never ran. The oracle is built first (tables-fixedform-corpus) because
# t01's framing rows read the C++ reference's bytes; under
# SCHEMA_REQUIRE_CORPUS=1 a missing corpus is a failure, not a skip. The
# -run pattern is pinned by TestCsFixedRowsTargetCoversEveryRoadmapTest: any
# row added later without being named here goes red. Owed-gap skips that stay
# skips (not toolchain absences) are cs/W16, cs/R28 (fixedroadmap_t07_test.go),
# message_graph_tree and connection_retain_conn/connection_graph_conn (no leg
# links the sibling ../serialize.cs runtime yet; messageform_held_test.go);
# -v keeps every skip in this target's log, and they are named in the pull
# request.
.PHONY: tables-cs-fixed-rows
tables-cs-fixed-rows: tables-fixedform-corpus
	DOTNET="$(DOTNET)" SCHEMA_REQUIRE_CORPUS=1 go test ./internal/codegen/cstable/ -count=1 -timeout 600s -v -run 'TestFixedRoadmap(T01Framing|T02Plans|CSharpT03PlansVersions|CSharpT04Versions|CSharpT05Evolution|CSharpT06Bounds|CSharpT07Writes|CSharpT08ConformanceIntegers|CSharpT09FloatsUnions|CSharpT10Optionals|CSharpT12PlansVersions|CSharpT13SecondLayoutForAHeldHash|CSharpT14CompressedFloats|CSharpT15GuardChain|CSharpT18ExactCounters|CSharpT18UnionArms)|TestMessageFormHeldCSharp'
	@echo 'tables C# fixed-rows: every fixed-roadmap row and the held message form ran once under one required target'

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

# THE FINITE RULE'S CONTROL (docs/SPEC-TABLES.md §3.3): the form-1 read's
# raw-bits test, the fixed read's finite pass and the form-1 writer's refusal,
# each sabotaged in turn through a go build -overlay, must each turn exactly
# their own vector of one probe red.
.PHONY: tables-cs-finite-float-negative-control
tables-cs-finite-float-negative-control: bin/schema
	sh test/cs-tables/finite-negative-control "$(DOTNET)"

# J1 — ACCESSOR AND DESCRIPTOR AGREEMENT (docs/PORTING.md, schema#421), the C#
# half. The generated managed class's public fields and the generated
# descriptor's delegates are two independent derivations of one layout, and a
# reading tier that only ever walks the descriptor could read it twice and never
# know. Go holds unsafe.Offsetof against f.Offset; C# has no f.Offset — its
# TableFieldInfo carries DELEGATES (§8.1), so this leg holds the named public
# field against the GetRaw/GetChild delegate and requires agreement, field by
# field, by name. A scalar accessor (codecs.go:826) and a pointer slot
# (codecs.go:814) are each sabotaged through a `go build -overlay` on the
# emitter, and each must turn the gate red on its own message.
.PHONY: tables-cs-accessor-descriptor-agreement tables-cs-accessor-negative-control tables-cs-slot-negative-control
tables-cs-accessor-descriptor-agreement: bin/schema
	sh test/cs-tables/accessor-descriptor-control "$(DOTNET)" agreement

tables-cs-accessor-negative-control: bin/schema
	sh test/cs-tables/accessor-descriptor-control "$(DOTNET)" scalar

tables-cs-slot-negative-control: bin/schema
	sh test/cs-tables/accessor-descriptor-control "$(DOTNET)" slot

test-cs: tables-cs-accessor-descriptor-agreement tables-cs-accessor-negative-control tables-cs-slot-negative-control

# THE C# PORT'S RELEASE GATE (docs/PORTING.md J3). certify.yml DERIVES this
# target by name, so the expensive half lands by adding it here and nothing
# else: the four wire-fuzz bindings at five times the shared N under a second
# seed, and the planted controls that make the leg's green mean something.
.PHONY: tables-cs-release
tables-cs-release:
	$(MAKE) tables-cs-wire-fuzz N=500000
	$(MAKE) tables-cs-wire-fuzz SEED=2 N=500000
	$(MAKE) tables-cs-region-fuzz N=500000
	$(MAKE) tables-cs-builder-fuzz N=500000
	$(MAKE) tables-cs-retain-fuzz N=500000
	$(MAKE) tables-cs-soak-negative-control
	$(MAKE) tables-cs-retain-negative-control
	$(MAKE) tables-cs-pack-negative-control
	$(MAKE) conformance-negative-control-cs
	$(MAKE) tables-cs-leg-keyed-negative-control tables-cs-leg-identity-negative-control tables-cs-leg-allocator-negative-control
	$(MAKE) tables-cs-leg-label-negative-control tables-cs-leg-blob-span-negative-control tables-cs-leg-map-order-negative-control
	$(MAKE) tables-cs-leg-region-blob-shared-negative-control conformance-cs-absent-negative-control
	$(MAKE) tables-cook-open-cs-order-negative-control tables-cs-usage-negative-control tables-cs-byte-order-negative-control

# J5 (docs/PORTING.md) — THE BENCH LEG'S GOLDEN GATE RUNS BEFORE THE CLOCK.
# The C# table leg's `--gate` verb round-trips all 64 corpus variants and
# byte-compares variant 0 against testdata/wire/bench_table.bin, refusing to
# time a codec that does not reproduce the corpus (bench/tables/README.md).
.PHONY: tables-cs-bench-gate
tables-cs-bench-gate: generated/bench/tables/cs/.stamp
	bench/tables/cs/leg build
	bench/tables/cs/leg run --gate
test-cs: tables-cs-bench-gate

# THE GENERATED C# IS FORMAT-CANONICAL AND ANALYZER-CLEAN (issues #424 and #547;
# docs/CS-TABLES.md, "The generated C# dialect"). `dotnet format` is the
# language's formatting authority and the Roslyn analyzers the SDK ships
# (Microsoft.CodeAnalysis.NetAnalyzers) are its linter, so an emitter that drifts
# from what they accept goes red here rather than on a reviewer.
#
# "Default strictness" is exactly CS_ANALYSIS_PROPS: the SDK's analyzers on, the
# latest analysis level, the Default mode, and no suppression file and no
# .editorconfig beside the generated units. test/cs/schematest.csproj and
# test/conformance/cs/schemaconformance.csproj carry the same three properties;
# the other packet projects carry none, so the gate hands the set to every
# project it checks, both to `dotnet format` (MSBuild reads an environment
# variable as a property) and to the build. A finding is fixed in the emitter
# (internal/codegen/csharp, internal/codegen/cstable), never silenced.
#
# cs_clean_check runs BOTH halves and reports each by name before it fails, so
# one red never hides the other: `dotnet format --verify-no-changes`
# (whitespace, style and analyzers at their default severity; a finding prints
# as `error WHITESPACE` or `error <rule id>` with its file), then a full rebuild
# with the analyzer set on and TreatWarningsAsErrors, so a compiler or analyzer
# warning (CS0169 for an unused private field, CA<n> for an analyzer finding)
# turns it red. $(1) the gate's label, $(2) the project, $(3) the generated
# directories formatted, $(4) extra NAME=VALUE properties (the negative
# control's redirect).
#
# --include takes each directory RELATIVE to the repository and WITH a trailing
# slash. dotnet format matches it as a glob against the project's documents,
# and an absolute directory path matches none of them and passes having checked
# nothing: that is how tables-cs-clean stood green at d713c7d66 over 295 table
# units that are not format-canonical (issue #547's census). The planted
# control below is what keeps this form honest.
CS_ANALYSIS_PROPS := EnableNETAnalyzers=true AnalysisLevel=latest AnalysisMode=Default

define cs_clean_check
	@mkdir -p build/cs-clean/$(1); st=0; \
	if env $(CS_ANALYSIS_PROPS) $(4) $(DOTNET) format $(2) --verify-no-changes \
		--include $(addsuffix /,$(3)) > build/cs-clean/$(1)/format.log 2>&1; then :; else \
		st=1; echo "RED $(1): dotnet format --verify-no-changes over $(3)"; \
		grep -E ' (error|warning) [A-Z]+[A-Z0-9]*:' build/cs-clean/$(1)/format.log || cat build/cs-clean/$(1)/format.log; \
	fi; \
	if $(DOTNET) build $(2) --no-incremental --nologo -v q -o build/cs-clean/$(1)/bin \
		$(addprefix -p:,$(CS_ANALYSIS_PROPS) $(4)) -p:TreatWarningsAsErrors=true \
		> build/cs-clean/$(1)/build.log 2>&1; then :; else \
		st=1; echo "RED $(1): dotnet build with $(CS_ANALYSIS_PROPS)"; \
		grep -E ' (error|warning) [A-Z]+[0-9]+:' build/cs-clean/$(1)/build.log | sort -u | grep . || tail -20 build/cs-clean/$(1)/build.log; \
	fi; \
	test $$st -eq 0 && echo "$(1): dotnet format and the analyzers clean over $(3)"
endef

# The SDK probe both gates share: a dotnet not on PATH SKIPS cleanly, and under
# SCHEMA_REQUIRE_CORPUS=1 that skip is a FAILURE (the corpus legs' idiom).
define cs_clean_probe
	@if ! command -v $(DOTNET) >/dev/null 2>&1; then \
		if [ "$(SCHEMA_REQUIRE_CORPUS)" = 1 ]; then \
			echo "FAIL $(1): $(DOTNET) is not installed and SCHEMA_REQUIRE_CORPUS=1"; exit 1; fi; \
		echo "SKIP $(1): $(DOTNET) is not installed"; exit 0; \
	fi
endef

# The generated TABLE units the conformance project compiles (issue #424).
.PHONY: tables-cs-clean
tables-cs-clean: build/tables-generated-cs/.stamp cards-cs-generate
	$(call cs_clean_probe,tables-cs-clean)
	@if command -v $(DOTNET) >/dev/null 2>&1; then $(MAKE) --no-print-directory tables-cs-clean-run; fi

.PHONY: tables-cs-clean-run
tables-cs-clean-run:
	$(call cs_clean_check,tables-cs-clean,test/conformance/cs/schemaconformance.csproj,build/tables-generated-cs)

# REQUIRED (issue #547, C# tables half). The table emitters in
# internal/codegen/cstable write what dotnet format writes: 152,044 WHITESPACE
# findings in 295 table units at the gate's fix are zero, fixed in the emitters
# and never by reformatting the output, so tables-cs-clean is a prerequisite of
# test-cs like every other C# gate.
test-cs: tables-cs-clean

# THE TABLES-CLEAN NEGATIVE CONTROL: a copy of the generated table units with ONE
# unit (a1/A1Table.cs) carrying one planted whitespace violation, a statement
# indented two columns past its block, compiled by the same project through its
# TablesGeneratedDir redirect. The gate must go red on it BY NAME: dotnet
# format's WHITESPACE finding in A1Table.cs. A gate that stayed green, or went
# red for another reason, fails the control.
TABLES_CS_CLEAN_NEG := build/tables-cs-clean-negative
.PHONY: tables-cs-clean-negative-control
tables-cs-clean-negative-control: build/tables-generated-cs/.stamp cards-cs-generate
	$(call cs_clean_probe,tables-cs-clean-negative-control)
	@if command -v $(DOTNET) >/dev/null 2>&1; then $(MAKE) --no-print-directory tables-cs-clean-negative-control-run; fi

.PHONY: tables-cs-clean-negative-control-run
tables-cs-clean-negative-control-run:
	@rm -rf $(TABLES_CS_CLEAN_NEG) && mkdir -p $(TABLES_CS_CLEAN_NEG)
	@cp -R build/tables-generated-cs $(TABLES_CS_CLEAN_NEG)/cs
	@awk 'done != 1 && $$0 ~ /^            if \(value == null\) return;$$/ { print "              if (value == null) return;"; done = 1; next } { print }' \
		build/tables-generated-cs/a1/A1Table.cs > $(TABLES_CS_CLEAN_NEG)/cs/a1/A1Table.cs
	@grep -q '^              if (value == null) return;$$' $(TABLES_CS_CLEAN_NEG)/cs/a1/A1Table.cs || \
		{ echo 'NEGATIVE CONTROL FAILED: the plant found no statement to misindent in a1/A1Table.cs'; exit 1; }
	@if $(MAKE) --no-print-directory tables-cs-clean-negative-run > $(TABLES_CS_CLEAN_NEG)/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: tables-cs-clean stayed green over a planted violation'; cat $(TABLES_CS_CLEAN_NEG)/log; exit 1; fi
	@grep -Eq 'A1Table\.cs.* error WHITESPACE:' $(TABLES_CS_CLEAN_NEG)/log || \
		{ echo 'NEGATIVE CONTROL FAILED: the misindented statement was not named'; cat $(TABLES_CS_CLEAN_NEG)/log; exit 1; }
	@if grep -E ' error WHITESPACE:' $(TABLES_CS_CLEAN_NEG)/log | grep -qv 'A1Table\.cs'; then \
		echo 'NEGATIVE CONTROL FAILED: a finding outside the planted unit'; cat $(TABLES_CS_CLEAN_NEG)/log; exit 1; fi
	@if grep -q 'RED tables-cs-clean-negative: dotnet build' $(TABLES_CS_CLEAN_NEG)/log; then \
		echo 'NEGATIVE CONTROL FAILED: the build half went red, not the format half'; cat $(TABLES_CS_CLEAN_NEG)/log; exit 1; fi
	@echo 'tables C# clean negative control: a misindented statement in A1Table.cs turns tables-cs-clean red by name'

.PHONY: tables-cs-clean-negative-run
tables-cs-clean-negative-run:
	$(call cs_clean_check,tables-cs-clean-negative,test/conformance/cs/schemaconformance.csproj,$(TABLES_CS_CLEAN_NEG)/cs,TablesGeneratedDir=$(CURDIR)/$(TABLES_CS_CLEAN_NEG)/cs)
test-cs: tables-cs-clean-negative-control

# The generated PACKET units (internal/codegen/csharp, issue #547): the
# examples and the bench corpus (schematest), the 128-bit examples (the
# ludicrous twin), the RealWorld bench unit (the bench project), and the
# packet-defaults and packet-text units, each through the project
# that compiles it.
PACKET_CLEAN_CS_STAMPS := generated/cs/.stamp generated/bench/cs/.stamp generated/cs-ludicrous/.stamp \
	build/packet-defaults/cs/.stamp build/packet-text/cs/.stamp

.PHONY: packet-clean-cs
packet-clean-cs: $(PACKET_CLEAN_CS_STAMPS)
	$(call cs_clean_probe,packet-clean-cs)
	@if command -v $(DOTNET) >/dev/null 2>&1; then $(MAKE) --no-print-directory packet-clean-cs-run; fi

.PHONY: packet-clean-cs-run
packet-clean-cs-run:
	@st=0; \
	$(MAKE) --no-print-directory packet-clean-cs-unit-examples || st=1; \
	$(MAKE) --no-print-directory packet-clean-cs-unit-ludicrous || st=1; \
	$(MAKE) --no-print-directory packet-clean-cs-unit-realworld || st=1; \
	$(MAKE) --no-print-directory packet-clean-cs-unit-defaults || st=1; \
	$(MAKE) --no-print-directory packet-clean-cs-unit-text || st=1; \
	$(MAKE) --no-print-directory packet-clean-cs-unit-wide || st=1; \
	if [ $$st -ne 0 ]; then echo "RED packet-clean-cs: a generated C# packet unit is not format- and analyzer-clean"; exit 1; fi
	@echo "packet C#: dotnet format and the analyzers clean over the generated packet units"

.PHONY: packet-clean-cs-unit-examples packet-clean-cs-unit-ludicrous packet-clean-cs-unit-realworld \
	packet-clean-cs-unit-defaults packet-clean-cs-unit-text packet-clean-cs-unit-wide
packet-clean-cs-unit-examples:
	$(call cs_clean_check,packet-clean-cs-examples,test/cs/schematest.csproj,generated/cs generated/bench/cs)
packet-clean-cs-unit-ludicrous:
	$(call cs_clean_check,packet-clean-cs-ludicrous,test/cs-ludicrous/ludicroustest.csproj,generated/cs-ludicrous)
packet-clean-cs-unit-realworld:
	$(call cs_clean_check,packet-clean-cs-realworld,bench/cs/schemabench.csproj,generated/bench/cs/realworld)
packet-clean-cs-unit-defaults:
	$(call cs_clean_check,packet-clean-cs-defaults,test/packet-defaults/cs/packet-defaults.csproj,build/packet-defaults/cs)
packet-clean-cs-unit-text:
	$(call cs_clean_check,packet-clean-cs-text,test/packet-text/cs/packet-text.csproj,build/packet-text/cs)
test-cs: packet-clean-cs

# THE PACKET-CLEAN NEGATIVE CONTROL: a copy of the generated examples with ONE
# unit (Constants.cs) carrying two planted violations, a misindented block and
# an unused private field, compiled by the same project through its
# PacketGeneratedDir redirect. The gate must go red on BOTH by name: dotnet
# format's WHITESPACE finding in Constants.cs, and the build's CS0169 on the
# planted field. A gate that stayed green, or went red for another reason,
# fails the control.
PACKET_CLEAN_CS_NEG := build/packet-clean-cs-negative
.PHONY: packet-clean-cs-negative-control
packet-clean-cs-negative-control: generated/cs/.stamp generated/bench/cs/.stamp
	@rm -rf $(PACKET_CLEAN_CS_NEG) && mkdir -p $(PACKET_CLEAN_CS_NEG)/cs
	@cp generated/cs/*.cs $(PACKET_CLEAN_CS_NEG)/cs/
	@awk 'done != 1 && prev ~ /public static partial class Schema/ && $$0 ~ /^    \{$$/ { \
		print; print "        private static int plantedUnusedField;"; \
		print "  private static class PlantedMisindentedBlock"; print "  {"; \
		print "  public const int Planted = 1;"; print "  }"; done = 1; next } \
		{ prev = $$0; print }' generated/cs/Constants.cs > $(PACKET_CLEAN_CS_NEG)/cs/Constants.cs
	@grep -q plantedUnusedField $(PACKET_CLEAN_CS_NEG)/cs/Constants.cs || \
		{ echo 'NEGATIVE CONTROL FAILED: the plant found no Schema class in Constants.cs'; exit 1; }
	@if $(MAKE) --no-print-directory packet-clean-cs-negative-run > $(PACKET_CLEAN_CS_NEG)/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: packet-clean-cs stayed green over a planted violation'; cat $(PACKET_CLEAN_CS_NEG)/log; exit 1; fi
	@grep -Eq 'Constants\.cs.* error WHITESPACE:' $(PACKET_CLEAN_CS_NEG)/log || \
		{ echo 'NEGATIVE CONTROL FAILED: the misindented block was not named'; cat $(PACKET_CLEAN_CS_NEG)/log; exit 1; }
	@grep -Eq 'Constants\.cs.* error CS0169:.*plantedUnusedField' $(PACKET_CLEAN_CS_NEG)/log || \
		{ echo 'NEGATIVE CONTROL FAILED: the unused private field was not named'; cat $(PACKET_CLEAN_CS_NEG)/log; exit 1; }
	@echo 'packet C# clean negative control: a misindented block and an unused private field turn packet-clean-cs red by name'

.PHONY: packet-clean-cs-negative-run
packet-clean-cs-negative-run:
	$(call cs_clean_check,packet-clean-cs-negative,test/cs/schematest.csproj,$(PACKET_CLEAN_CS_NEG)/cs,PacketGeneratedDir=$(CURDIR)/$(PACKET_CLEAN_CS_NEG)/cs)
test-cs: packet-clean-cs-negative-control

.PHONY: tables-cs-fixedform-arm-negative-control
tables-cs-fixedform-arm-negative-control: bin/schema
	sh test/cs-tables/armtext-negative-control "$(DOTNET)"

# THE C# LEG of `make test`: the table gates and the C# conformance negative
# control, the cook-open gates on the C# side, the bench units' compile gates
# (a unit that generates but does not compile is issue #80's lesson), and the
# packet tests.
.PHONY: test-cs
test-cs: toolchain-cs build/tables-generated-cs/.stamp generated/bench/tables/cs/.stamp generated/bench/paired/cs/.stamp generated/cs/.stamp generated/cs-ludicrous/.stamp generated/bench/cs/.stamp
	$(MAKE) tables-cs-json-walk
	$(MAKE) tables-cs-standalone
	$(MAKE) tables-cs-variable-surface
	$(MAKE) tables-cs-view
	$(MAKE) tables-cs-leg
	$(MAKE) tables-cs-alloc
	$(MAKE) tables-cs-alloc-runtime-negative-control
	$(MAKE) tables-cs-soak TABLES_CS_SOAK_ITERATIONS=1048576
	$(MAKE) tables-cs-soak-negative-control
	$(MAKE) tables-cs-versioning
	$(MAKE) tables-cs-wire-fuzz
	$(MAKE) tables-cs-region-fuzz
	$(MAKE) tables-cs-builder-fuzz
	$(MAKE) tables-cs-retain-fuzz
	$(MAKE) tables-cs-retain-negative-control
	$(MAKE) tables-cs-pack-negative-control
	$(MAKE) tables-cs-fixedform-arm-negative-control
	$(MAKE) conformance-negative-control-cs
	$(MAKE) tables-cook-open-cs
	$(MAKE) tables-cook-open-cs-lengths-negative-control
	$(MAKE) tables-cook-open-cs-root-negative-control
	$(MAKE) tables-cook-open-cs-walk-negative-control
	$(DOTNET) build bench/tables/cs -c Release --nologo -v quiet
	$(DOTNET) build bench/paired/cs -c Release --nologo -v quiet -property:UseSharedCompilation=false "-property:SerializeCsRoot=$(abspath $(SERIALIZE_CS))"
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



test-cs:

# THE FINITE-FLOAT CONTROL (SPEC §4.3, serialize format 2.0). test/cs's
# TestFiniteFloats requires a non-finite float32/float64 packet read to be
# refused as non_finite_float. This builds test/cs with serialize.cs's per-rule
# switch (-p:SerializeConformanceControls=true) and deletes one finite rule at a
# time: the run must go red on exactly that rule's read vectors, the list in
# test/cs/finite-controls/<rule>.expected, and with no rule deleted it stays green.
.PHONY: packet-finite-cs-negative-control
packet-finite-cs-negative-control: toolchain-cs generated/cs/.stamp generated/bench/cs/.stamp
	$(DOTNET) build test/cs/schematest.csproj -c Debug -p:SerializeConformanceControls=true \
		--artifacts-path build/packet-finite/cs-negative --nologo -v quiet
	cd test/cs && $(DOTNET) ../../build/packet-finite/cs-negative/bin/schematest/debug/schematest.dll
	@for rule in float-finite double-finite; do \
		if ( cd test/cs && SERIALIZE_CS_DELETED_RULE=$$rule $(DOTNET) ../../build/packet-finite/cs-negative/bin/schematest/debug/schematest.dll ) \
			> build/packet-finite/cs-negative/$$rule.log 2>&1; then \
			echo "NEGATIVE CONTROL FAILED: C# packet reads passed with $$rule deleted"; exit 1; fi; \
		grep '^FAILED: ' build/packet-finite/cs-negative/$$rule.log > build/packet-finite/cs-negative/$$rule.got || true; \
		if ! diff -u test/cs/finite-controls/$$rule.expected build/packet-finite/cs-negative/$$rule.got; then \
			echo "NEGATIVE CONTROL FAILED: deleting $$rule did not fail exactly its vectors"; \
			cat build/packet-finite/cs-negative/$$rule.log; exit 1; fi; \
	done
	@echo 'packet finite-float C# negative control: each deleted rule fails exactly its read vectors'

test-cs: packet-finite-cs-negative-control

# THE CARDS UNIT'S C# LEG (docs/SPEC-TABLES.md §2): the card record as a
# generated variable table with an owning facade (tables/cards/cs) and the
# brief text parsed once and rendered many times. These targets live in
# make/cs.mk (NOT a separate make/cards-cs.mk) because the root Makefile's
# port registry treats every make/*.mk as a language, so a cards-only fragment
# would be refused there as a phantom port.

# cards-cs-generate produces ONLY build/tables-generated-cs/cards, cleaning
# that owned output first so a deleted symbol cannot survive a regeneration.
.PHONY: cards-cs-generate
cards-cs-generate: bin/schema
	@rm -rf build/tables-generated-cs/cards
	@mkdir -p build/tables-generated-cs/cards
	./bin/schema generate --lang cs --out build/tables-generated-cs/cards tables/cards

# cards-cs-test builds the generated unit with the facade in Debug and
# Release and runs the facade test program in both. It depends on bin/schema
# and cards-cs-generate only, never on build/tables-generated-cs/.stamp or
# build-conformance-cs, so a red elsewhere in the leg cannot hold it.
.PHONY: cards-cs-test
cards-cs-test: bin/schema cards-cs-generate
	$(DOTNET) build tables/cards/cs/Cards.csproj -c Debug --nologo -v quiet
	$(DOTNET) build tables/cards/cs/Cards.csproj -c Release --nologo -v quiet
	$(DOTNET) run --project tables/cards/cs/Tests/Tests.csproj -c Debug --nologo
	$(DOTNET) run --project tables/cards/cs/Tests/Tests.csproj -c Release --nologo

# cards-cs-driver builds the facade driver program once, so the conformance
# check pays no build per surface.
.PHONY: cards-cs-driver
cards-cs-driver: cards-cs-generate
	@mkdir -p build/cards-driver-cs
	$(DOTNET) build tables/cards/cs/Driver/Driver.csproj -c Release -o build/cards-driver-cs --nologo -v quiet

# cards-cs-conformance runs the harness `cards` subcommand over the C#
# driver with all nine surfaces required and no absence accepted: load, save,
# render, parse and bounds from a-cs-card-round-trips-and-renders-the-shared-
# corpus, upgrade, edit, taskhash and view from
# a-cs-card-upgrades-edits-and-walks-the-shared-corpus.
.PHONY: cards-cs-conformance
cards-cs-conformance: build/conformance-harness cards-cs-driver
	./build/conformance-harness cards --corpus tables/cards/conformance --driver tables/cards/cs/cards-driver --require load,save,render,parse,bounds,upgrade,edit,taskhash,view

# cards-cs-conformance-negative-control: the same check against the driver
# with a test-only environment switch that changes one rendered byte,
# required to FAIL on render while the other eight surfaces pass.
.PHONY: cards-cs-conformance-negative-control
cards-cs-conformance-negative-control: build/conformance-harness cards-cs-driver
	@if CARDS_CS_SABOTAGE=1 ./build/conformance-harness cards --corpus tables/cards/conformance --driver tables/cards/cs/cards-driver --require load,save,render,parse,bounds,upgrade,edit,taskhash,view > build/cards-cs-conformance-neg.log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: the card check stayed green with one rendered byte changed'; exit 1; fi
	@grep -qE 'cards render .*FAIL' build/cards-cs-conformance-neg.log || { echo 'NEGATIVE CONTROL FAILED: render did not go red'; cat build/cards-cs-conformance-neg.log; exit 1; }
	@grep -qE 'cards (load|save|parse|bounds|upgrade|edit|taskhash|view) pass' build/cards-cs-conformance-neg.log || { echo 'NEGATIVE CONTROL FAILED: the other surfaces did not stay green'; cat build/cards-cs-conformance-neg.log; exit 1; }
	@echo 'cards C# negative control: one changed rendered byte turns exactly render red'

# cards-cs-conformance-upgrade-negative-control: a test-only switch that
# drops the retained fields (the package-private retention seam driven with
# no id storage) must FAIL upgrade alone, while the other eight surfaces —
# including the five that share the driver's load path and the three that
# share its edit and hash paths — stay green.
.PHONY: cards-cs-conformance-upgrade-negative-control
cards-cs-conformance-upgrade-negative-control: build/conformance-harness cards-cs-driver
	@if CARDS_CS_UPGRADE_SABOTAGE=1 ./build/conformance-harness cards --corpus tables/cards/conformance --driver tables/cards/cs/cards-driver --require load,save,render,parse,bounds,upgrade,edit,taskhash,view > build/cards-cs-conformance-upgrade-neg.log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: the card check stayed green with the retained fields dropped'; exit 1; fi
	@grep -qE 'cards upgrade .*FAIL' build/cards-cs-conformance-upgrade-neg.log || { echo 'NEGATIVE CONTROL FAILED: upgrade did not go red'; cat build/cards-cs-conformance-upgrade-neg.log; exit 1; }
	@grep -qE 'cards (load|save|render|parse|bounds|edit|taskhash|view) pass' build/cards-cs-conformance-upgrade-neg.log || { echo 'NEGATIVE CONTROL FAILED: the other surfaces did not stay green'; cat build/cards-cs-conformance-upgrade-neg.log; exit 1; }
	@echo 'cards C# negative control: dropping the retained fields turns exactly upgrade red'

# cards-cs-conformance-hash-negative-control: a test-only switch that folds
# the excluded needs list into the task hash must FAIL taskhash alone, while
# the other eight surfaces stay green.
.PHONY: cards-cs-conformance-hash-negative-control
cards-cs-conformance-hash-negative-control: build/conformance-harness cards-cs-driver
	@if CARDS_CS_HASH_SABOTAGE=1 ./build/conformance-harness cards --corpus tables/cards/conformance --driver tables/cards/cs/cards-driver --require load,save,render,parse,bounds,upgrade,edit,taskhash,view > build/cards-cs-conformance-hash-neg.log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: the card check stayed green with needs folded into the hash'; exit 1; fi
	@grep -qE 'cards taskhash .*FAIL' build/cards-cs-conformance-hash-neg.log || { echo 'NEGATIVE CONTROL FAILED: taskhash did not go red'; cat build/cards-cs-conformance-hash-neg.log; exit 1; }
	@grep -qE 'cards (load|save|render|parse|bounds|upgrade|edit|view) pass' build/cards-cs-conformance-hash-neg.log || { echo 'NEGATIVE CONTROL FAILED: the other surfaces did not stay green'; cat build/cards-cs-conformance-hash-neg.log; exit 1; }
	@echo 'cards C# negative control: folding needs into the hash turns exactly taskhash red'

test-cs: cards-cs-test cards-cs-conformance cards-cs-conformance-negative-control cards-cs-conformance-upgrade-negative-control cards-cs-conformance-hash-negative-control

# THE CARDS UNIT RIDES THE C# CONFORMANCE DRIVER'S WIRE FUZZER (§4.2, §6.6).
# These mirror make/dart.mk's cards-dart-wire-fuzz and cards-dart-retain-fuzz
# line for line, against the same oracle harness, the same manifest
# (tables/cards/fuzz/MANIFEST.txt: unit cards, root Card, one pinned populated
# instance) and the same seed, so the C# Card codec is held to the identical
# byte-for-byte contract the Go leg's cards-wire-fuzz pins. The driver is
# test/conformance/cs, which compiles build/tables-generated-cs/cards (from
# cards-cs-generate, through build-conformance-cs) and registers the Card root
# in its codec table (src/Program.cs) and its retaining pairs
# (src/RetainCodecs.cs).
CARDS_CS_FUZZ_TIME ?= 60s
CARDS_CS_DRIVER = $(DOTNET) test/conformance/cs/bin/Debug/net10.0/schemaconformance.dll wire-fuzz

# cards-cs-wire-fuzz is the oracle harness over the Card root only. --vectors
# '' keeps the default pinned vectors out of this Card-only manifest. The greps
# hold the nonzero seed/root/mutant claim and the zero-divergence claim, as
# make/go.mk's cards-wire-fuzz does.
.PHONY: cards-cs-wire-fuzz
cards-cs-wire-fuzz: build-conformance-cs build/conformance-harness
	@timeout $(CARDS_CS_FUZZ_TIME) ./build/conformance-harness wire-fuzz \
		--manifest tables/cards/fuzz/MANIFEST.txt --json tables/cards/fuzz --vectors '' \
		--driver '$(CARDS_CS_DRIVER)' --seed 24845619678 --n 100 \
		> build/cards-cs-wire-fuzz.log 2>&1; \
		status=$$?; cat build/cards-cs-wire-fuzz.log; \
		if [ $$status -ne 0 ]; then exit $$status; fi
	@grep -q '0 divergences' build/cards-cs-wire-fuzz.log
	@grep -qE '[1-9][0-9]* seeds over [1-9][0-9]* roots' build/cards-cs-wire-fuzz.log
	@grep -qE '[1-9][0-9]* mutants' build/cards-cs-wire-fuzz.log

# THE CARD WIRE-FUZZ RIDES THE C# LEG'S GATE (§4.2): the same oracle harness,
# the same seed, over the Card root only.
test-cs: cards-cs-wire-fuzz

# cards-cs-retain-fuzz is the SAME fuzzer through the RETENTION ARM
# (docs/SPEC-TABLES.md §6.6): every mutant is read and written back through the
# Card codec's retaining pair, and the two retention counters ride beside the
# six. The seed, count and coverage greps are cards-cs-wire-fuzz's, plus
# --retain, so a retained save is held to the Go reference leg byte for byte.
.PHONY: cards-cs-retain-fuzz
cards-cs-retain-fuzz: build-conformance-cs build/conformance-harness
	@timeout $(CARDS_CS_FUZZ_TIME) ./build/conformance-harness wire-fuzz \
		--manifest tables/cards/fuzz/MANIFEST.txt --json tables/cards/fuzz --vectors '' \
		--driver '$(CARDS_CS_DRIVER)' --seed 24845619678 --n 100 --retain \
		> build/cards-cs-retain-fuzz.log 2>&1; \
		status=$$?; cat build/cards-cs-retain-fuzz.log; \
		if [ $$status -ne 0 ]; then exit $$status; fi
	@grep -q '0 divergences' build/cards-cs-retain-fuzz.log
	@grep -qE '[1-9][0-9]* seeds over [1-9][0-9]* roots' build/cards-cs-retain-fuzz.log
	@grep -qE '[1-9][0-9]* mutants' build/cards-cs-retain-fuzz.log

# THE CARD WIRE-FUZZ'S RETAIN ARM RIDES THE C# LEG'S GATE (§6.6).
test-cs: cards-cs-retain-fuzz

# cards-cs-retain-negative-control: the driver's test-only CARDS_CS_RETAIN_DROP=1
# switch hands the Card root's retaining load a store with no capacity, so
# every unknown field is dropped. The retain arm must go RED on the retention
# report (the leg keeps nothing where the Go oracle keeps the unknown field)
# while the plain arm, run with the same switch set, stays GREEN: the break is
# the retention's and nothing else's. A crash or a timeout is not detection.
.PHONY: cards-cs-retain-negative-control
cards-cs-retain-negative-control: build-conformance-cs build/conformance-harness
	@mkdir -p build/wire-fuzz
	@if CARDS_CS_RETAIN_DROP=1 timeout $(CARDS_CS_FUZZ_TIME) ./build/conformance-harness wire-fuzz \
		--manifest tables/cards/fuzz/MANIFEST.txt --json tables/cards/fuzz --vectors '' \
		--driver '$(CARDS_CS_DRIVER)' --seed 24845619678 --n 100 --retain \
		--failed build/wire-fuzz/failed-cards-retain-neg-cs.bin \
		> build/cards-cs-retain-neg.log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: the Card retain arm stayed green with retention dropped'; cat build/cards-cs-retain-neg.log; exit 1; fi
	@grep -qE 'FAILED after [1-9][0-9]* mutants: the retention report differs' build/cards-cs-retain-neg.log || { echo 'NEGATIVE CONTROL FAILED: the retain arm did not go red on the retention report'; cat build/cards-cs-retain-neg.log; exit 1; }
	@! grep -Eq 'driver exited|driver wrote nothing|panic|Unhandled exception' build/cards-cs-retain-neg.log || { echo 'NEGATIVE CONTROL FAILED: the driver broke rather than diverged'; cat build/cards-cs-retain-neg.log; exit 1; }
	@CARDS_CS_RETAIN_DROP=1 timeout $(CARDS_CS_FUZZ_TIME) ./build/conformance-harness wire-fuzz \
		--manifest tables/cards/fuzz/MANIFEST.txt --json tables/cards/fuzz --vectors '' \
		--driver '$(CARDS_CS_DRIVER)' --seed 24845619678 --n 100 \
		> build/cards-cs-retain-neg-plain.log 2>&1 || { echo 'NEGATIVE CONTROL FAILED: the plain arm went red too'; cat build/cards-cs-retain-neg-plain.log; exit 1; }
	@grep -q '0 divergences' build/cards-cs-retain-neg-plain.log || { echo 'NEGATIVE CONTROL FAILED: the plain arm diverged'; cat build/cards-cs-retain-neg-plain.log; exit 1; }
	@echo 'cards C# negative control: dropping the Card retention turns exactly the retain arm red'

test-cs: cards-cs-retain-negative-control
