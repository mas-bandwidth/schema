# make/schema-dart-nightly-fuzz.mk — NIGHTLY FUZZING for the Dart table
# readers (issue card schema-dart-nightly-fuzz).
#
# THE CORPUS is every table schema the Dart generate targets read: the
# directories named by `./bin/schema generate --lang dart` in make/dart.mk
# (tables/examples, pointers, blobs, lists, maps, messages, backend, stream,
# vocab, vocab9, scalars, block, blockhome, arms, and examples-wide). It is
# generated into build/nightly-fuzz/gen and the harness reads a registry built
# from those files, so the list and the corpus cannot drift apart.
#
# THE FUZZER is test/dart-tables/nightly_fuzz.dart: a seeded mutation loop over
# the generated readers. package:fuzz has no maintained coverage-guided
# equivalent for the Dart 3.13 VM, so this is the seeded loop the card allows;
# every finding names its seed and writes the minimised input, so it replays.
# Each table root is driven three ways: tolerant wire load, save-then-load
# (strict), and fromJson text parse.
#
# THE CLOCK IS AN ARGUMENT. DART_NIGHTLY_FUZZ_SECONDS defaults to 10 minutes
# (the card's budget per port); the replay target passes no clock at all and is
# the fast per-push gate.
#
# THE SEEDS are the Go fuzz corpus, internal/fuzz/testdata, and the committed
# conformance wire vectors under testdata/ and tables/ — copied into
# build/nightly-fuzz/seeds and given to every table before the random pass.

DART_NIGHTLY_FUZZ_SECONDS ?= 600
DART_NIGHTLY_FUZZ_SEED ?= 1
DART_NIGHTLY_FUZZ_BUILD ?= build/nightly-fuzz

# unit:directory, one entry per generate line in make/dart.mk
DART_NIGHTLY_FUZZ_UNITS := \
	examples:tables/examples \
	arms:tables/arms \
	backend:tables/backend \
	blobs:tables/blobs \
	block:tables/block \
	blockhome:tables/blockhome \
	lists:tables/lists \
	maps:tables/maps \
	messages:tables/messages \
	pointers:tables/pointers \
	scalars:tables/scalars \
	stream:tables/stream \
	vocab:tables/vocab \
	vocab9:tables/vocab9 \
	wide:examples-wide

