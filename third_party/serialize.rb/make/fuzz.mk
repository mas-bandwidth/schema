# The fuzz harness (fuzz/fuzz.rb), under the floor interpreter.
#
# fuzz-replay is the deterministic gate: every seed saved under fuzz/corpus
# runs its fixed set of cases, in seconds, with no clock. fuzz is the live run,
# nightly only: FUZZ_SEED and FUZZ_TIMEOUT (seconds) are arguments, and the
# harness prints the seed first and a reproduce line for every failing case.
# The nightly workflow draws a fresh seed and a long budget:
#   make fuzz FUZZ_SEED=<n> FUZZ_TIMEOUT=1800 RUBY_FLOOR=/path/to/ruby
# fuzz-replay-negative-control replays over a copy of lib/ with one planted
# defect, a read that accepts a set padding bit, and requires the replay red
# by that refusal's name.

FUZZ_SEED ?= 1
FUZZ_TIMEOUT ?= 60
FUZZ_RUBY ?= $(RUBY_FLOOR)

# the reader's padding check, and the planted defect that never refuses it
FUZZ_PLANT_SITE := return refuse(NONZERO_PADDING) if pad != 0 && take(pad) != 0
FUZZ_PLANT := return refuse(NONZERO_PADDING) if pad != 0 && take(pad) != 0 && pad.negative?

.PHONY: fuzz fuzz-replay fuzz-replay-negative-control

fuzz:
	$(call need,FUZZ_RUBY)
	$(ISOLATED) $(FUZZ_RUBY) -w fuzz/fuzz.rb --seed $(FUZZ_SEED) --timeout $(FUZZ_TIMEOUT)

fuzz-replay:
	$(call need,FUZZ_RUBY)
	$(ISOLATED) $(FUZZ_RUBY) -w fuzz/fuzz.rb --replay fuzz/corpus

fuzz-replay-negative-control:
	$(call need,FUZZ_RUBY)
	@rm -rf build/fuzz-negative-control && mkdir -p build/fuzz-negative-control
	@cp -R lib build/fuzz-negative-control/lib
	@SITE='$(FUZZ_PLANT_SITE)' PLANT='$(FUZZ_PLANT)' $(ISOLATED) $(FUZZ_RUBY) -e \
		'p = ARGV[0]; s = File.read(p); n = s.scan(ENV.fetch("SITE")).size; abort("plant site found #{n} times, want 1") unless n == 1; File.write(p, s.sub(ENV.fetch("SITE"), ENV.fetch("PLANT")))' \
		build/fuzz-negative-control/lib/serialize/core.rb
	@if $(ISOLATED) $(FUZZ_RUBY) -w fuzz/fuzz.rb --replay fuzz/corpus --lib build/fuzz-negative-control/lib > build/fuzz-negative-control/log 2>&1; then \
		cat build/fuzz-negative-control/log; echo "NEGATIVE CONTROL FAILED: the replay stayed green over a reader that accepts a set padding bit"; exit 1; fi
	@grep -q "expected refusal nonzero_padding, got accept" build/fuzz-negative-control/log || \
		{ cat build/fuzz-negative-control/log; echo "NEGATIVE CONTROL FAILED: the replay went red, but not by nonzero_padding"; exit 1; }
	@grep -m1 "expected refusal nonzero_padding, got accept" build/fuzz-negative-control/log
	@echo "fuzz negative control: a reader that accepts a set padding bit turns the replay red, by name"
