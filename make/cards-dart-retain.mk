# make/cards-dart-retain.mk — the Dart card's RETAINED fuzz arm (docs/SPEC-TABLES.md
# §6.6). This fragment is the card's own; it rides beside make/dart.mk's
# cards-dart-wire-fuzz and is kept apart so the facade card (cards-dart-wire-fuzz)
# and the retain card extend the leg's gate in parallel without editing make/dart.mk.
# A card fragment is not a port: it holds the card's extra --retain arm on the
# port's own cards-dart-wire-fuzz.

# cards-dart-retain-fuzz is the SAME fuzzer through the RETENTION ARM
# (docs/SPEC-TABLES.md §6.6): every mutant is read and written back through the
# Card codec's retaining pair, and the two retention counters ride beside the
# six. The seed, count and coverage greps are cards-dart-wire-fuzz's, plus
# --retain, so a retained save is held to the Go reference leg byte for byte.
.PHONY: cards-dart-retain-fuzz
cards-dart-retain-fuzz: cards-dart-generate build/conformance-harness build/conformance-dart
	@timeout $(CARDS_DART_FUZZ_TIME) ./build/conformance-harness wire-fuzz \
		--manifest tables/cards/fuzz/MANIFEST.txt --json tables/cards/fuzz --vectors '' \
		--driver 'build/conformance-dart wire-fuzz' --seed 24845619678 --n 100 --retain \
		> build/cards-dart-retain-fuzz.log 2>&1; \
		status=$$?; cat build/cards-dart-retain-fuzz.log; \
		if [ $$status -ne 0 ]; then exit $$status; fi
	@grep -q '0 divergences' build/cards-dart-retain-fuzz.log
	@grep -qE '[1-9][0-9]* seeds over [1-9][0-9]* roots' build/cards-dart-retain-fuzz.log
	@grep -qE '[1-9][0-9]* mutants' build/cards-dart-retain-fuzz.log

# THE CARD WIRE-FUZZ'S RETAIN ARM RIDES THE DART LEG'S GATE (§6.6): the same
# oracle harness, the same seed, over the Card root only, so the Dart Card
# codec's retained save is held to the identical byte-for-byte contract the Go
# leg's cards-retain-fuzz pins.
test-dart: cards-dart-retain-fuzz
