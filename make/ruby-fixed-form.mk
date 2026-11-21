# make/ruby-fixed-form.mk — Ruby Form-3 fixed tables, live C++ byte oracle,
# compiled lineage plans, and planted negative controls (docs/PORT-RUBY.md;
# docs/plans/ruby-leg.md, R5).


RUBY_FIXED_SHARDS := ruby-fixed-form-core ruby-fixed-form-suite \
	ruby-fixed-form-values ruby-fixed-form-shapes \
	ruby-fixed-form-unions-optionals ruby-fixed-form-evolution \
	ruby-fixed-form-kind-pairs ruby-fixed-writer-lineage

.PHONY: ruby-fixed-form $(RUBY_FIXED_SHARDS) \
	ruby-fixed-form-negative-control \
	ruby-fixed-core-values-negative-control \
	ruby-fixed-proof-values-negative-control \
	ruby-fixed-proof-shapes-negative-control \
	ruby-fixed-proof-unions-optionals-negative-control \
	ruby-fixed-proof-evolution-negative-control \
	ruby-fixed-crc-negative-control \
	ruby-fixed-refused-hash-negative-control \
	ruby-fixed-writer-lineage-negative-control

test-ruby: ruby-fixed-form
ruby-fixed-form: $(RUBY_FIXED_SHARDS)

ruby-fixed-form-core: ruby-toolchain-check
	sh test/slowgate/proof ruby-fixed-form-core 'TestRubyFixedForm TestRubyFixedFormLineage TestRubyFixedFormDeprecatedEnum TestRubyFixedFormPacketKeyed TestRubyFixedIntegrityCRC TestRubyFixedIdentityTrustedLayoutOnly TestRubyFixedIdentityRejectsUnrecordedSchemes TestRubyFixedShapeCompare TestRubyFixedCoreValues' -count=1 -timeout 600s -run '^(TestRubyFixedForm$$|TestRubyFixedFormLineage$$|TestRubyFixedFormDeprecatedEnum$$|TestRubyFixedFormPacketKeyed$$|TestRubyFixedIntegrityCRC$$|TestRubyFixedIdentityTrustedLayoutOnly$$|TestRubyFixedIdentityRejectsUnrecordedSchemes$$|TestRubyFixedShapeCompare$$|TestRubyFixedCoreValues$$)' ./internal/codegen/rubytable/

ruby-fixed-form-suite: ruby-toolchain-check build/fixedform-corpus/.stamp
	SCHEMA_REQUIRE_CORPUS=1 sh test/slowgate/proof ruby-fixed-form-suite 'TestRubyFixedProofSuite' -count=1 -timeout 600s -run '^TestRubyFixedProofSuite$$' ./internal/codegen/rubytable/

ruby-fixed-form-values: ruby-toolchain-check
	sh test/slowgate/proof ruby-fixed-form-values 'TestRubyFixedProofValues TestRubyFixedProofValuesTypes TestRubyFixedProofValuesReadRules TestRubyFixedProofValuesFloatBounds' -count=1 -timeout 600s -run '^(TestRubyFixedProofValues$$|TestRubyFixedProofValuesTypes$$|TestRubyFixedProofValuesReadRules$$|TestRubyFixedProofValuesFloatBounds$$)' ./internal/codegen/rubytable/

ruby-fixed-form-shapes: ruby-toolchain-check
	sh test/slowgate/proof ruby-fixed-form-shapes 'TestRubyFixedProofShapes TestRubyFixedProofShapesText TestRubyFixedProofShapesArrayBounds' -count=1 -timeout 600s -run '^(TestRubyFixedProofShapes$$|TestRubyFixedProofShapesText$$|TestRubyFixedProofShapesArrayBounds$$)' -skip '^TestRubyFixedProofShapes$$/^negative-controls$$' ./internal/codegen/rubytable/

ruby-fixed-form-unions-optionals: ruby-toolchain-check
	sh test/slowgate/proof ruby-fixed-form-unions-optionals 'TestRubyFixedProofUnionsOptionals' -count=1 -timeout 600s -run '^TestRubyFixedProofUnionsOptionals$$' -skip '^TestRubyFixedProofUnionsOptionals$$/^controls$$' ./internal/codegen/rubytable/

