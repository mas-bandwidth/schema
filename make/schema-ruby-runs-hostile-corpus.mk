# make/schema-ruby-runs-hostile-corpus.mk — the Ruby leg's shared hostile-corpus
# runner and bounded memory/time gate (docs/HOSTILE-CORPUS.md; docs/plans/ruby-leg.md, R12).

.PHONY: schema-ruby-runs-hostile-corpus schema-ruby-runs-hostile-corpus-negative-control schema-ruby-resource-bounds schema-ruby-resource-bounds-negative-control ruby-hostile

schema-ruby-runs-hostile-corpus: ruby-toolchain-check
	SCHEMA_SLOW=1 $(GOTESTRUN) -count=1 -timeout 600s -tags schema_leg_ruby -run '^(TestSchemaRubyRunsHostileCorpus|TestSchemaRubyHostileScorerOutcomes)$$' ./internal/ci/

schema-ruby-runs-hostile-corpus-negative-control: ruby-toolchain-check
	SCHEMA_SLOW=1 $(GOTESTRUN) -count=1 -timeout 600s -tags schema_leg_ruby -run '^TestSchemaRubyHostileCorpusPlantedReader$$' ./internal/ci/

schema-ruby-resource-bounds: ruby-toolchain-check
	SCHEMA_SLOW=1 $(GOTESTRUN) -count=1 -timeout 600s -tags schema_leg_ruby -run '^TestSchemaRubyResourceBoundsHostileShapes$$' ./internal/ci/

schema-ruby-resource-bounds-negative-control: ruby-toolchain-check
	SCHEMA_SLOW=1 $(GOTESTRUN) -count=1 -timeout 600s -tags schema_leg_ruby -run '^TestSchemaRubyResourceBoundsPlantedPattern$$' ./internal/ci/

ruby-hostile: schema-ruby-runs-hostile-corpus schema-ruby-runs-hostile-corpus-negative-control schema-ruby-resource-bounds schema-ruby-resource-bounds-negative-control

test-ruby: schema-ruby-runs-hostile-corpus schema-ruby-runs-hostile-corpus-negative-control schema-ruby-resource-bounds schema-ruby-resource-bounds-negative-control
