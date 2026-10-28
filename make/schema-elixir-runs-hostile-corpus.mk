# make/schema-elixir-runs-hostile-corpus.mk — the Elixir port runs the shared
# hostile-input corpus (schema-elixir-runs-hostile-corpus).
#
# The corpus is the committed portable directory docs/HOSTILE-CORPUS.md names:
# internal/ci/hostilecorpus/corpus/manifest.tsv plus one raw input per line.
# This target generates the units the manifest names, compiles them with the
# pinned BEAM toolchain, and hands every input to the port's own reader
# (test/elixir-hostile/runner.exs). Reads files only: no network, no clock.
#
# A disagreement the port already owes is carried, by name, in
# test/elixir-hostile/known-findings.tsv, and printed as a FIX-CARD. A
# disagreement no FIX-CARD names is a failure, and a FIX-CARD that has gone
# green is a failure too, so the list cannot rot into silence.
#
# The planted-bad-pattern control (nightly) changes the generated text reader
# in a scratch copy so an accepted input is refused, and requires the runner to
# go red before the unplanted target is green again.

ELIXIR_HOSTILE_CORPUS ?= internal/ci/hostilecorpus/corpus
ELIXIR_HOSTILE_BUILD  ?= build/elixir-hostile
ELIXIR_HOSTILE_FINDINGS ?= test/elixir-hostile/known-findings.tsv

ELIXIR_HOSTILE_UNITS := tabledemo:tables/examples \
	messagedemo:tables/messages blobdemo:tables/blobs \
	graphdemo:tables/pointers scalardemo:tables/scalars \
	streamdemo:tables/stream mapdemo:tables/maps listdemo:tables/lists probe:test/elixir-hostile/probe.schema

$(ELIXIR_HOSTILE_BUILD)/generated/.stamp: bin/schema make/schema-elixir-runs-hostile-corpus.mk \
		$(wildcard tables/examples/*.schema tables/messages/*.schema tables/blobs/*.schema \
			tables/pointers/*.schema tables/scalars/*.schema tables/stream/*.schema \
			tables/maps/*.schema tables/lists/*.schema) \
		test/elixir-hostile/probe.schema
	@rm -rf $(ELIXIR_HOSTILE_BUILD)/generated && mkdir -p $(ELIXIR_HOSTILE_BUILD)/generated
	@for unit in $(ELIXIR_HOSTILE_UNITS); do \
		name=$${unit%%:*}; path=$${unit#*:}; \
		./bin/schema generate --lang elixir --out $(ELIXIR_HOSTILE_BUILD)/generated/$$name $$path || exit 1; \
	done
	@touch $@

$(ELIXIR_HOSTILE_BUILD)/ebin/.stamp: $(ELIXIR_HOSTILE_BUILD)/generated/.stamp
	@rm -rf $(ELIXIR_HOSTILE_BUILD)/ebin && mkdir -p $(ELIXIR_HOSTILE_BUILD)/ebin
	$(ELIXIRC) -o $(ELIXIR_HOSTILE_BUILD)/ebin $(ELIXIR_HOSTILE_BUILD)/generated/*/*.ex
	@touch $@

.PHONY: schema-elixir-runs-hostile-corpus
schema-elixir-runs-hostile-corpus: $(ELIXIR_HOSTILE_BUILD)/ebin/.stamp
	SCHEMA_ELIXIR_HOSTILE_FINDINGS=$(ELIXIR_HOSTILE_FINDINGS) \
		$(ELIXIR) test/elixir-hostile/runner.exs "$(ELIXIR_HOSTILE_CORPUS)" \
		"$(CURDIR)/$(ELIXIR_HOSTILE_BUILD)/ebin"

# The port's normal unit target runs the corpus: this file is included after
# make/elixir.mk by the Makefile's wildcard, so the prerequisite lands here and
# make/elixir.mk itself is untouched.
test-elixir: schema-elixir-runs-hostile-corpus

.PHONY: schema-elixir-runs-hostile-corpus-negative-control
schema-elixir-runs-hostile-corpus-negative-control: $(ELIXIR_HOSTILE_BUILD)/ebin/.stamp
	@rm -rf $(ELIXIR_HOSTILE_BUILD)/plant && mkdir -p $(ELIXIR_HOSTILE_BUILD)/plant/generated $(ELIXIR_HOSTILE_BUILD)/plant/ebin
	@cp -R $(ELIXIR_HOSTILE_BUILD)/generated/* $(ELIXIR_HOSTILE_BUILD)/plant/generated/
	@sed -i 's|{:ok, Map.merge(defaults(desc), value), report}|{:error, :malformed, report}|' \
		$(ELIXIR_HOSTILE_BUILD)/plant/generated/*/Table.ex
	@if cmp -s $(ELIXIR_HOSTILE_BUILD)/generated/tabledemo/Table.ex $(ELIXIR_HOSTILE_BUILD)/plant/generated/tabledemo/Table.ex; then \
		echo 'NEGATIVE CONTROL FAILED: the planted reader bug did not apply'; exit 1; fi
	$(ELIXIRC) -o $(ELIXIR_HOSTILE_BUILD)/plant/ebin $(ELIXIR_HOSTILE_BUILD)/plant/generated/*/*.ex
	@if SCHEMA_ELIXIR_HOSTILE_FINDINGS=$(ELIXIR_HOSTILE_FINDINGS) \
			$(ELIXIR) test/elixir-hostile/runner.exs "$(ELIXIR_HOSTILE_CORPUS)" \
			"$(CURDIR)/$(ELIXIR_HOSTILE_BUILD)/plant/ebin" \
			> $(ELIXIR_HOSTILE_BUILD)/plant/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: a text reader that refuses accepted inputs left the corpus green'; \
		cat $(ELIXIR_HOSTILE_BUILD)/plant/log; exit 1; fi
	@grep -q '^FAIL ' $(ELIXIR_HOSTILE_BUILD)/plant/log || \
		{ echo 'NEGATIVE CONTROL FAILED: the runner went red without naming an unexplained case'; \
		  cat $(ELIXIR_HOSTILE_BUILD)/plant/log; exit 1; }
	@$(MAKE) --no-print-directory schema-elixir-runs-hostile-corpus
	@echo 'elixir hostile-corpus negative control: a text reader that refuses accepted inputs reds the corpus; unplanted it is green'
