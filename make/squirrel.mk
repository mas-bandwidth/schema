# make/squirrel.mk — the SQUIRREL leg (docs/CONTRIBUTING.md, "Adding a language";
# docs/PORT-SQUIRREL.md is the port's design). Included by the Makefile's
# wildcard include; the Makefile names no language. The leg registers itself at
# the end of this file.
#
# Squirrel is a built-in target (compiler/target_squirrel.go): build/schema-squirrel
# is the same compiler as bin/schema, built at the path the leg's targets and
# tests name.

# THE PINNED INTERPRETER (docs/PORT-SQUIRREL.md, 64-BIT AND 128-BIT INTEGERS).
# Squirrel 3.2 from the upstream tag, checked against one sha256, built as sq64
# with SQUSEDOUBLE: sq64 makes _intsize_ 8 (a 64-bit integer, the codec's
# int64), and without -DSQUSEDOUBLE the float is 32-bit (16777217.0 ==
# 16777216.0), which no float64 field survives. dist/ is gitignored.
SQUIRREL_VERSION := 3.2
SQUIRREL_URL     := https://github.com/albertodemichelis/squirrel/archive/refs/tags/v$(SQUIRREL_VERSION).tar.gz
SQUIRREL_SHA256  := 02805414cfadd5bbb921891d3599b83375a40650abd6404a8ab407dc5e86a996
SQUIRREL_DIR     := dist/squirrel-$(SQUIRREL_VERSION)
SQ ?= $(CURDIR)/$(SQUIRREL_DIR)/bin/sq
# the conformance driver and the test helper (internal/codegen/squirrel/sqrun)
# are processes this make spawns, so they read the pin from the environment
export SQ

.PHONY: dist-squirrel
dist-squirrel:
	@mkdir -p dist
	@if [ ! -f dist/squirrel-$(SQUIRREL_VERSION).tar.gz ]; then \
		curl -fsSL -o dist/squirrel-$(SQUIRREL_VERSION).tar.gz.part $(SQUIRREL_URL) && \
		mv dist/squirrel-$(SQUIRREL_VERSION).tar.gz.part dist/squirrel-$(SQUIRREL_VERSION).tar.gz; fi
	@got=$$(sha256sum dist/squirrel-$(SQUIRREL_VERSION).tar.gz | cut -d' ' -f1); \
	if [ "$$got" != "$(SQUIRREL_SHA256)" ]; then \
		echo "dist-squirrel REFUSES: dist/squirrel-$(SQUIRREL_VERSION).tar.gz has sha256 $$got, the pin is $(SQUIRREL_SHA256)"; \
		echo "  delete the file and run make dist-squirrel again; a different tarball is never built"; \
		exit 1; fi
	@if [ ! -x $(SQUIRREL_DIR)/bin/sq ]; then \
		rm -rf $(SQUIRREL_DIR) && tar -xzf dist/squirrel-$(SQUIRREL_VERSION).tar.gz -C dist && \
		{ $(MAKE) -C $(SQUIRREL_DIR) sq64 DEFS=-DSQUSEDOUBLE > dist/squirrel-build.log 2>&1 || { cat dist/squirrel-build.log; exit 1; }; }; fi
	@echo "squirrel $(SQUIRREL_VERSION) $(SQUIRREL_URL)"
	@echo "sha256 $(SQUIRREL_SHA256)"
	@echo "sq $(CURDIR)/$(SQUIRREL_DIR)/bin/sq"

# THE COMMITTED PACKET TREES, as every main port carries generated/<lang> and
# generated/<lang>-ludicrous: the packet emitter's output for examples/ and
# examples128/, checked current by ci-full.yml's `generated` job
# (test/generated-tree/verify derives this stamp list) and linted by
# make/schema-squirrel-generated-lint-gate.mk.
generated/squirrel/.stamp: bin/schema $(SCHEMAS)
	./bin/schema generate --lang squirrel --out generated/squirrel examples
	@touch $@

generated/squirrel-ludicrous/.stamp: bin/schema $(SCHEMAS128)
	./bin/schema generate --lang squirrel --out generated/squirrel-ludicrous examples128
	@touch $@

# THE TOOLCHAIN GATE, this leg's half (issue #599): `make test` runs this before
# the chain starts and refuses by name when the pin does not resolve.
.PHONY: toolchain-squirrel
toolchain-squirrel:
	@$(call toolchain_probe,squirrel,SQ,$(SQ))

# the leg's own copy of the compiler, at the path its targets name
build/schema-squirrel: $(GO_SOURCES)
	@mkdir -p build
	go build -o $@ ./cmd/schema

