# cards-js.mk - The cards unit's JavaScript table backend

# Generate the cards unit
.PHONY: cards-js-generate
cards-js-generate: bin/schema
	rm -rf build/tables-generated-js/cards
	mkdir -p build/tables-generated-js/cards
	./bin/schema generate --lang js --out build/tables-generated-js/cards tables/cards

# THE CARD WIRE-FUZZ GATE (docs/SPEC-TABLES.md §4.2): the oracle harness over the
# Card root alone, answered by this leg's variable-form codec. --vectors '' keeps
# the other units' pinned vectors out of this Card-only manifest; the greps below
# hold the nonzero seed/mutant claim the same way make/go.mk's cards-wire-fuzz
# does, so a zero-coverage roster fails rather than printing a hollow pass.
.PHONY: cards-js-wire-fuzz
cards-js-wire-fuzz: build/conformance-harness cards-js-generate
	@timeout $(CARDS_FUZZ_TIME) ./build/conformance-harness wire-fuzz \
		--manifest tables/cards/fuzz/MANIFEST.txt --json tables/cards/fuzz --vectors '' \
		--driver 'test/conformance/js/driver wire-fuzz' --seed 24845619678 --n 100 \
		> build/cards-js-wire-fuzz.log 2>&1; \
		status=$$?; cat build/cards-js-wire-fuzz.log; \
		if [ $$status -ne 0 ]; then exit $$status; fi
	@grep -q '0 divergences' build/cards-js-wire-fuzz.log
	@grep -qE '[1-9][0-9]* seeds over [1-9][0-9]* roots' build/cards-js-wire-fuzz.log
	@grep -qE '[1-9][0-9]* mutants' build/cards-js-wire-fuzz.log

# The js leg's table gate (make/js.mk's test-js) plus the Card root in the
# oracle fuzzer.
test-js: cards-js-wire-fuzz
