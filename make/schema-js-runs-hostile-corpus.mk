# make/schema-js-runs-hostile-corpus.mk — the JavaScript port runs the shared
# hostile-input corpus (docs/HOSTILE-CORPUS.md), the twin of
# make/schema-dart-runs-hostile-corpus.mk.
#
# The corpus is the committed portable directory
# internal/ci/hostilecorpus/corpus: manifest.tsv plus one raw input per line,
# with the Go engine's verdicts. This target generates one JavaScript unit per
# schema the manifest names and hands every input to the port's own readers
# (test/js-hostile/runner.mjs): the table readers for the table carriers, and
# for a packet case both read tiers (the serialize.js runtime tier and the flat
# tier), each under NODE_ENV=development and NODE_ENV=production. Every row runs
# on an isolated worker thread with a per-row time budget
# (HOSTILE_CASE_BUDGET_MS, default 5000). Reads files only: no network, no clock.
#
# The runtime tier imports the pinned serialize.js sibling checkout
# (../serialize.js, SERIALIZE_JS_TAG in the workflows), as every JS test leg does.
#
# A disagreement the port already owes is carried, by name, in
# test/js-hostile/known-findings.tsv and printed as a FIX-CARD. A disagreement no
# FIX-CARD names is a failure, and a FIX-CARD that has gone green is a failure
# too, so the list cannot rot into silence.
#
# The negative control plants a changed verdict in a scratch copy of the
# manifest (wire-valid's accept turned into a refusal) and requires the runner
# to go red naming that case and no other. It runs the unplanted target first
# (its prerequisite), so the control carries both halves.

JS_HOSTILE_CORPUS    ?= internal/ci/hostilecorpus/corpus
JS_HOSTILE_BUILD     := build/js-hostile
JS_HOSTILE_FINDINGS  ?= test/js-hostile/known-findings.tsv
JS_HOSTILE_SERIALIZE ?= ../serialize.js

.PHONY: schema-js-hostile-generate
schema-js-hostile-generate: bin/schema
	@rm -rf $(JS_HOSTILE_BUILD)/generated && mkdir -p $(JS_HOSTILE_BUILD)/generated
	@$(NODE) test/js-hostile/runner.mjs generate "$(JS_HOSTILE_CORPUS)" $(JS_HOSTILE_BUILD)/generated ./bin/schema >/dev/null

.PHONY: schema-js-runs-hostile-corpus
schema-js-runs-hostile-corpus: schema-js-hostile-generate
	$(NODE) test/js-hostile/runner.mjs run "$(JS_HOSTILE_CORPUS)" $(JS_HOSTILE_BUILD)/generated \
		"$(JS_HOSTILE_SERIALIZE)" "$(JS_HOSTILE_FINDINGS)"

test-js: schema-js-runs-hostile-corpus

.PHONY: schema-js-runs-hostile-corpus-negative-control
schema-js-runs-hostile-corpus-negative-control: schema-js-runs-hostile-corpus
	@rm -rf $(JS_HOSTILE_BUILD)/plant && mkdir -p $(JS_HOSTILE_BUILD)/plant
	@cp -R "$(JS_HOSTILE_CORPUS)" $(JS_HOSTILE_BUILD)/plant/corpus
	@awk -F'\t' 'BEGIN { OFS = "\t" } $$1 == "wire-valid" && $$7 == "accept" { $$7 = "refuse"; $$8 = "damaged"; $$9 = "-" } { print }' \
		"$(JS_HOSTILE_CORPUS)/manifest.tsv" > $(JS_HOSTILE_BUILD)/plant/corpus/manifest.tsv
	@if cmp -s "$(JS_HOSTILE_CORPUS)/manifest.tsv" $(JS_HOSTILE_BUILD)/plant/corpus/manifest.tsv; then \
		echo 'NEGATIVE CONTROL FAILED: the planted verdict did not apply'; exit 1; fi
	@if $(NODE) test/js-hostile/runner.mjs run $(JS_HOSTILE_BUILD)/plant/corpus $(JS_HOSTILE_BUILD)/generated \
			"$(JS_HOSTILE_SERIALIZE)" "$(JS_HOSTILE_FINDINGS)" > $(JS_HOSTILE_BUILD)/plant/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: a changed verdict left the corpus green'; \
		cat $(JS_HOSTILE_BUILD)/plant/log; exit 1; fi
	@grep -q '^FAIL wire-valid ' $(JS_HOSTILE_BUILD)/plant/log || \
		{ echo 'NEGATIVE CONTROL FAILED: the runner went red without naming the planted case'; \
		  cat $(JS_HOSTILE_BUILD)/plant/log; exit 1; }
	@if grep '^FAIL ' $(JS_HOSTILE_BUILD)/plant/log | grep -v '^FAIL wire-valid '; then \
		echo 'NEGATIVE CONTROL FAILED: the plant turned another row red'; \
		cat $(JS_HOSTILE_BUILD)/plant/log; exit 1; fi
	@tail -1 $(JS_HOSTILE_BUILD)/plant/log
	@echo 'js hostile-corpus negative control: a changed verdict reds the corpus on exactly its row; unplanted it is green'
