# make/schema-ruby-generated-strict-gate.mk — ruby -w, RuboCop 1.92.2, RBS 4.2.0
# and Steep 2.1.0 (D::Ruby.strict) over generated packet and table corpora
# (docs/PORT-RUBY.md; docs/plans/ruby-leg.md, R14).

.PHONY: schema-ruby-generated-strict-gate schema-ruby-generated-strict-gate-negative-control

schema-ruby-generated-strict-gate: ruby-toolchain-check
	sh test/slowgate/proof schema-ruby-generated-strict-gate 'TestRubyGeneratedStrictGate' -count=1 -timeout 600s -run '^TestRubyGeneratedStrictGate$$' ./internal/ci/

schema-ruby-generated-strict-gate-negative-control: ruby-toolchain-check
	sh test/slowgate/proof schema-ruby-generated-strict-gate-negative-control 'TestRubyGeneratedStrictGatePlantedPattern/ruby-warning TestRubyGeneratedStrictGatePlantedPattern/rubocop-offense TestRubyGeneratedStrictGatePlantedPattern/steep-type-error' -count=1 -timeout 600s -run '^TestRubyGeneratedStrictGatePlantedPattern$$' ./internal/ci/

# THE PULL-REQUEST FORM (docs/CI-TIERS.md; ci-fast.yml nightly-fast row
# ruby-strict): ruby -w on both pins and one RuboCop pass over the same units,
# with the RuboCop and ruby -w planted controls. RBS and Steep stay above, nightly.
.PHONY: schema-ruby-generated-strict-gate-fast
schema-ruby-generated-strict-gate-fast: ruby-toolchain-check
	sh test/slowgate/proof schema-ruby-generated-strict-gate-fast 'TestRubyGeneratedStrictGateFast/ruby-w TestRubyGeneratedStrictGateFast/rubocop TestRubyGeneratedStrictGatePlantedPattern/ruby-warning TestRubyGeneratedStrictGatePlantedPattern/rubocop-offense' -count=1 -timeout 100s -run '^(TestRubyGeneratedStrictGateFast|TestRubyGeneratedStrictGatePlantedPattern)$$/^(ruby-w|rubocop|ruby-warning|rubocop-offense)$$' ./internal/ci/

test-ruby: schema-ruby-generated-strict-gate schema-ruby-generated-strict-gate-negative-control
