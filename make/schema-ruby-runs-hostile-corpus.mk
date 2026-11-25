# make/schema-ruby-runs-hostile-corpus.mk — the Ruby leg's shared hostile-corpus
# runner and bounded memory/time gate (docs/HOSTILE-CORPUS.md; docs/plans/ruby-leg.md, R12).

.PHONY: schema-ruby-runs-hostile-corpus schema-ruby-runs-hostile-corpus-negative-control schema-ruby-resource-bounds schema-ruby-resource-bounds-negative-control ruby-hostile

# THE BUILD, DECLARED: the prerequisites the corpus target builds first.
# tools/schema-test builds this target before a row's deadline starts (the
# hostile harness's Build step), so what it builds is never a case's timeout;
# what the run recipe itself still compiles is paid inside the run.
.PHONY: schema-ruby-runs-hostile-corpus-build
schema-ruby-runs-hostile-corpus-build: ruby-toolchain-check

schema-ruby-runs-hostile-corpus: schema-ruby-runs-hostile-corpus-build
	$(GO_TEST) -count=1 -timeout 600s -tags schema_leg_ruby -run '^(TestHostileCorpus_ruby|TestSchemaRubyHostileScorerOutcomes)$$' ./internal/ci/

schema-ruby-runs-hostile-corpus-negative-control: ruby-toolchain-check
	$(GO_TEST) -count=1 -timeout 600s -tags schema_leg_ruby -run '^TestSchemaRubyHostileCorpusPlantedReader$$' ./internal/ci/

schema-ruby-resource-bounds: ruby-toolchain-check
	$(GO_TEST) -count=1 -timeout 600s -tags schema_leg_ruby -run '^TestSchemaRubyResourceBoundsHostileShapes$$' ./internal/ci/

schema-ruby-resource-bounds-negative-control: ruby-toolchain-check
	$(GO_TEST) -count=1 -timeout 600s -tags schema_leg_ruby -run '^TestSchemaRubyResourceBoundsPlantedPattern$$' ./internal/ci/

ruby-hostile: schema-ruby-runs-hostile-corpus schema-ruby-runs-hostile-corpus-negative-control schema-ruby-resource-bounds schema-ruby-resource-bounds-negative-control

test-ruby: schema-ruby-runs-hostile-corpus schema-ruby-runs-hostile-corpus-negative-control schema-ruby-resource-bounds schema-ruby-resource-bounds-negative-control
