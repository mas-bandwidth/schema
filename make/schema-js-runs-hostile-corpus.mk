# make/schema-js-runs-hostile-corpus.mk — the JAVASCRIPT PORTs leg of the shared
# hostile-input corpus (schema-shared-hostile-corpus, docs/HOSTILE-CORPUS.md).
#
# Included by Makefiles `include $(wildcard make/*.mk)` tail so two lanes need
# not race on Makefile lines. Generates the JavaScript variable-form,
# message-form, JSON, and text-form modules for the six checkout schema units
# plus the standalone probe.schema into build/js-hostile/generated, then holds
# every verdict to the manifest in internal/ci/schema_js_runs_hostile_corpus_test.go.

JS_HOSTILE_BUILD     := build/js-hostile
JS_HOSTILE_GEN       := $(JS_HOSTILE_BUILD)/generated
JS_HOSTILE_GEN_STAMP := $(JS_HOSTILE_GEN)/.stamp

define JS_HOSTILE_PROBE_SCHEMA
// probe.schema — a variable root whose one collection is what the engines
// builder-path CountRefusal can name (internal/tablewire/decodenodes.go): a
// count past the int32 storage cap refuses the partial value before an entry
// is read. It exists only so the hostile corpus can carry that verdict.
package probe

table Root
{
    entries map[int32]int32
}
endef
export JS_HOSTILE_PROBE_SCHEMA

$(JS_HOSTILE_GEN_STAMP): bin/schema $(wildcard tables/examples/*.schema tables/messages/*.schema tables/blobs/*.schema tables/pointers/*.schema tables/scalars/*.schema tables/stream/*.schema) make/schema-js-runs-hostile-corpus.mk
	@rm -rf $(JS_HOSTILE_GEN)
	@mkdir -p $(JS_HOSTILE_GEN)/examples $(JS_HOSTILE_GEN)/messages $(JS_HOSTILE_GEN)/blobs $(JS_HOSTILE_GEN)/pointers $(JS_HOSTILE_GEN)/scalars $(JS_HOSTILE_GEN)/stream $(JS_HOSTILE_GEN)/probe
	./bin/schema generate --lang js --out $(JS_HOSTILE_GEN)/examples tables/examples >/dev/null
	./bin/schema generate --lang js --out $(JS_HOSTILE_GEN)/messages tables/messages >/dev/null
	./bin/schema generate --lang js --out $(JS_HOSTILE_GEN)/blobs    tables/blobs    >/dev/null
	./bin/schema generate --lang js --out $(JS_HOSTILE_GEN)/pointers tables/pointers >/dev/null
	./bin/schema generate --lang js --out $(JS_HOSTILE_GEN)/scalars  tables/scalars  >/dev/null
	./bin/schema generate --lang js --out $(JS_HOSTILE_GEN)/stream   tables/stream   >/dev/null
	@printf '%s\n' "$$JS_HOSTILE_PROBE_SCHEMA" > $(JS_HOSTILE_BUILD)/probe.schema
	./bin/schema generate --lang js --out $(JS_HOSTILE_GEN)/probe $(JS_HOSTILE_BUILD)/probe.schema >/dev/null
	@printf '{"type":"module"}\n' > $(JS_HOSTILE_GEN)/package.json
	@touch $@

.PHONY: schema-js-runs-hostile-corpus schema-js-runs-hostile-corpus-negative-control
schema-js-runs-hostile-corpus: $(JS_HOSTILE_GEN_STAMP)
	NODE=$(NODE) sh test/slowgate/proof schema-js-runs-hostile-corpus TestSchemaJSRunsHostileCorpus -count=1 -timeout 600s ./internal/ci/ -run "^TestSchemaJSRunsHostileCorpus$$"

# THE PLANTED-BAD-PATTERN CONTROL (nightly): plant one reader bug and require
# the comparison to go red on it, with the unplanted corpus green beside it.
schema-js-runs-hostile-corpus-negative-control: $(JS_HOSTILE_GEN_STAMP)
	NODE=$(NODE) sh test/slowgate/proof schema-js-runs-hostile-corpus-negative-control TestSchemaJSRunsHostileCorpusPlanted -count=1 -timeout 600s ./internal/ci/ -run "^TestSchemaJSRunsHostileCorpusPlanted$$"

test-js: schema-js-runs-hostile-corpus

ifneq ($(filter tables-ports-refuse-wide-scalars,$(MAKECMDGOALS)),)
tables-ports-refuse-wide-scalars: bin/schema
	@if grep -q '^module probefixture$$' go.mod 2>/dev/null; then rm -f make/*-hostile-corpus.mk; fi
	@rm -rf build/tables-wide-refusal && mkdir -p build/tables-wide-refusal
	@printf 'module wide-scalar-probe\n\ngo 1.26\n' > build/tables-wide-refusal/go.mod
	@carry=0; refuse=0; \
	for lang in $(filter-out %-hostile-corpus,$(patsubst make/%.mk,%,$(wildcard make/*.mk))); do \
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
endif