# THE CONFORMANCE BUILD: every unit of the tables manifest generated for
# squirrel (testdata/conformance/tables/MANIFEST.txt). A unit the target
# refuses is written to REFUSED.txt with its refusal rather than failing the
# build: the refusal is the leg's answer for that unit until a card lifts it
# (docs/PORT-SQUIRREL.md, THE REGISTER).
build/conformance-squirrel: build/schema-squirrel testdata/conformance/tables/MANIFEST.txt
	@rm -rf $@ && mkdir -p $@
	@: > $@/REFUSED.txt
	@grep '^unit ' testdata/conformance/tables/MANIFEST.txt | while read -r _ key paths; do \
		if ! ./build/schema-squirrel generate --lang squirrel --out $@/$$key $$paths > $@/$$key.log 2>&1; then \
			printf '%s %s\n' "$$key" "$$(tr '\n' ' ' < $@/$$key.log)" >> $@/REFUSED.txt; \
		fi; rm -f $@/$$key.log; \
	done
	@echo "conformance-squirrel: $$(ls -d $@/*/ 2>/dev/null | wc -l) units generated, $$(wc -l < $@/REFUSED.txt) refused (build/conformance-squirrel/REFUSED.txt)"

.PHONY: test-squirrel update-goldens-squirrel
# the Go oracle (TestSquirrelViewMatchesTheGoView and the fixed-load oracle) builds
# against the pinned serialize.go, fetched when no sibling is present
test-squirrel: toolchain-squirrel dist-squirrel build/fixedform-corpus/.stamp $(SERIALIZE_GO_RUNTIME)/go.mod
	sh test/slowgate/proof test-squirrel \
		'TestSkeletonFixture TestSqrunFailsAScriptThatThrows TestSqrunRoundTripsInputs TestSquirrelFileNamesNeverShadowModuleLocals TestSquirrelLoaderResolvesOnlyItsDirectory TestSquirrelPacketFiniteNegativeControl TestSquirrelPacketFloatsAreFinite TestSquirrelTypeWireReadNegativeControl TestSquirrelTypeWireReadsTheGoldens TestSquirrelTypeWireWideTextVectors TestSquirrelTypeWireWriteNegativeControl TestSquirrelTypeWireWritesTheGoldens TestSquirrelTypesConstructAtTheirDefaults TestVendoredSquirrelSerializeIsThePin' \
		-count=1 -timeout 600s ./internal/codegen/squirrel/
	sh test/slowgate/proof test-squirrel \
		'TestDeprecatedVariantReadsDefaultWriteRefused TestJSONRefusesIllFormedAtBound TestSquirrelAnnounceHonorsReceiverBounds TestSquirrelAnnounceReadsFormBeforeLength TestSquirrelBase64TextNegativeControl TestSquirrelBase64TextRoundTripsEveryInstance TestSquirrelBlobNamedTwice TestSquirrelBlobSharedMutantMatchesGo TestSquirrelBlockBuilderMatchesTheCorpus TestSquirrelBlockReadMatchesTheCorpus TestSquirrelBytesMessagePairs TestSquirrelCompressedFloatClampNegativeControl TestSquirrelCompressedFloatClampsLikeGo TestSquirrelCookMeasureNegativeControl TestSquirrelCookMeasureRefusesLikeCook TestSquirrelCookReadMatchesTheCorpus TestSquirrelCookWriterMatchesTheCorpus TestSquirrelDriverListsItsSurfaces TestSquirrelFixedForm128BitLeaves TestSquirrelFixedFormAgreesWithTheByteOracle TestSquirrelFixedFormNegativeControl TestSquirrelFixedLoadNamesEveryRefusal TestSquirrelFixedReadsItsLineage TestSquirrelJsonAgreesWithTheCorpus TestSquirrelJsonGuardedGroupsElide TestSquirrelJsonNegativeControl TestSquirrelJsonTextZeroByteTakesOneRule TestSquirrelKeyedArraysAreHeldAtBothEnds TestSquirrelKeyedAccessorCoversBothEnds TestSquirrelKeyedNegativeControls TestSquirrelMapWriterSort TestSquirrelMapWriterSortNegativeControl TestSquirrelJsonLabelNegativeControl TestSquirrelTableSaveIdentityNegativeControl TestSquirrelBlockByteOrderNegativeControl TestSquirrelBlockOrderWordNegativeControl TestSquirrelTextDifferentialAgainstTheGoEngine TestSquirrelTextDifferentialNegativeControl TestSquirrelCountRefusalStopsTheBuilder TestSquirrelCountRefusalStopsTheBuilderNegativeControl TestSquirrelLoadTextRefusesNonCanonicalBase64 TestSquirrelMapPresence TestSquirrelMapWireCountRefusal TestSquirrelMapsInJson TestSquirrelMapsMatchTheWire TestSquirrelMessageDepthBoundary TestSquirrelMessageFormMatchesTheGoldens TestSquirrelMessageFormNegativeControl TestSquirrelMessageFormSetsPresentOptionalField TestSquirrelMessageQuantizerIsGos TestSquirrelMessageQuantizerNegativeControl TestSquirrelMessageUnsignedRangeClampAcrossSignBit TestSquirrelMessageWidenArrayElements TestSquirrelMessageWidenFloat32ToFloat64 TestSquirrelMessageWidensAndSignExtends TestSquirrelRequiredInJson TestSquirrelRequiredOnTheWire TestSquirrelRetainMatchesIndependentRewrite TestSquirrelStrictReadNamesNestedPointerArrayElement TestSquirrelStrictReadReadsFormOneAtFixedRoot TestSquirrelStrictReadRefusesByName TestSquirrelTableDescriptorsLoad TestSquirrelTableEmitsOnlyItsDeclaredForms TestSquirrelTableFiniteFloatForms TestSquirrelTableFiniteFloatFormsNegativeControl TestSquirrelTableFiniteFloatNegativeControl TestSquirrelTableFloatsAreFinite TestSquirrelTableLoadMatchesTheReports TestSquirrelTableLoadNegativeControl TestSquirrelTableSaveNegativeControl TestSquirrelTableSaveRoundTripsEveryInstance TestSquirrelTimestampStrictRFC3339 TestSquirrelViewMatchesTheGoView TestSquirrelWireCountRefusalNamesCountRefusal TestSquirrelWireFuzzNegativeControl TestSquirrelWireGuardedGroupsElide TestSquirrelWireIntegrityFixedForm' \
		-count=1 -timeout 600s -skip 'NightlyFuzz' ./internal/codegen/squirreltable/
