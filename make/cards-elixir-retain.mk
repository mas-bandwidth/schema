# cards-elixir-retain-fuzz is the SAME fuzzer through the RETENTION ARM
# (docs/SPEC-TABLES.md §6.6): every mutant is read and written back through the
# Card codec's retaining pair, and the two retention counters ride beside the
# six. The seed, count and coverage greps are cards-elixir-wire-fuzz's, plus
# --retain, so a retained save is held to the Go reference leg byte for byte.
cards-elixir-retain-fuzz: build/conformance-harness build/elixir-cards-ebin/.stamp
	@ELIXIR_TABLES_EBIN=$(CURDIR)/build/elixir-cards-ebin \
		./build/conformance-harness wire-fuzz \
		--manifest tables/cards/fuzz/MANIFEST.txt --json tables/cards/fuzz --vectors '' \
		--driver 'test/conformance/elixir/driver wire-fuzz' --seed 24845619678 --n 100 --retain \
		> build/cards-elixir-retain-fuzz.log 2>&1; \
		status=$$?; cat build/cards-elixir-retain-fuzz.log; \
		if [ $$status -ne 0 ]; then exit $$status; fi
	@grep -q '0 divergences' build/cards-elixir-retain-fuzz.log
	@grep -qE '[1-9][0-9]* seeds over [1-9][0-9]* roots' build/cards-elixir-retain-fuzz.log
	@grep -qE '[1-9][0-9]* mutants' build/cards-elixir-retain-fuzz.log

test-elixir: cards-elixir-retain-fuzz
