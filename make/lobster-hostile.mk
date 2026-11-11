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
# FIX-CARD. A schema the Lobster target refuses is a NAMED ABSENCE: each of its
# cases is printed ABSENT with the refusal and counted, never passed. A
# disagreement no FIX-CARD names is a failure, and a FIX-CARD that has gone green is a failure
# too, so the list cannot rot into silence.
#
# The planted-bad-pattern control inverts the variable walk's root body
# verdict and makes the fixed read answer nothing, in a copy of the generated
# units, so a clean wire of either form reads as damaged, and requires the
# runner to go red naming the accepted wire case before the unplanted run is
# green again. A second plant removes the packet reader's const check in a copy
# of the packet unit, so a packet row the manifest refuses reads as accepted,
# and requires the runner to go red naming that packet case.

LOBSTER_HOSTILE_BUILD ?= build/lobster-hostile
LOBSTER_HOSTILE_PACKET_PLANT ?= $(LOBSTER_HOSTILE_BUILD)-packet-plant

.PHONY: schema-lobster-runs-hostile-corpus schema-lobster-runs-hostile-corpus-negative-control
schema-lobster-runs-hostile-corpus: lobster-dist-present
	go run ./test/conformance/lobster/hostile --units $(LOBSTER_HOSTILE_BUILD)

# the port's unit target runs the corpus (the prerequisite lands here, so
# make/lobster.mk itself is untouched)
test-lobster: schema-lobster-runs-hostile-corpus

schema-lobster-runs-hostile-corpus-negative-control: schema-lobster-runs-hostile-corpus
	@rm -rf $(LOBSTER_HOSTILE_BUILD)-plant && cp -R $(LOBSTER_HOSTILE_BUILD) $(LOBSTER_HOSTILE_BUILD)-plant
	@sed -i.orig -e 's/^        if not body_at(reader(st, 1, first), rec, false, false):$$/        if body_at(reader(st, 1, first), rec, false, false):/' \
		-e 's/^        return fixed_record_codec(root)\.read_fixed(data, report)$$/        return nil/' $(LOBSTER_HOSTILE_BUILD)-plant/*/load.lobster
	@if cmp -s $(LOBSTER_HOSTILE_BUILD)/tables_examples/load.lobster $(LOBSTER_HOSTILE_BUILD)-plant/tables_examples/load.lobster; then \
		echo 'NEGATIVE CONTROL FAILED: the planted reader bug did not apply'; exit 1; fi
	@if go run ./test/conformance/lobster/hostile --units $(LOBSTER_HOSTILE_BUILD)-plant --no-generate \
			> $(LOBSTER_HOSTILE_BUILD)-plant.log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: a load that calls a clean wire damaged left the corpus green'; \
		cat $(LOBSTER_HOSTILE_BUILD)-plant.log; exit 1; fi
	@grep -q '^FAIL wire-valid ' $(LOBSTER_HOSTILE_BUILD)-plant.log || \
		{ echo 'NEGATIVE CONTROL FAILED: the runner went red without naming the accepted wire case'; \
		  cat $(LOBSTER_HOSTILE_BUILD)-plant.log; exit 1; }
	@rm -rf $(LOBSTER_HOSTILE_PACKET_PLANT) && cp -R $(LOBSTER_HOSTILE_BUILD) $(LOBSTER_HOSTILE_PACKET_PLANT)
	@set -- $(LOBSTER_HOSTILE_PACKET_PLANT)/*/hostile.lobster; \
	 if [ "$$#" -ne 1 ] || [ ! -f "$$1" ]; then \
		echo 'NEGATIVE CONTROL FAILED: the packet unit module is missing'; exit 1; fi; \
	 dst=$$1; src=$(LOBSTER_HOSTILE_BUILD)/$${dst#$(LOBSTER_HOSTILE_PACKET_PLANT)/}; \
	 if [ ! -f "$$src" ]; then \
		echo 'NEGATIVE CONTROL FAILED: the unplanted packet unit module is missing'; exit 1; fi; \
	 sed -i.orig -e 's/^    if rv != 171: return stream\.fail(serialize_refusal_value_out_of_range).*/    if rv == 300: return stream.fail(serialize_refusal_value_out_of_range)/' "$$dst"; \
	 if cmp -s "$$src" "$$dst"; then \
		echo 'NEGATIVE CONTROL FAILED: the planted packet const-read bug did not apply'; exit 1; fi
	@if go run ./test/conformance/lobster/hostile --units $(LOBSTER_HOSTILE_PACKET_PLANT) --no-generate \
			> $(LOBSTER_HOSTILE_PACKET_PLANT).log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: a packet reader that admits a wrong const left the corpus green'; \
		cat $(LOBSTER_HOSTILE_PACKET_PLANT).log; exit 1; fi
	@grep -q '^FAIL packet-const-wrong ' $(LOBSTER_HOSTILE_PACKET_PLANT).log || \
		{ echo 'NEGATIVE CONTROL FAILED: the runner went red without naming the refused packet const case'; \
		  cat $(LOBSTER_HOSTILE_PACKET_PLANT).log; exit 1; }
	@$(MAKE) --no-print-directory schema-lobster-runs-hostile-corpus
	@echo 'lobster hostile-corpus negative control: a load that calls a clean wire damaged and a packet reader that admits a wrong const red the corpus; unplanted it is green'
