# make/ruby.mk — the RUBY leg (docs/CONTRIBUTING.md, "Adding a language";
# docs/PORT-RUBY.md is the port's design). Included by the Makefile's
# wildcard include; the Makefile names no language. The leg registers itself
# at the end of this file.
#
# The pinned interpreters and analyzers are make/ruby-toolchain.mk's. The
# target stays behind the schema_leg_ruby build tag until the table layers
# (R5 fixed tables and R6 variable tables) satisfy the cross-leg table gates,
# so R4 builds build/schema-ruby with -tags schema_leg_ruby.

build/schema-ruby: $(shell find compiler internal cmd ir -name '*.go' -o -name '*.rb' -o -name '*.rbs' 2>/dev/null) $(EMBED_SOURCES)
	@mkdir -p build
	go build -tags schema_leg_ruby -o $@ ./cmd/schema
# SERIALIZE_PIN, the vendored serialize.rb's pin, and every other runtime file
# the compiler embeds: a pin bump must rebuild the compiler and so
# build/conformance-ruby, or a stale runtime can pass.
# TestConformanceCompilersRebuildOnTheirRuntime holds it.
build/schema-ruby: $(shell find internal/codegen/ruby/runtime internal/codegen/rubytable/runtime -type f 2>/dev/null)

build/packet-write-cfloat/ruby/.stamp: build/schema-ruby test/packet-write-cfloat/PacketCFloat.schema
	./build/schema-ruby generate --lang ruby --out build/packet-write-cfloat/ruby test/packet-write-cfloat/PacketCFloat.schema
	@touch $@

.PHONY: packet-write-cfloat-ruby
packet-write-cfloat-ruby: build/mustrun build/packet-write-cfloat/ruby/.stamp
	$(MUSTRUN) -name packet-write-cfloat-ruby -count 10 -- $(RUBY) test/packet-write-cfloat/ruby/main.rb build/packet-write-cfloat/ruby test/packet-write-cfloat/cases.txt
	$(MUSTRUN) -name packet-write-cfloat-ruby-floor -count 10 -- $(RUBY_FLOOR) test/packet-write-cfloat/ruby/main.rb build/packet-write-cfloat/ruby test/packet-write-cfloat/cases.txt

.PHONY: test-ruby test-ruby-recipe update-goldens-ruby ruby-skeleton-negative-control
test-ruby: ruby-toolchain-check ruby-serialize-test packet-utf8-ruby packet-write-cfloat-ruby bench-ruby-gate
	$(MAKE) test-ruby-recipe
test-ruby-recipe: ruby-toolchain-check
	sh test/slowgate/proof test-ruby-packet 'TestRubySkeleton TestRubyIdentifierCorpus TestVendoredRubySerializeIsThePin TestRubyPacketClose TestRubyPacketCloseAnalyzer TestRubyPacketDefaults TestRubyPacketFinite TestRubyPacketVoid TestRubyPacketReadBitsAndOwnership TestRubyPacketShapeBench' -count=1 -timeout 600s -tags schema_leg_ruby -run '^(TestRubySkeleton|TestRubyIdentifierCorpus$$|TestVendoredRubySerializeIsThePin|TestRubyPacketClose$$|TestRubyPacketCloseAnalyzer$$|TestRubyPacketDefaults$$|TestRubyPacketFinite$$|TestRubyPacketVoid$$|TestRubyPacketReadBitsAndOwnership$$|TestRubyPacketShapeBench$$)' ./internal/codegen/ruby/
	sh test/slowgate/proof test-ruby-tables 'TestRubyVariableTablesAllSurfacesAndStrictAnalyze TestRubyMessageFormAndRetainUnknown TestRubyBlockManifestAndRealUnitsAnalyze TestRubyCompressedFloatFloat32Ends TestRubyMessageBytesElementKind TestRubyRetainRefusalEmptiesTheStore' -count=1 -timeout 600s -run '^(TestRubyVariableTablesAllSurfacesAndStrictAnalyze|TestRubyMessageFormAndRetainUnknown|TestRubyBlockManifestAndRealUnitsAnalyze|TestRubyCompressedFloatFloat32Ends|TestRubyMessageBytesElementKind|TestRubyRetainRefusalEmptiesTheStore)$$' ./internal/codegen/rubytable/
	sh test/slowgate/proof test-ruby-compiler 'TestRubyTargetIsRegistered TestRubyGeneratesAnEmptyUnitThroughTheDriver TestRubyCarriesThePacketWireThroughTheDriver TestNewLegRubyRecipe TestCountedArrayIsBornAtItsDeclaredMinimum TestCountOnWriteIsDebugOnlyWhereTheLanguageHasThatIdiom TestGuardCommentsSpellTheConditionTheWayDescriptorsDo TestVoidArmInTypeBodyIsCheckedAndCarried TestUnionTagEnumExportsMaxNoCountAndItsDebugName TestTagEnumDebugNameCoversNoneVariantsAndOutOfSet TestUnionTagOnWriteIsDebugOnlyWhereTheLanguageHasThatIdiom TestPacketValueDefaultsCarriers TestNoVariableTableMessagePath TestEveryTargetHasATableBackend TestFixedTableValueDefaultsEveryLeg TestFX1GeneratesOnEveryLeg' -count=1 -tags schema_leg_ruby -run 'Ruby|CountedArray|CountOnWrite|GuardComments|VoidArmInTypeBody|UnionTag|TagEnumDebugName|PacketValueDefaultsCarriers|TestNoVariableTableMessagePath|TestEveryTargetHasATableBackend|FixedTableValueDefaultsEveryLeg|FX1GeneratesOnEveryLeg' ./compiler
update-goldens-ruby: ;

ruby-skeleton-negative-control: ruby-toolchain-check
	sh test/slowgate/proof ruby-skeleton-negative-control 'TestRubySkeleton' -count=1 -timeout 600s -tags schema_leg_ruby -run 'TestRubySkeleton/negative-control' ./internal/codegen/ruby/

# build/conformance-ruby generates every MANIFEST.txt tables unit for Ruby,
# plus the variable-class framing siblings for fixed roots.
build/conformance-ruby: build/schema-ruby testdata/conformance/tables/MANIFEST.txt make/ruby.mk $(CONFORMANCE_UNIT_SCHEMAS)
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
	@test "$$(grep -c '^fixed table Bounded$$' tables/lists/Migrate.schema)" = 1
	@test "$$(grep -c '^package listdemo$$' tables/lists/Migrate.schema)" = 1
	@mkdir -p $@/listdemo/_framing/source $@/listdemo/_framing/listdemo
	@sed -e 's/^fixed table Bounded$$/table Bounded/' -e 's/^package listdemo$$/package listdemo_framing/' tables/lists/Migrate.schema > $@/listdemo/_framing/source/Migrate.schema
	./build/schema-ruby generate --lang ruby --out $@/listdemo/_framing/listdemo $@/listdemo/_framing/source/Migrate.schema
	@test "$$(grep -c '^fixed table CountedMin$$' tables/examples/Tables.schema)" = 1
	@test "$$(grep -c '^package tabledemo$$' tables/examples/Tables.schema)" = 1
	@mkdir -p $@/tabledemo/_framing/source $@/tabledemo/_framing/tabledemo
	@sed -e 's/^fixed table CountedMin$$/table CountedMin/' -e 's/^package tabledemo$$/package tabledemo_framing/' tables/examples/Tables.schema > $@/tabledemo/_framing/source/Tables.schema
	./build/schema-ruby generate --lang ruby --out $@/tabledemo/_framing/tabledemo $@/tabledemo/_framing/source/Tables.schema

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
