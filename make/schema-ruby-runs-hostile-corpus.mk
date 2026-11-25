# make/schema-ruby-runs-hostile-corpus.mk — the Ruby leg's shared hostile-corpus
# runner and bounded memory/time gate (docs/HOSTILE-CORPUS.md; docs/plans/ruby-leg.md, R12).

.PHONY: schema-ruby-runs-hostile-corpus schema-ruby-runs-hostile-corpus-negative-control schema-ruby-resource-bounds schema-ruby-resource-bounds-negative-control ruby-hostile

schema-ruby-runs-hostile-corpus: ruby-toolchain-check

schema-ruby-runs-hostile-corpus-negative-control: ruby-toolchain-check

schema-ruby-resource-bounds: ruby-toolchain-check

schema-ruby-resource-bounds-negative-control: ruby-toolchain-check

ruby-hostile: schema-ruby-runs-hostile-corpus schema-ruby-runs-hostile-corpus-negative-control schema-ruby-resource-bounds schema-ruby-resource-bounds-negative-control

test-ruby: schema-ruby-runs-hostile-corpus schema-ruby-runs-hostile-corpus-negative-control schema-ruby-resource-bounds schema-ruby-resource-bounds-negative-control
