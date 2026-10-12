# make/cards-elixir.mk — the Elixir leg's card-table targets
# (docs/SPEC-TABLES.md §2, §3.1). Included by the Makefile's wildcard
# include; it registers the card targets on the leg's own gate, so the
# variable-form slice this backend carries is gated without editing
# make/elixir.mk.

.PHONY: cards-elixir-generate cards-elixir-wire-fuzz

# cards-elixir-generate emits the Elixir table surface for tables/cards.
# The packet emitter's name check no longer refuses the `Card` table for
# colliding with the module it writes for schema file Card.schema
# (docs/SPEC-TABLES.md §11): a table lowers to a module in the TABLE backend,
# so the packet emitter skips it and the command reaches elixirtable.
cards-elixir-generate:
	@rm -rf build/tables-generated-elixir/cards
	@mkdir -p build/tables-generated-elixir/cards
	./bin/schema generate --lang elixir --out build/tables-generated-elixir/cards tables/cards

# the cards corpus and the driver compiled into one ebin, so the wire-fuzz
# driver's one BEAM can name the Cards.Table codec it drives.
build/elixir-cards-ebin/.stamp: cards-elixir-generate test/conformance/elixir/driver_impl.ex
	@rm -rf build/elixir-cards-ebin && mkdir -p build/elixir-cards-ebin
	$(ELIXIRC) -o build/elixir-cards-ebin build/tables-generated-elixir/cards/*.ex \
		test/conformance/elixir/driver_impl.ex
	@touch $@

# cards-elixir-wire-fuzz runs the conformance harness's wire fuzzer over the
# Card root, comparing the Elixir driver's reads and writes with the Go
# reference leg's. It requires cards-elixir-generate to have succeeded first.
# The gate refuses a zero-coverage roster: the harness errors when the leg has
# a codec for none of the named roots, and the greps below hold the nonzero
# seed/mutant claim explicitly.
cards-elixir-wire-fuzz: build/conformance-harness build/elixir-cards-ebin/.stamp
	@ELIXIR_TABLES_EBIN=$(CURDIR)/build/elixir-cards-ebin \
		./build/conformance-harness wire-fuzz \
		--manifest tables/cards/fuzz/MANIFEST.txt --json tables/cards/fuzz --vectors '' \
		--driver 'test/conformance/elixir/driver wire-fuzz' --seed 24845619678 --n 100 \
		> build/cards-elixir-wire-fuzz.log 2>&1; \
		status=$$?; cat build/cards-elixir-wire-fuzz.log; \
		if [ $$status -ne 0 ]; then exit $$status; fi
	@grep -q '0 divergences' build/cards-elixir-wire-fuzz.log
	@grep -qE '[1-9][0-9]* seeds over [1-9][0-9]* roots' build/cards-elixir-wire-fuzz.log
	@grep -qE '[1-9][0-9]* mutants' build/cards-elixir-wire-fuzz.log

test-elixir: cards-elixir-wire-fuzz
