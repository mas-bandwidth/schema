# make/checks/schema-shared-hostile-corpus.mk — the shared hostile-input corpus gate.
#
# ONE CORPUS, EVERY PORT (schema-shared-hostile-corpus). The corpus is committed
# at internal/ci/hostilecorpus/corpus: one raw file per input under inputs/ and
# one line-oriented manifest whose verdicts are the Go engine's
# (internal/tablewire, internal/tabletext). The generator in
# tools/hostilecorpus rebuilds the manifest from the engine, so no generated
# reader can drift it. The Makefile includes this file explicitly beside the
# other make/checks gates.

GO ?= go

.PHONY: schema-shared-hostile-corpus schema-shared-hostile-corpus-generate schema-shared-hostile-corpus-negative-control

# THE FAST GATE (push and pull request): the committed manifest still matches
# the engine, and the corpus names every refusal reason the engine can produce.
schema-shared-hostile-corpus:
	$(GO) test -v -count=1 -timeout 600s ./internal/ci -run '^(TestHostileRefusalReasonFailsClosed|TestSchemaSharedHostileCorpus|TestSchemaSharedHostileCorpusReasons|TestSchemaSharedHostileCorpusSources|TestSchemaSharedHostileCorpusExcludedSources)$$'

# Regenerate the manifest from the engine after a deliberate verdict change.
schema-shared-hostile-corpus-generate:
	$(GO) run ./tools/hostilecorpus generate

# THE PLANTED-BAD-PATTERN CONTROL (nightly): change a verdict in the committed
# manifest, require the drift gate to go red, then require the unplanted corpus
# to be green. A control that cannot go red is no control.
schema-shared-hostile-corpus-negative-control:
	$(GO) test -v -count=1 -timeout 600s ./internal/ci -run '^(TestSchemaSharedHostileCorpusNegativeControl|TestSchemaSharedHostileCorpusSourcesNegativeControl)$$'

