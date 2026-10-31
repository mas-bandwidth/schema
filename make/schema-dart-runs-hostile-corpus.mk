# make/schema-dart-runs-hostile-corpus.mk — the Dart port runs the shared
# hostile-input corpus (docs/HOSTILE-CORPUS.md), the twin of
# make/schema-elixir-runs-hostile-corpus.mk.
#
# The corpus is the committed portable directory
# internal/ci/hostilecorpus/corpus: manifest.tsv plus one raw input per line,
# with the Go engine's verdicts. This target generates the units the manifest
# names and hands every input to the port's own reader
# (test/dart-hostile/runner.dart). Reads files only: no network, no clock.
#
# A disagreement the port already owes is carried, by name, in
# test/dart-hostile/known-findings.tsv and printed as a FIX-CARD. A disagreement
# no FIX-CARD names is a failure, and a FIX-CARD that has gone green is a failure
# too, so the list cannot rot into silence.
#
# The planted-bad-pattern control changes the generated text reader in a
# scratch copy so every accepted text input is refused, and requires the runner
# to go red naming an unexplained case, and no wire or announce case (message-valid
# states its decoded defaults through the text read of `{}`, so it reddens too).
# It runs the unplanted target first (its
# prerequisite), so the pull-request negative-control row carries both halves.

DART_HOSTILE_CORPUS   ?= internal/ci/hostilecorpus/corpus
DART_HOSTILE_BUILD    := build/dart-hostile
DART_HOSTILE_FINDINGS ?= test/dart-hostile/known-findings.tsv

DART_HOSTILE_UNITS := tabledemo:tables/examples \
	messagedemo:tables/messages blobdemo:tables/blobs \
	graphdemo:tables/pointers scalardemo:tables/scalars \
	streamdemo:tables/stream mapdemo:tables/maps listdemo:tables/lists probe:test/dart-hostile/probe.schema

$(DART_HOSTILE_BUILD)/generated/.stamp: bin/schema make/schema-dart-runs-hostile-corpus.mk \
		$(wildcard tables/examples/*.schema tables/messages/*.schema tables/blobs/*.schema \
			tables/pointers/*.schema tables/scalars/*.schema tables/stream/*.schema \
			tables/maps/*.schema tables/lists/*.schema) \
		test/dart-hostile/probe.schema
	@rm -rf $(DART_HOSTILE_BUILD)/generated && mkdir -p $(DART_HOSTILE_BUILD)/generated
	@for unit in $(DART_HOSTILE_UNITS); do \
		name=$${unit%%:*}; path=$${unit#*:}; \
		./bin/schema generate --lang dart --out $(DART_HOSTILE_BUILD)/generated/$$name $$path >/dev/null || exit 1; \
	done
	@touch $@

.PHONY: schema-dart-runs-hostile-corpus
schema-dart-runs-hostile-corpus: $(DART_HOSTILE_BUILD)/generated/.stamp
	$(DART) run test/dart-hostile/runner.dart "$(DART_HOSTILE_CORPUS)" "$(DART_HOSTILE_FINDINGS)"

test-dart: schema-dart-runs-hostile-corpus

.PHONY: schema-dart-runs-hostile-corpus-negative-control
schema-dart-runs-hostile-corpus-negative-control: schema-dart-runs-hostile-corpus
	@rm -rf $(DART_HOSTILE_BUILD)/plant && mkdir -p $(DART_HOSTILE_BUILD)/plant/build/dart-hostile $(DART_HOSTILE_BUILD)/plant/test/dart-hostile
	@cp -R $(DART_HOSTILE_BUILD)/generated $(DART_HOSTILE_BUILD)/plant/build/dart-hostile/generated
	@cp test/dart-hostile/runner.dart $(DART_HOSTILE_BUILD)/plant/test/dart-hostile/runner.dart
	@sed -i.orig "s|^  final root = _jsonDecodeExact(_jsonPrepare(text, r));|  final root = _jsonDecodeExact(_jsonPrepare(text, r)); throw const FormatException('PLANTED');|" \
		$(DART_HOSTILE_BUILD)/plant/build/dart-hostile/generated/*/*Wire.dart
	@if cmp -s $(DART_HOSTILE_BUILD)/generated/tabledemo/TabledemoWire.dart $(DART_HOSTILE_BUILD)/plant/build/dart-hostile/generated/tabledemo/TabledemoWire.dart; then \
		echo 'NEGATIVE CONTROL FAILED: the planted reader bug did not apply'; exit 1; fi
	@if $(DART) run $(DART_HOSTILE_BUILD)/plant/test/dart-hostile/runner.dart "$(DART_HOSTILE_CORPUS)" "$(DART_HOSTILE_FINDINGS)" \
			> $(DART_HOSTILE_BUILD)/plant/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: a text reader that refuses accepted inputs left the corpus green'; \
		cat $(DART_HOSTILE_BUILD)/plant/log; exit 1; fi
	@grep -q '^FAIL conformance-' $(DART_HOSTILE_BUILD)/plant/log || \
		{ echo 'NEGATIVE CONTROL FAILED: the runner went red without naming an unexplained text case'; \
		  cat $(DART_HOSTILE_BUILD)/plant/log; exit 1; }
	@if grep '^FAIL \(wire\|message\|announce\)-' $(DART_HOSTILE_BUILD)/plant/log | grep -v '^FAIL message-valid '; then \
		echo 'NEGATIVE CONTROL FAILED: a text-reader plant turned a wire, message or announce case red'; \
		cat $(DART_HOSTILE_BUILD)/plant/log; exit 1; fi
	@tail -1 $(DART_HOSTILE_BUILD)/plant/log
	@echo 'dart hostile-corpus negative control: a text reader that refuses accepted inputs reds the corpus; unplanted it is green'
