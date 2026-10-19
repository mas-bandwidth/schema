# make/schema-shared-hostile-corpus.mk — the shared hostile-input corpus gate.
#
# ONE CORPUS, EVERY PORT (schema-shared-hostile-corpus). The corpus is
# internal/ci/hostilecorpus: raw input files plus one line-oriented manifest
# whose verdicts are the Go engine's (internal/tablewire, internal/tabletext).
# tools/hostilecorpus rebuilds the manifest from the engine, so no generated
# reader can drift it. The Makefile includes make/*.mk; this file names no
# other card and needs no Makefile edit.

GO ?= go

.PHONY: schema-shared-hostile-corpus schema-shared-hostile-corpus-generate schema-shared-hostile-corpus-negative-control

# THE FAST GATE (push and pull request): the committed manifest still matches
# the engine, and the corpus names every refusal reason the engine can produce.
schema-shared-hostile-corpus:
	$(GO) test -count=1 -timeout 600s ./internal/ci -run 'TestSchemaSharedHostileCorpus'
	$(GO) run ./tools/hostilecorpus -check

# Regenerate the manifest from the engine after a deliberate verdict change.
schema-shared-hostile-corpus-generate:
	$(GO) run ./tools/hostilecorpus

# THE PLANTED-BAD-PATTERN CONTROL (nightly): insert a bad verdict into a copy
# of the committed corpus, require the drift gate to go red, then require the
# unplanted corpus to be green. A control that cannot go red is no control.
schema-shared-hostile-corpus-negative-control:
	$(GO) test -count=1 -timeout 600s ./internal/ci -run '^TestSchemaSharedHostileCorpusNegativeControl'
