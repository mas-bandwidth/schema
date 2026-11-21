# make/ruby.mk — the RUBY leg (docs/CONTRIBUTING.md, "Adding a language";
# docs/PORT-RUBY.md is the port's design). Included by the Makefile's
# wildcard include; the Makefile names no language. The leg registers itself
# at the end of this file.
#
# The pinned interpreters and analyzers are make/ruby-toolchain.mk's. Ruby is
# a built-in target of every build, so build/schema-ruby is a plain build.

build/schema-ruby: $(shell find compiler internal cmd ir -name '*.go' -o -name '*.rb' -o -name '*.rbs' 2>/dev/null)
	@mkdir -p build
	go build -o $@ ./cmd/schema
# SERIALIZE_PIN, the vendored serialize.rb's pin, and every other runtime file
# the compiler embeds: a pin bump must rebuild the compiler and so
# build/conformance-ruby, or a stale runtime can pass.
# TestConformanceCompilersRebuildOnTheirRuntime holds it.
build/schema-ruby: $(shell find internal/codegen/ruby/runtime internal/codegen/rubytable/runtime -type f 2>/dev/null)

.PHONY: test-ruby test-ruby-recipe update-goldens-ruby ruby-skeleton-negative-control
test-ruby: ruby-toolchain-check ruby-serialize-test packet-utf8-ruby bench-ruby-gate
	$(MAKE) test-ruby-recipe
test-ruby-recipe: ruby-toolchain-check
	sh test/slowgate/proof test-ruby-packet 'TestRubySkeleton TestVendoredRubySerializeIsThePin TestRubyPacketClose TestRubyPacketCloseAnalyzer TestRubyPacketDefaults TestRubyPacketFinite TestRubyPacketVoid TestRubyPacketReadBitsAndOwnership TestRubyPacketShapeBench' -count=1 -timeout 600s -run '^(TestRubySkeleton|TestVendoredRubySerializeIsThePin|TestRubyPacketClose$$|TestRubyPacketCloseAnalyzer$$|TestRubyPacketDefaults$$|TestRubyPacketFinite$$|TestRubyPacketVoid$$|TestRubyPacketReadBitsAndOwnership$$|TestRubyPacketShapeBench$$)' ./internal/codegen/ruby/
	sh test/slowgate/proof test-ruby-tables 'TestRubyVariableTablesAllSurfacesAndStrictAnalyze TestRubyMessageFormAndRetainUnknown TestRubyBlockManifestAndRealUnitsAnalyze' -count=1 -timeout 600s -run '^(TestRubyVariableTablesAllSurfacesAndStrictAnalyze|TestRubyMessageFormAndRetainUnknown|TestRubyBlockManifestAndRealUnitsAnalyze)$$' ./internal/codegen/rubytable/
	sh test/slowgate/proof test-ruby-compiler 'TestRubyTargetIsRegistered TestRubyGeneratesAnEmptyUnitThroughTheDriver TestRubyCarriesThePacketWireThroughTheDriver TestNewLegRubyRecipe TestCountedArrayIsBornAtItsDeclaredMinimum TestCountOnWriteIsDebugOnlyWhereTheLanguageHasThatIdiom TestGuardCommentsSpellTheConditionTheWayDescriptorsDo TestVoidArmInTypeBodyIsCheckedAndCarried TestUnionTagEnumExportsMaxNoCountAndItsDebugName TestTagEnumDebugNameCoversNoneVariantsAndOutOfSet TestUnionTagOnWriteIsDebugOnlyWhereTheLanguageHasThatIdiom TestPacketValueDefaultsCarriers TestNoVariableTableMessagePath TestEveryTargetHasATableBackend TestFixedTableValueDefaultsEveryLeg TestFX1GeneratesOnEveryLeg' -count=1 -run 'Ruby|CountedArray|CountOnWrite|GuardComments|VoidArmInTypeBody|UnionTag|TagEnumDebugName|PacketValueDefaultsCarriers|TestNoVariableTableMessagePath|TestEveryTargetHasATableBackend|FixedTableValueDefaultsEveryLeg|FX1GeneratesOnEveryLeg' ./compiler
update-goldens-ruby: ;

ruby-skeleton-negative-control: ruby-toolchain-check
	sh test/slowgate/proof ruby-skeleton-negative-control 'TestRubySkeleton' -count=1 -timeout 600s -run 'TestRubySkeleton/negative-control' ./internal/codegen/ruby/

# build/conformance-ruby generates every MANIFEST.txt tables unit for Ruby,
# plus the variable-class framing sibling for tblr1 (test/tables/R1.schema).
build/conformance-ruby: build/schema-ruby testdata/conformance/tables/MANIFEST.txt make/ruby.mk internal/codegen/ruby/runtime/SERIALIZE_PIN $(CONFORMANCE_UNIT_SCHEMAS)
	@rm -rf $@ && mkdir -p $@
	@awk '$$1 == "unit" { key = $$2; $$1 = ""; $$2 = ""; print key $$0 }' testdata/conformance/tables/MANIFEST.txt | \
	while read -r key paths; do \
		if ./build/schema-ruby generate --lang ruby --out $@/$$key $$paths > $@/$$key.log 2>&1; then \
			rm -f $@/$$key.log; \
		else \
			rm -rf $@/$$key; mv $@/$$key.log $@/$$key.refused; \
			echo "conformance-ruby: $$key refused: $$(head -1 $@/$$key.refused)"; \
		fi; \
	done
	@echo "conformance-ruby: $$(ls -d $@/*/ 2>/dev/null | wc -l | tr -d ' ') units generated, $$(ls $@/*.refused 2>/dev/null | wc -l | tr -d ' ') refused by name"
	@test "$$(grep -c '^fixed table Cfg$$' test/tables/R1.schema)" = 1
	@test "$$(grep -c '^package tblr1$$' test/tables/R1.schema)" = 1
	@mkdir -p $@/tblr1/_framing/source $@/tblr1/_framing/tblr1
	@sed -e 's/^fixed table Cfg$$/table Cfg/' -e 's/^package tblr1$$/package tblr1_framing/' test/tables/R1.schema > $@/tblr1/_framing/source/R1.schema
	./build/schema-ruby generate --lang ruby --out $@/tblr1/_framing/tblr1 $@/tblr1/_framing/source/R1.schema

generated/bench/ruby/.stamp: build/schema-ruby $(SCHEMAS_BENCH)
	@rm -rf generated/bench/ruby && mkdir -p generated/bench/ruby
	./build/schema-ruby generate --lang ruby --out generated/bench/ruby bench/corpus/Bench.schema
	@touch $@

generated/ruby/.stamp: build/schema-ruby $(SCHEMAS)
	@rm -rf generated/ruby && mkdir -p generated/ruby
	./build/schema-ruby generate --lang ruby --out generated/ruby examples
	@touch $@

generated/ruby-ludicrous/.stamp: build/schema-ruby $(SCHEMAS128)
	@rm -rf generated/ruby-ludicrous && mkdir -p generated/ruby-ludicrous
	./build/schema-ruby generate --lang ruby --out generated/ruby-ludicrous examples128
	@touch $@

generated/bench/tables/ruby/.stamp: ;

TEST_LEGS           += test-ruby
BENCH_TABLES_LEGS   += generated/bench/tables/ruby/.stamp
TOOLCHAIN_LEGS      += ruby
TOOLCHAIN_PINS_ruby := RUBY RUBY_FLOOR RUBOCOP STEEP RBS
CONFORMANCE_LEGS    += $(call unless_skipped,ruby,build/conformance-ruby)
GOLDENS_LEGS        += update-goldens-ruby