update-goldens-squirrel: ;

# THE PACKET FINITE-FLOAT CONTROL (SPEC §4.3, serialize format 2.0). The
# generated packet code reaches the vendored serialize.nut for every bare float,
# and the runtime refuses a non-finite one; test/packet-finite/squirrel/main.nut
# holds that at every float shape a packet carries (TestSquirrelPacketFloatsAreFinite,
# under test-squirrel). The control drops the runtime's read test at both widths
# in memory and must red exactly the 63 non_finite_float read vectors while the
# write traps and the bit-exact round trips stay green.
.PHONY: packet-finite-squirrel-negative-control
packet-finite-squirrel-negative-control: toolchain-squirrel dist-squirrel
	sh test/slowgate/proof packet-finite-squirrel-negative-control 'TestSquirrelPacketFiniteNegativeControl' \
		-count=1 -run '^TestSquirrelPacketFiniteNegativeControl$$' ./internal/codegen/squirrel/

# THE TABLE FINITE-FLOAT CONTROLS (docs/SPEC-TABLES.md §3.3, §3.4): every
# read refuses a NaN or an infinity on its raw bits as non_finite_float and
# every writer refuses one. The positive rows are TestSquirrelTableFloatsAreFinite
# (variable form, retained unknowns, against the Go oracle) and
# TestSquirrelTableFiniteFloatForms (message form against the oracle, fixed
# form), under test-squirrel. Each control removes ONE check in memory and must
# red exactly that check's rows: the variable read, the retained-entry test and
# the variable writer; the message read and writer; the fixed read and writer.
.PHONY: tables-squirrel-finite-float-negative-control
tables-squirrel-finite-float-negative-control: toolchain-squirrel dist-squirrel
	sh test/slowgate/proof tables-squirrel-finite-float-negative-control \
		'TestSquirrelTableFiniteFloatFormsNegativeControl TestSquirrelTableFiniteFloatNegativeControl' \
		-count=1 -run '^TestSquirrelTableFiniteFloat(Forms)?NegativeControl$$' ./internal/codegen/squirreltable/

