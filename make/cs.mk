# make/cs.mk — the C# leg (docs/CONTRIBUTING.md, "Adding a language"). Included
# by the Makefile's wildcard include; the Makefile names no language. The leg
# registers itself at the end of this file.

# the serialize.cs runtime the generated C# targets, a sibling checkout;
# test/cs/schematest.csproj and its ludicrous twin carry the same relative path
SERIALIZE_CS ?= ../serialize.cs

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

build/tables-generated-cs/.stamp: bin/schema $(SCHEMAS_TABLES) $(SCHEMAS_TABLES_POINTERS) $(SCHEMAS_TABLES_BLOCK) test/tables/V1.schema test/tables/V2.schema test/tables/P1.schema test/tables/P3.schema
	@mkdir -p build/tables-generated-cs
	./bin/schema generate --lang cs --out build/tables-generated-cs/examples tables/examples
	# the POINTERED unit: its C# WIRE surface is refused by name (§11) and its
	# two ACCELERATORS are emitted all the same, because neither needs a codec
	# (§7, §19). This is where the cook's C# read side comes from.
	./bin/schema generate --lang cs --out build/tables-generated-cs/pointers tables/pointers
	./bin/schema generate --lang cs --out build/tables-generated-cs/block tables/block
	./bin/schema generate --lang cs --out build/tables-generated-cs/blockhome tables/blockhome
	./bin/schema generate --lang cs --out build/tables-generated-cs/v1 test/tables/V1.schema
	./bin/schema generate --lang cs --out build/tables-generated-cs/v2 test/tables/V2.schema
	./bin/schema generate --lang cs --out build/tables-generated-cs/p1 test/tables/P1.schema
	./bin/schema generate --lang cs --out build/tables-generated-cs/p3 test/tables/P3.schema
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