.PHONY: schema-dart-nightly-fuzz-corpus
schema-dart-nightly-fuzz-corpus: bin/schema
	@rm -rf $(DART_NIGHTLY_FUZZ_BUILD)/gen $(DART_NIGHTLY_FUZZ_BUILD)/seeds
	@mkdir -p $(DART_NIGHTLY_FUZZ_BUILD)/gen $(DART_NIGHTLY_FUZZ_BUILD)/seeds
	@set -e; for spec in $(DART_NIGHTLY_FUZZ_UNITS); do \
		unit=$${spec%%:*}; dir=$${spec##*:}; \
		./bin/schema generate --lang dart --out $(DART_NIGHTLY_FUZZ_BUILD)/gen/$$unit $$dir >/dev/null; \
	done
	@i=0; for f in $$(find testdata tables internal/fuzz/testdata -type f \
			\( -name '*.bin' -o -name '*.wire' -o -path '*/testdata/fuzz/*/*' \) 2>/dev/null); do \
		i=$$((i+1)); cp "$$f" $(DART_NIGHTLY_FUZZ_BUILD)/seeds/seed-$$i; \
	done
	@echo "nightly fuzz corpus: $(words $(DART_NIGHTLY_FUZZ_UNITS)) units, $$(ls $(DART_NIGHTLY_FUZZ_BUILD)/seeds | wc -l) seeds"

$(DART_NIGHTLY_FUZZ_BUILD)/registry.dart: schema-dart-nightly-fuzz-corpus test/dart-tables/nightly_fuzz_registry.sh
	@sh test/dart-tables/nightly_fuzz_registry.sh \
		$(DART_NIGHTLY_FUZZ_BUILD)/gen $@ $(CURDIR)

.PHONY: schema-dart-nightly-fuzz
schema-dart-nightly-fuzz: toolchain-dart $(DART_NIGHTLY_FUZZ_BUILD)/registry.dart
	@cp test/dart-tables/nightly_fuzz.dart $(DART_NIGHTLY_FUZZ_BUILD)/harness.dart
	@$(DART) $(DART_NIGHTLY_FUZZ_BUILD)/harness.dart \
		--seconds $(DART_NIGHTLY_FUZZ_SECONDS) \
		--seed $(DART_NIGHTLY_FUZZ_SEED) \
		--seeds $(DART_NIGHTLY_FUZZ_BUILD)/seeds

# THE FAST REPLAY: every committed wire vector, replayed through the readers it
# names in testdata/wire/tables/fuzz-vectors/INDEX.txt, and every saved nightly
# regression under testdata/wire/tables/nightly-fuzz-regressions. No clock and
# no mutation: this is what runs on a push.
.PHONY: schema-dart-nightly-fuzz-replay
schema-dart-nightly-fuzz-replay: toolchain-dart $(DART_NIGHTLY_FUZZ_BUILD)/registry.dart
	@cp test/dart-tables/nightly_fuzz.dart $(DART_NIGHTLY_FUZZ_BUILD)/harness.dart
	@set -- $$(awk '!/^#/ && NF >= 4 { print $$3 "=" $$4 }' \
		testdata/wire/tables/fuzz-vectors/INDEX.txt); \
	saved=$$(find testdata/wire/tables/nightly-fuzz-regressions -type f 2>/dev/null | sort); \
	if [ $$# -eq 0 ] && [ -z "$$saved" ]; then \
		echo "nightly fuzz replay: no vectors and no saved regressions — a gate over nothing"; exit 1; \
	fi; \
	$(DART) $(DART_NIGHTLY_FUZZ_BUILD)/harness.dart --replay "$$@" $$saved

# THE PLANTED BAD PATTERN: one bounds test is removed from a SCRATCH COPY of
# the generated reader (readLeb no longer checks p against limit), the fuzzer
# is pointed at the scratch registry, and it must go red inside one nightly
# budget. The minimised input is then replayed against the same scratch reader
# and must fail, so the saved input is the replay proof and not a log line.
# Nothing tracked is written to.
.PHONY: schema-dart-nightly-fuzz-negative-control
schema-dart-nightly-fuzz-negative-control: toolchain-dart schema-dart-nightly-fuzz-corpus
	@rm -rf $(DART_NIGHTLY_FUZZ_BUILD)-nc
	@mkdir -p $(DART_NIGHTLY_FUZZ_BUILD)-nc
	@cp -r $(DART_NIGHTLY_FUZZ_BUILD)/gen $(DART_NIGHTLY_FUZZ_BUILD)-nc/gen
	@f=$$(grep -rl 'if (count < 0 || count > (data.length - 9) ~/ 8) return 2;' $(DART_NIGHTLY_FUZZ_BUILD)-nc/gen | head -1); \
	if [ -z "$$f" ]; then echo "NEGATIVE CONTROL FAILED: the negative-count bound matched nothing — the emitter moved"; exit 1; fi; \
	sed -i 's@if (count < 0 || count > (data.length - 9) ~/ 8) return 2;@if (count > (data.length - 9) ~/ 8) return 2; // SABOTAGED@' "$$f"; \
	grep -q SABOTAGED "$$f" || { echo "NEGATIVE CONTROL FAILED: the sabotage matched nothing"; exit 1; }
	@sh test/dart-tables/nightly_fuzz_registry.sh \
		$(DART_NIGHTLY_FUZZ_BUILD)-nc/gen $(DART_NIGHTLY_FUZZ_BUILD)-nc/registry.dart $(CURDIR)
	@cp test/dart-tables/nightly_fuzz.dart $(DART_NIGHTLY_FUZZ_BUILD)-nc/harness.dart
	@if $(DART) $(DART_NIGHTLY_FUZZ_BUILD)-nc/harness.dart \
			--seconds 30 --seed 1 --seeds $(DART_NIGHTLY_FUZZ_BUILD)/seeds \
			> $(DART_NIGHTLY_FUZZ_BUILD)-nc/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the fuzzer stayed green with the readLeb bound removed"; \
		cat $(DART_NIGHTLY_FUZZ_BUILD)-nc/log; exit 1; \
	fi
	@grep -q '^FINDING ' $(DART_NIGHTLY_FUZZ_BUILD)-nc/log || \
		{ echo "NEGATIVE CONTROL FAILED: the fuzzer went red for another reason"; \
		  cat $(DART_NIGHTLY_FUZZ_BUILD)-nc/log; exit 1; }
	@grep -m1 '^FINDING ' $(DART_NIGHTLY_FUZZ_BUILD)-nc/log
	@saved=$$(grep -m1 '^SAVED ' $(DART_NIGHTLY_FUZZ_BUILD)-nc/log | sed 's/^SAVED //; s/ (.*//'); \
	if [ ! -f "$$saved" ]; then echo "NEGATIVE CONTROL FAILED: the finding saved no input"; exit 1; fi; \
	if $(DART) $(DART_NIGHTLY_FUZZ_BUILD)-nc/harness.dart --replay \
			$$(grep -m1 '^FINDING ' $(DART_NIGHTLY_FUZZ_BUILD)-nc/log | sed -E 's/^FINDING [^ ]+ table=([^ ]+) .*/\1=/')"$$saved" \
			> $(DART_NIGHTLY_FUZZ_BUILD)-nc/replay.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the saved input replayed clean against the planted reader"; \
		cat $(DART_NIGHTLY_FUZZ_BUILD)-nc/replay.log; exit 1; \
	fi
	@grep -q '^REPLAY FAILED ' $(DART_NIGHTLY_FUZZ_BUILD)-nc/replay.log || \
		{ echo "NEGATIVE CONTROL FAILED: the replay failed for another reason"; \
		  cat $(DART_NIGHTLY_FUZZ_BUILD)-nc/replay.log; exit 1; }
	@grep -m1 '^REPLAY FAILED ' $(DART_NIGHTLY_FUZZ_BUILD)-nc/replay.log
	@echo "negative control: a dropped readLeb bound turns the nightly fuzzer RED and the saved input fails replay"