ruby-fixed-form-evolution: ruby-toolchain-check build/fixedform-corpus/.stamp
	SCHEMA_REQUIRE_CORPUS=1 sh test/slowgate/proof ruby-fixed-form-evolution 'TestRubyFixedProofEvolution TestRubyFixedProofEvolutionValues TestRubyFixedProofEvolutionStrict TestRubyFixedProofEvolutionNumeric TestRubyFixedNumericLadders TestRubyFixedProofEvolutionHostile' -count=1 -timeout 600s -run '^(TestRubyFixedProofEvolution$$|TestRubyFixedProofEvolutionValues$$|TestRubyFixedProofEvolutionStrict$$|TestRubyFixedProofEvolutionNumeric$$|TestRubyFixedNumericLadders$$|TestRubyFixedProofEvolutionHostile$$)' ./internal/codegen/rubytable/

ruby-fixed-form-kind-pairs: ruby-toolchain-check
	sh test/slowgate/proof ruby-fixed-form-kind-pairs 'TestRubyFixedProofEvolutionKindPairs' -count=1 -timeout 600s -run '^TestRubyFixedProofEvolutionKindPairs$$' ./internal/codegen/rubytable/

ruby-fixed-writer-lineage: ruby-toolchain-check
	sh test/slowgate/proof ruby-fixed-writer-lineage 'TestRubyFixedWriterLock' -count=1 -timeout 600s -run '^TestRubyFixedWriterLock$$' ./compiler/

ruby-fixed-form-negative-control: ruby-toolchain-check
	sh test/slowgate/proof ruby-fixed-form-negative-control 'TestRubyFixedFormNegativeControl' -count=1 -timeout 600s -run '^TestRubyFixedFormNegativeControl$$' ./internal/codegen/rubytable/

ruby-fixed-core-values-negative-control: ruby-toolchain-check
	sh test/slowgate/proof ruby-fixed-core-values-negative-control 'TestRubyFixedCoreValuesNegativeControl' -count=1 -timeout 600s -run '^TestRubyFixedCoreValuesNegativeControl$$' ./internal/codegen/rubytable/

ruby-fixed-proof-values-negative-control: ruby-toolchain-check
	sh test/slowgate/proof ruby-fixed-proof-values-negative-control 'TestRubyFixedProofValuesNegativeControl' -count=1 -timeout 600s -run '^TestRubyFixedProofValuesNegativeControl$$' ./internal/codegen/rubytable/

ruby-fixed-proof-shapes-negative-control: ruby-toolchain-check
	sh test/slowgate/proof ruby-fixed-proof-shapes-negative-control 'TestRubyFixedProofShapes' -count=1 -timeout 600s -run '^TestRubyFixedProofShapes$$/^negative-controls$$' ./internal/codegen/rubytable/
	sh test/slowgate/proof ruby-fixed-proof-shapes-negative-control-2 'TestRubyFixedProofShapesNegativeControl' -count=1 -timeout 600s -run '^TestRubyFixedProofShapesNegativeControl$$' ./internal/codegen/rubytable/

ruby-fixed-proof-unions-optionals-negative-control: ruby-toolchain-check
	sh test/slowgate/proof ruby-fixed-proof-unions-optionals-negative-control 'TestRubyFixedProofUnionsOptionals' -count=1 -timeout 600s -run '^TestRubyFixedProofUnionsOptionals$$/^controls$$' ./internal/codegen/rubytable/

ruby-fixed-proof-evolution-negative-control: ruby-toolchain-check build/fixedform-corpus/.stamp
	SCHEMA_REQUIRE_CORPUS=1 sh test/slowgate/proof ruby-fixed-proof-evolution-negative-control 'TestRubyFixedProofEvolutionNegativeControl TestRubyFixedProofEvolutionKindPairsNegativeControl' -count=1 -timeout 600s -run '^(TestRubyFixedProofEvolutionNegativeControl$$|TestRubyFixedProofEvolutionKindPairsNegativeControl$$)' ./internal/codegen/rubytable/

ruby-fixed-crc-negative-control: ruby-toolchain-check
	sh test/slowgate/proof ruby-fixed-crc-negative-control 'TestRubyFixedIntegrityCRCNegativeControl' -count=1 -timeout 600s -run '^TestRubyFixedIntegrityCRCNegativeControl$$' ./internal/codegen/rubytable/

ruby-fixed-refused-hash-negative-control: ruby-toolchain-check
	sh test/slowgate/proof ruby-fixed-refused-hash-negative-control 'TestRubyFixedFormRefusedHash' -count=1 -timeout 600s -run '^TestRubyFixedFormRefusedHash$$' ./internal/codegen/rubytable/

ruby-fixed-writer-lineage-negative-control: ruby-toolchain-check
	sh test/slowgate/proof ruby-fixed-writer-lineage-negative-control 'TestRubyFixedWriterLockNegativeControl' -count=1 -timeout 600s -run '^TestRubyFixedWriterLockNegativeControl$$' ./compiler/