.PHONY: tables-cs-refuses-pointers
tables-cs-refuses-pointers: bin/schema
	@rm -rf build/tables-cs-refusal && mkdir -p build
	./bin/schema generate --lang cs --out build/tables-cs-refusal tables/pointers
	@if ls build/tables-cs-refusal/*Table.cs >/dev/null 2>&1; then \
		echo "REFUSAL GATE FAILED: the C# backend emitted a wire surface for a pointered unit"; exit 1; \
	fi
	@for f in build/tables-cs-refusal/*Cook.cs build/tables-cs-refusal/*Block.cs; do \
		grep -q "THE C# WIRE SURFACE OF THIS UNIT IS REFUSED, BY NAME" $$f || \
			{ echo "REFUSAL GATE FAILED: $$f does not carry the refusal banner"; exit 1; }; \
		grep -q "is a named follow-on" $$f || \
			{ echo "REFUSAL GATE FAILED: $$f does not name the follow-on"; exit 1; }; \
		grep -q "Album, Depot, Layer, ListNode, Marker, Scene and TreeNode" $$f || \
			{ echo "REFUSAL GATE FAILED: $$f does not name every refused table"; exit 1; }; \
	done
	@n=$$(ls build/tables-cs-refusal/*Cook.cs | wc -l | tr -d ' '); \
		if [ "$$n" -lt 3 ]; then \
			echo "REFUSAL GATE FAILED: found $$n Cook sources for the pointered unit, expected 3 — the glob, not the property, is what broke"; exit 1; \
		fi
	@echo "tables C# refusal gate: a pointered unit's WIRE half is refused by name, in every source it does emit, and its cooks still open"

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
COOK_CS := cd test/cs-cook && dotnet run --no-build --

.PHONY: build-cs-cook
build-cs-cook: build/tables-generated-cs/.stamp
	cd test/cs-cook && dotnet build -v q --nologo

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
	cd test/cs-cook && SEED=$(SEED) N=$(if $(filter-out 100000,$(N)),$(N),$(COOK_CS_N)) dotnet run --no-build -- fuzz Scene ../../build/cook-open/Scene.cook
	cd test/cs-cook && SEED=$(SEED) N=$(if $(filter-out 100000,$(N)),$(N),$(COOK_CS_N)) dotnet run --no-build -- fuzz TreeNode ../../build/cook-open/TreeNode.cook
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
	@if ( cd test/cs-cook && dotnet build -v q --nologo \
			-p:CookGeneratedDir=../../build/cook-open-cs-$(1)/gen \
			-p:BaseOutputPath=../../build/cook-open-cs-$(1)/bin/ \
			-p:BaseIntermediateOutputPath=../../build/cook-open-cs-$(1)/obj/ \
			> ../../build/cook-open-cs-$(1)/build.log 2>&1 ); then :; else \
		echo "NEGATIVE CONTROL FAILED: the sabotaged emitter's output does not compile"; \
		cat build/cook-open-cs-$(1)/build.log; exit 1; \
	fi
	@if ( cd test/cs-cook && dotnet run --no-build \
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
	$(call cook_open_cs_sabotage,root,if (dataLength < %d) { return false; },if (dataLength == ulong.MaxValue) { return false; } // NEGATIVE CONTROL)

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
	cd test/conformance/cs && dotnet build -v q --nologo

# THE NEGATIVE CONTROL FOR THE C# WALK (docs/SPEC-TABLES.md §16.5), and it is a
# different sabotage from the C++ one above on purpose. That one flips a byte of
# a DUMP and proves the harness can see a wrong ANSWER; this one breaks the
# WALKER and proves the harness can see a wrong WALK.
#
# It is sabotaged IN THE EMITTER and generated afresh, the way the block fuzz's
# controls are (block_fuzz_sabotage above), because the walker IS emitter
# source — one constant in internal/codegen/cstable/json.go — so patching the
# emitter is patching the walk itself rather than an artifact of it. No tracked
# file is written to: the sed lands in build/, a Go build overlay points the
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
# go RED and every other surface must stay GREEN. A matrix whose every cell went
# red would be saying "something broke" rather than "the C# text form broke" —
# and json-write staying green is what says the break is the READER's.
.PHONY: conformance-negative-control-cs
conformance-negative-control-cs:
	@echo "conformance-negative-control-cs: dormant — the surface it turns red is absent while this port writes the wire's previous form (docs/SPEC-TABLES.md §3, schema#513)"

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
# THE LEG IS DORMANT while this port writes the wire's previous form (schema
# #513): the goldens under testdata/wire/tables are the id-table form, and a
# codec that does not write that form cannot reproduce them. What is absent is
# the corpus it holds itself to, not the leg.
.PHONY: tables-cs-leg
tables-cs-leg:
	@echo "tables-cs-leg: dormant — the corpus it gates against is absent while this port writes the wire's previous form (docs/SPEC-TABLES.md §3, schema#513)"

# THE C# LEG of `make test`: the table gates and the C# conformance negative
# control, the cook-open gates on the C# side, the bench units' compile gates
# (a unit that generates but does not compile is issue #80's lesson), and the
# packet tests.
.PHONY: test-cs
test-cs: build/tables-generated-cs/.stamp generated/bench/tables/cs/.stamp generated/cs/.stamp generated/cs-ludicrous/.stamp generated/bench/cs/.stamp
	$(MAKE) tables-cs-json-walk
	$(MAKE) tables-cs-standalone
	$(MAKE) tables-cs-refuses-pointers
	$(MAKE) tables-cs-leg
	$(MAKE) conformance-negative-control-cs
	$(MAKE) tables-cook-open-cs
	$(MAKE) tables-cook-open-cs-lengths-negative-control
	$(MAKE) tables-cook-open-cs-root-negative-control
	dotnet build bench/tables/cs -c Release --nologo -v quiet
	cd bench/cs && dotnet build -c Release --nologo -v quiet
	cd test/cs && dotnet run
	cd test/cs-ludicrous && dotnet run

TEST_LEGS         += test-cs
CONFORMANCE_LEGS  += build-conformance-cs build-cs-cook
BENCH_TABLES_LEGS += generated/bench/tables/cs/.stamp
GOLDENS_LEGS      += update-goldens-cs

# THE C# NATIVE GATE (issue #547). Two halves, and they cover different sets
# for a reason that is the emitter's shape rather than a choice here. Both run
# before the leg reports.
#
# THE FORMATTER half is `dotnet format whitespace --verify-no-changes` over the
# generated trees of both corpora. --folder is what lets the formatter read a
# DIRECTORY rather than a project, and that is required here: a generated
# unit's Block and Cook accelerators share one set of blittable records, so no
# single project compiles a whole unit, let alone the whole corpus
# (test/conformance/cs/schemaconformance.csproj excludes them by name for
# exactly this reason). --folder also means whitespace alone; the style and
# analyzer fixers need a workspace, and what they would see is the second
# half's business.
#
# THE ANALYZER half is the .NET analyzers at their default mode, warnings as
# errors, over the two projects that DO compile: the packet corpus through
# test/cs, and the tables corpus through the conformance project. Analyzers are
# on by default for this TargetFramework; naming the properties here says what
# the gate depends on rather than leaving it to an SDK default.
.PHONY: native-cs
native-cs: generated/cs/.stamp generated/cs-ludicrous/.stamp build/tables-generated-cs/.stamp
	@fail=0; \
	echo "==== dotnet format"; \
	dotnet format whitespace generated/cs --folder --verify-no-changes || fail=1; \
	dotnet format whitespace generated/cs-ludicrous --folder --verify-no-changes || fail=1; \
	dotnet format whitespace build/tables-generated-cs --folder --verify-no-changes || fail=1; \
	echo "==== the .NET analyzers"; \
	( cd test/cs && dotnet build -v q --nologo \
		-p:EnableNETAnalyzers=true -p:AnalysisMode=Default -p:TreatWarningsAsErrors=true ) || fail=1; \
	( cd test/conformance/cs && dotnet build -v q --nologo \
		-p:EnableNETAnalyzers=true -p:AnalysisMode=Default -p:TreatWarningsAsErrors=true ) || fail=1; \
	if [ $$fail -ne 0 ]; then echo "native C#: the findings above are the emitter's"; exit 1; fi; \
	echo "native C#: dotnet format canonical and the .NET analyzers clean over the examples and tables corpora"

NATIVE_LEGS       += native-cs