# THE FLOAT-PARITY CONTROLS (docs/SPEC-TABLES.md §3.3, §4, §7.6; SPEC.md §4.3),
# beside internal/codegen/squirreltable/floatparity_test.go's positive rows
# against the Go engine: the variable read's compressed-float clamp removed
# must red exactly the rows the engine clamps; the message quantizer put back
# to one rounding in double must red exactly the fuzz rows where that parts
# from ir.TableMessageQuantize, and an over-count index clamped instead of
# refused must red exactly the over-count rows; CookMeasure that does not walk
# must red exactly the non-finite rows.
.PHONY: tables-squirrel-float-parity-negative-control
tables-squirrel-float-parity-negative-control: toolchain-squirrel dist-squirrel
	sh test/slowgate/proof tables-squirrel-float-parity-negative-control \
		'TestSquirrelCompressedFloatClampNegativeControl TestSquirrelCookMeasureNegativeControl TestSquirrelMessageQuantizerNegativeControl' \
		-count=1 -run '^TestSquirrel(CompressedFloatClamp|MessageQuantizer|CookMeasure)NegativeControl$$' ./internal/codegen/squirreltable/

generated/bench/tables/squirrel/.stamp: ;

TEST_LEGS         += test-squirrel
CONFORMANCE_LEGS  += $(call unless_skipped,squirrel,build/conformance-squirrel)
BENCH_TABLES_LEGS += generated/bench/tables/squirrel/.stamp
GOLDENS_LEGS      += update-goldens-squirrel
TOOLCHAIN_LEGS          += squirrel
TOOLCHAIN_PINS_squirrel := SQ

# I12 (docs/PORTING.md) — THE DOCUMENTED SURFACE COMPILES AND RUNS, Squirrel
# side. The gate pulls the marked fragment out of docs/USAGE.md, wraps it in a
# script that supplies the one helper the page leaves to the caller, and runs
# it under the pinned interpreter against the generated `tables/pointers`
# modules, so the day the emitted surface moves the page goes red with it. The
# fragment saves a two-node list and walks it back: it must reach both nodes.
# USAGE_DOC names the page; the control points it at a sabotaged copy.
USAGE_DOC ?= docs/USAGE.md
USAGE_SQUIRREL_DIR ?= build/squirrel-usage
.PHONY: tables-squirrel-usage tables-squirrel-usage-negative-control
tables-squirrel-usage: toolchain-squirrel bin/schema $(USAGE_DOC)
	@rm -rf $(USAGE_SQUIRREL_DIR) && mkdir -p $(USAGE_SQUIRREL_DIR)
	./bin/schema generate --lang squirrel --out $(USAGE_SQUIRREL_DIR) tables/pointers
	printf 'local used = { n = 0 };\nlocal use = function(x) { used.n++; };\n' > $(USAGE_SQUIRREL_DIR)/usage.nut
	sed -n '/<!-- squirrel-table-usage -->/,/<!-- \/squirrel-table-usage -->/p' $(USAGE_DOC) \
		| sed '1,2d' | sed '$$d' | sed '$$d' >> $(USAGE_SQUIRREL_DIR)/usage.nut
	printf 'if (used.n != 2) throw "the walk reached " + used.n + " node(s), want 2";\n' >> $(USAGE_SQUIRREL_DIR)/usage.nut
	printf 'print("usage: docs/USAGE.md Squirrel example saved the Scene and walked " + used.n + " node(s)\\n");\n' >> $(USAGE_SQUIRREL_DIR)/usage.nut
	grep -q 'SceneLoad' $(USAGE_SQUIRREL_DIR)/usage.nut || \
		{ echo "MISSING: $(USAGE_DOC) carries no squirrel-table-usage example"; exit 1; }
	cd $(USAGE_SQUIRREL_DIR) && $(SQ) usage.nut > usage.out 2> usage.err; cat usage.out; \
		if [ -s usage.err ]; then cat usage.err; exit 1; fi; grep -q '^usage: ' usage.out

# THE REVERSED WITNESSES: two edited copies of the page, each checked to have
# changed, must each turn the gate red. The first renames the strict read the
# page calls (the surface moved: the run errors); the second walks a member the
# Scene does not carry (the example runs but reaches the wrong count).
tables-squirrel-usage-negative-control:
	@rm -rf build/squirrel-usage-negative && mkdir -p build/squirrel-usage-negative
	sed 's/T\.SceneLoad(/T.SceneLaod(/' docs/USAGE.md > build/squirrel-usage-negative/renamed.md
	sed 's/local node = back\.Head;/local node = back.Alias;/' docs/USAGE.md > build/squirrel-usage-negative/walks-one.md
	@for c in renamed walks-one; do \
		if cmp -s docs/USAGE.md build/squirrel-usage-negative/$$c.md; then \
			echo "NEGATIVE CONTROL: the $$c edit did not apply to docs/USAGE.md"; exit 1; fi; \
		if $(MAKE) tables-squirrel-usage USAGE_DOC=build/squirrel-usage-negative/$$c.md USAGE_SQUIRREL_DIR=build/squirrel-usage-negative/$$c > build/squirrel-usage-negative/$$c.log 2>&1; then \
			echo "NEGATIVE CONTROL FAILED: the $$c copy of the page passed the Squirrel usage gate"; exit 1; fi; \
		echo "tables-squirrel-usage negative control: the $$c copy fails the gate"; \
	done

