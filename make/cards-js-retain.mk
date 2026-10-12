# make/cards-js-retain.mk — the JavaScript card root under the RETAINED fuzzer
# (docs/SPEC-TABLES.md §6.6, §4.2). Kept apart from make/cards-js.mk, which the
# facade card extends in parallel: this fragment is the retain leg alone.
#
# It mirrors make/js.mk's cards-js-wire-fuzz over the SAME manifest, seed and
# coverage greps, with --retain, so the JavaScript Card codec's retaining load
# and save are held to the identical byte-for-byte contract the oracle's retain
# arm (§6.6) reproduces. `test-js: cards-js-retain-fuzz` adds the target to the
# leg's gate without editing make/js.mk.
.PHONY: cards-js-retain-fuzz

cards-js-retain-fuzz: build/conformance-harness cards-js-generate
	@./build/conformance-harness wire-fuzz \
		--manifest tables/cards/fuzz/MANIFEST.txt --json tables/cards/fuzz --vectors '' \
		--retain \
		--driver 'test/conformance/js/driver wire-fuzz' --seed 24845619678 --n 100 \
		> build/cards-js-retain-fuzz.log 2>&1; \
		status=$$?; cat build/cards-js-retain-fuzz.log; \
		if [ $$status -ne 0 ]; then exit $$status; fi
	@grep -q '0 divergences' build/cards-js-retain-fuzz.log
	@grep -qE '[1-9][0-9]* seeds over [1-9][0-9]* roots' build/cards-js-retain-fuzz.log
	@grep -qE '[1-9][0-9]* mutants' build/cards-js-retain-fuzz.log

test-js: cards-js-retain-fuzz
