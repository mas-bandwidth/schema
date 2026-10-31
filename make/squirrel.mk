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
		'TestDeprecatedVariantReadsDefaultWriteRefused TestJSONRefusesIllFormedAtBound TestSquirrelAnnounceHonorsReceiverBounds TestSquirrelAnnounceReadsFormBeforeLength TestSquirrelBase64TextNegativeControl TestSquirrelBase64TextRoundTripsEveryInstance TestSquirrelBlobNamedTwice TestSquirrelBlobSharedMutantMatchesGo TestSquirrelBlockBuilderMatchesTheCorpus TestSquirrelBlockReadMatchesTheCorpus TestSquirrelBytesMessagePairs TestSquirrelCompressedFloatClampNegativeControl TestSquirrelCompressedFloatClampsLikeGo TestSquirrelCookMeasureNegativeControl TestSquirrelCookMeasureRefusesLikeCook TestSquirrelCookReadMatchesTheCorpus TestSquirrelCookWriterMatchesTheCorpus TestSquirrelDriverListsItsSurfaces TestSquirrelFixedForm128BitLeaves TestSquirrelFixedFormAgreesWithTheByteOracle TestSquirrelFixedFormNegativeControl TestSquirrelFixedLoadNamesEveryRefusal TestSquirrelFixedReadsItsLineage TestSquirrelJsonAgreesWithTheCorpus TestSquirrelJsonGuardedGroupsElide TestSquirrelJsonNegativeControl TestSquirrelJsonTextZeroByteTakesOneRule TestSquirrelKeyedArraysAreHeldAtBothEnds TestSquirrelLoadTextRefusesNonCanonicalBase64 TestSquirrelMapPresence TestSquirrelMapWireCountRefusal TestSquirrelMapsInJson TestSquirrelMapsMatchTheWire TestSquirrelMessageDepthBoundary TestSquirrelMessageFormMatchesTheGoldens TestSquirrelMessageFormNegativeControl TestSquirrelMessageFormSetsPresentOptionalField TestSquirrelMessageQuantizerIsGos TestSquirrelMessageQuantizerNegativeControl TestSquirrelMessageUnsignedRangeClampAcrossSignBit TestSquirrelMessageWidenArrayElements TestSquirrelMessageWidenFloat32ToFloat64 TestSquirrelMessageWidensAndSignExtends TestSquirrelRequiredInJson TestSquirrelRequiredOnTheWire TestSquirrelRetainMatchesIndependentRewrite TestSquirrelStrictReadNamesNestedPointerArrayElement TestSquirrelStrictReadReadsFormOneAtFixedRoot TestSquirrelStrictReadRefusesByName TestSquirrelTableDescriptorsLoad TestSquirrelTableEmitsOnlyItsDeclaredForms TestSquirrelTableFiniteFloatForms TestSquirrelTableFiniteFloatFormsNegativeControl TestSquirrelTableFiniteFloatNegativeControl TestSquirrelTableFloatsAreFinite TestSquirrelTableLoadMatchesTheReports TestSquirrelTableLoadNegativeControl TestSquirrelTableSaveNegativeControl TestSquirrelTableSaveRoundTripsEveryInstance TestSquirrelTimestampStrictRFC3339 TestSquirrelViewMatchesTheGoView TestSquirrelWireCountRefusalNamesCountRefusal TestSquirrelWireFuzzNegativeControl TestSquirrelWireGuardedGroupsElide TestSquirrelWireIntegrityFixedForm' \
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
