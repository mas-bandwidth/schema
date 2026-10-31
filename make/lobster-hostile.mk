# make/lobster-hostile.mk — the Lobster port runs the shared hostile-input
# corpus (schema-lobster-runs-hostile-corpus; docs/HOSTILE-CORPUS.md).
#
# The corpus is the committed portable directory: internal/ci/hostilecorpus/
# corpus/manifest.tsv plus one raw input per line. The runner
# (test/conformance/lobster/hostile) generates the Lobster unit of every schema
# the manifest names into build/lobster-hostile, runs
# test/conformance/lobster/hostile.lobster once per unit under the pinned
# toolchain, and holds every answer to the engine's committed verdict. Reads
# files only: no network, no clock.
#
# A disagreement the port already owes is carried, by name, in
# test/conformance/lobster/hostile-known-findings.tsv and printed as a
# FIX-CARD (none today: all 161 cases agree, over every carrier). A
# disagreement no FIX-CARD names is a failure, and a FIX-CARD that has gone green is a failure
# too, so the list cannot rot into silence.
#
# The planted-bad-pattern control flips the load's stopped verdict in a copy
# of the generated units, so a clean wire reads as damaged, and requires the
# runner to go red naming the accepted wire case before the unplanted run is
# green again.

LOBSTER_HOSTILE_BUILD ?= build/lobster-hostile

.PHONY: schema-lobster-runs-hostile-corpus schema-lobster-runs-hostile-corpus-negative-control
schema-lobster-runs-hostile-corpus: lobster-dist-present
	go run ./test/conformance/lobster/hostile --units $(LOBSTER_HOSTILE_BUILD)

# the port's unit target runs the corpus (the prerequisite lands here, so
# make/lobster.mk itself is untouched)
test-lobster: schema-lobster-runs-hostile-corpus

schema-lobster-runs-hostile-corpus-negative-control: schema-lobster-runs-hostile-corpus
	@rm -rf $(LOBSTER_HOSTILE_BUILD)-plant && cp -R $(LOBSTER_HOSTILE_BUILD) $(LOBSTER_HOSTILE_BUILD)-plant
	@sed -i.orig 's/report\.stopped = not body_at(/report.stopped = body_at(/' $(LOBSTER_HOSTILE_BUILD)-plant/*/load.lobster
	@if cmp -s $(LOBSTER_HOSTILE_BUILD)/tables_examples/load.lobster $(LOBSTER_HOSTILE_BUILD)-plant/tables_examples/load.lobster; then \
		echo 'NEGATIVE CONTROL FAILED: the planted reader bug did not apply'; exit 1; fi
	@if go run ./test/conformance/lobster/hostile --units $(LOBSTER_HOSTILE_BUILD)-plant --no-generate \
			> $(LOBSTER_HOSTILE_BUILD)-plant.log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: a load that calls a clean wire damaged left the corpus green'; \
		cat $(LOBSTER_HOSTILE_BUILD)-plant.log; exit 1; fi
	@grep -q '^FAIL wire-valid ' $(LOBSTER_HOSTILE_BUILD)-plant.log || \
		{ echo 'NEGATIVE CONTROL FAILED: the runner went red without naming the accepted wire case'; \
		  cat $(LOBSTER_HOSTILE_BUILD)-plant.log; exit 1; }
	@$(MAKE) --no-print-directory schema-lobster-runs-hostile-corpus
	@echo 'lobster hostile-corpus negative control: a load that calls a clean wire damaged reds the corpus; unplanted it is green'
