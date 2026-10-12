# THE CARD WIRE-FUZZ GATE, RETAINED (docs/SPEC-TABLES.md §6.6): the same Card
# root through both engines' RETAINING paths — the driver answers --retain with
# card_load_retain and card_save_retain over the caller-owned stores — compared
# against the oracle's own retention. The seed, count and coverage greps are the
# wire-fuzz arm's (make/rust.mk cards-rust-wire-fuzz). The facade card owns
# make/cards-rust.mk; this retain arm lives in its own fragment so the two cards
# extend the Rust leg in parallel without editing each other's file or
# make/rust.mk.
CARDS_RUST_FUZZ_TIME ?= 60s

.PHONY: cards-rust-retain-fuzz
cards-rust-retain-fuzz: build/conformance-harness build/conformance-rust-card
	@timeout $(CARDS_RUST_FUZZ_TIME) ./build/conformance-harness wire-fuzz \
		--manifest tables/cards/fuzz/MANIFEST.txt --json tables/cards/fuzz --vectors '' \
		--driver 'build/conformance-rust-card wire-fuzz' --retain --seed 24845619678 --n 100 \
		> build/cards-rust-retain-fuzz.log 2>&1; \
		status=$$?; cat build/cards-rust-retain-fuzz.log; \
		if [ $$status -ne 0 ]; then exit $$status; fi
	@grep -q '0 divergences' build/cards-rust-retain-fuzz.log
	@grep -qE '[1-9][0-9]* seeds over [1-9][0-9]* roots' build/cards-rust-retain-fuzz.log
	@grep -qE '[1-9][0-9]* mutants' build/cards-rust-retain-fuzz.log

test-rust: cards-rust-retain-fuzz