test-squirrel: tables-squirrel-usage

# J2 (docs/PORTING.md) — THE RUNTIME HOME. A unit's table module lives in one
# file named by the PACKAGE (<Package>Tables.nut, squirreltable.ModuleName),
# never by the schema file that sorts first, so a file that sorts earlier
# relocates nothing: the module keeps its name and the embedded runtime files
# (schema_table_load.nut, ...) are byte-identical across the two trees.
.PHONY: tables-squirrel-runtime-home tables-squirrel-runtime-home-negative-control
tables-squirrel-runtime-home: bin/schema
	@rm -rf build/runtime-home-squirrel && mkdir -p build/runtime-home-squirrel/src
	@cp tables/examples/*.schema build/runtime-home-squirrel/src/
	@printf 'package tabledemo\n\ntype AaaPacket\n{\n    tag uint8\n}\n' > build/runtime-home-squirrel/src/Aaa.schema
	@./bin/schema generate --lang squirrel --out build/runtime-home-squirrel/base tables/examples 2>/dev/null
	@./bin/schema generate --lang squirrel --out build/runtime-home-squirrel/added build/runtime-home-squirrel/src 2>/dev/null
	@for tree in base added; do \
		test -f build/runtime-home-squirrel/$$tree/TabledemoTables.nut || \
			{ echo "RUNTIME HOME GATE FAILED: the $$tree tree has no TabledemoTables.nut (the table module is named by the package)"; ls build/runtime-home-squirrel/$$tree/*Tables.nut; exit 1; }; \
	done
	@for f in internal/codegen/squirreltable/runtime/*.nut; do \
		n=schema_table_$$(basename $$f); \
		test -f build/runtime-home-squirrel/base/$$n || { echo "RUNTIME HOME GATE FAILED: the base tree carries no $$n"; exit 1; }; \
		cmp -s build/runtime-home-squirrel/base/$$n build/runtime-home-squirrel/added/$$n || \
			{ echo "RUNTIME HOME GATE FAILED: the table runtime file $$n moved when the unit gained an earlier-sorting file"; exit 1; }; \
	done
	@echo "runtime home gate (Squirrel): the table module stays TabledemoTables.nut and the table runtime files stay byte-identical when an earlier-sorting file joins the unit"

# Its NEGATIVE CONTROL: put the file-order rule back (the module named by the
# file that sorts first, ir.ProtocolIdHome) and the home must MOVE when the
# earlier-sorting file joins.
tables-squirrel-runtime-home-negative-control: bin/schema tables-squirrel-runtime-home
	@sed 's|return p + "Tables"|return ir.ProtocolIdHome(u) + "Tables" // SABOTAGED: back to the file order|' \
		internal/codegen/squirreltable/squirreltable.go > build/squirrel-runtime-home-fileorder.gotext
	@grep -q SABOTAGED build/squirrel-runtime-home-fileorder.gotext || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; }
	@printf '{"Replace":{"%s/internal/codegen/squirreltable/squirreltable.go":"%s/build/squirrel-runtime-home-fileorder.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/squirrel-runtime-home-overlay.json
	@go build -overlay=build/squirrel-runtime-home-overlay.json -o build/schema-squirrel-runtime-home-sabotaged ./cmd/schema
	@rm -rf build/runtime-home-squirrel/base-sabotage build/runtime-home-squirrel/added-sabotage
	@./build/schema-squirrel-runtime-home-sabotaged generate --lang squirrel --out build/runtime-home-squirrel/base-sabotage tables/examples 2>/dev/null
	@./build/schema-squirrel-runtime-home-sabotaged generate --lang squirrel --out build/runtime-home-squirrel/added-sabotage build/runtime-home-squirrel/src 2>/dev/null
	@base=$$(cd build/runtime-home-squirrel/base-sabotage && ls *Tables.nut | grep -v '^Tables.nut$$'); \
	 added=$$(cd build/runtime-home-squirrel/added-sabotage && ls *Tables.nut | grep -v '^Tables.nut$$'); \
	 if [ "$$base" = "$$added" ]; then \
		echo "NEGATIVE CONTROL FAILED: the file-order rule kept the table module in $$base — the gate is watching nothing"; exit 1; \
	 fi; \
	 echo "runtime home negative control (Squirrel): the file-order rule moves the table module from $$base to $$added"

test-squirrel: tables-squirrel-runtime-home
