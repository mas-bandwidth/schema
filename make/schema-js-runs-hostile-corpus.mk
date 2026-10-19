# make/schema-js-runs-hostile-corpus.mk — the JAVASCRIPT PORTs leg of the shared
# hostile-input corpus (schema-shared-hostile-corpus, docs/HOSTILE-CORPUS.md).
#
# The Makefile's existing `include $(wildcard make/*.mk)` picks this file up,
# so the card needs no Makefile edit and cannot race another lane on one.
# Generates the JavaScript variable-form, message-form, JSON, and text-form
# modules for the six checkout schema units
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

