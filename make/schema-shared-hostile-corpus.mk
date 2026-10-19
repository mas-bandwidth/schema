# make/schema-shared-hostile-corpus.mk — the shared hostile-input corpus gate.
#
# ONE CORPUS, EVERY PORT (schema-shared-hostile-corpus). The corpus is committed
# in internal/ci/schema_shared_hostile_corpus_test.go: raw input payloads plus
# one line-oriented manifest whose verdicts are the Go engine's
# (internal/tablewire, internal/tabletext). The generator in that file rebuilds
# the manifest from the engine, so no generated reader can drift it. The
# Makefile includes make/*.mk; this file names no other card and needs no
# Makefile edit.

GO ?= go

.PHONY: schema-shared-hostile-corpus schema-shared-hostile-corpus-generate schema-shared-hostile-corpus-negative-control

# THE FAST GATE (push and pull request): the committed manifest still matches
# the engine, and the corpus names every refusal reason the engine can produce.
schema-shared-hostile-corpus:
	$(GO) test -v -count=1 -timeout 600s ./internal/ci -run '^(TestSchemaSharedHostileCorpus|TestSchemaSharedHostileCorpusReasons)$$'

# Regenerate the manifest from the engine after a deliberate verdict change.
schema-shared-hostile-corpus-generate:
	HOSTILE_CORPUS_UPDATE=1 $(GO) test -v -count=1 -timeout 600s ./internal/ci -run '^TestSchemaSharedHostileCorpusGenerate$$'

# THE PLANTED-BAD-PATTERN CONTROL (nightly): change a verdict in the committed
# manifest, require the drift gate to go red, then require the unplanted corpus
# to be green. A control that cannot go red is no control.
schema-shared-hostile-corpus-negative-control:
	$(GO) test -v -count=1 -timeout 600s ./internal/ci -run '^TestSchemaSharedHostileCorpusNegativeControl$$'

# THE REGISTRY OVERRIDE. The Makefile defines tables-ports-refuse-wide-scalars
# before it includes make/*.mk, and that recipe walks EVERY make/<lang>.mk as a
# language leg. This file is a gate, not a language, so the recipe above is
# redefined with it filtered out. The widening of the wildcard is the only
# change; the recipe body is the Makefile's own.
tables-ports-refuse-wide-scalars: bin/schema
	@rm -rf build/tables-wide-refusal && mkdir -p build/tables-wide-refusal
	# Successful probe output is a fixture, outside the compiler's Go module.
	@printf 'module wide-scalar-probe\n\ngo 1.26\n' > build/tables-wide-refusal/go.mod
	@carry=0; refuse=0; \
	for lang in $(patsubst make/%.mk,%,$(filter-out make/schema-shared-hostile-corpus.mk,$(wildcard make/*.mk))); do \
		if ./bin/schema generate --lang $$lang --out build/tables-wide-refusal/$$lang tables/scalars > build/tables-wide-refusal/$$lang.log 2>&1; then \
			carry=$$((carry+1)); continue; \
		fi; \
		if grep -q 'is not implemented' build/tables-wide-refusal/$$lang.log && \
		   [ "$$(sed -n '1p' compiler/target_$$lang.go 2>/dev/null)" = "//go:build schema_leg_$$lang" ]; then \
			echo "tables wide-scalar: $$lang has no --lang yet (schema new-leg skeleton)"; \
			continue; \
		fi; \
		grep -q "does not carry the fixed-point and 128-bit table-wire kinds yet" build/tables-wide-refusal/$$lang.log || \
			{ echo "REFUSAL GATE FAILED: the $$lang backend stopped for another reason:"; cat build/tables-wide-refusal/$$lang.log; exit 1; }; \
		grep -q "SimState.reach fixed(112, 16)" build/tables-wide-refusal/$$lang.log || \
			{ echo "REFUSAL GATE FAILED: the $$lang backend does not name the refused field"; exit 1; }; \
		refuse=$$((refuse+1)); \
	done; \
	if [ $$((carry+refuse)) -eq 0 ]; then echo "REFUSAL GATE FAILED: no port discovered under make/"; exit 1; fi; \
	echo "tables wide-scalar refusal gate: $$refuse ports refuse the unit by name, naming every wide field and the follow-on; $$carry carry the kinds"
