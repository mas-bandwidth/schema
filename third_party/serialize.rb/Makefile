# serialize.rb: the tests and the analyzer. RUBY_FLOOR (the oldest CRuby
# series in normal maintenance), RUBY (the newest stable CRuby) and RUBOCOP
# (RuboCop, run under the floor) are the pins make/ruby-toolchain.mk builds
# into dist/ (make dist-ruby) whenever dist/ holds them; the environment or
# the command line overrides. The library and its tests use the standard
# library only, so any CRuby 3.4 or newer runs them; with no dist/ and nothing
# set, both interpreters are ruby from PATH and RuboCop is the floor's own gem.
#
#   make dist-ruby && make test
#   make test RUBY_FLOOR=/path/to/ruby-3.4/bin/ruby RUBY=/path/to/ruby-4.0/bin/ruby

include make/ruby-toolchain.mk
include make/ci.mk
include make/release.mk
.DEFAULT_GOAL := test

RUBY_FLOOR ?= ruby
RUBY ?= ruby
RUBOCOP ?= $(RUBY_FLOOR) -e 'load Gem.bin_path("rubocop", "rubocop")' --

# the interpreter sees no RUBYOPT or RUBYLIB of the caller's: the library and
# the tests are found only where test/run.rb puts them
ISOLATED = env -u RUBYOPT -u RUBYLIB

.PHONY: test test-floor test-current lint negative-control conformance
test: test-floor test-current lint negative-control

need = @if [ -z "$($(1))" ]; then echo "make $@ REFUSES: $(1) is unset; name it: make $@ $(1)=/path/to/it"; exit 1; fi

# Every test under one interpreter, warnings on (-w): test/run.rb refuses a
# run that selects no test, and one in which Ruby printed a warning.
test-floor:
	$(call need,RUBY_FLOOR)
	$(ISOLATED) $(RUBY_FLOOR) -w test/run.rb

test-current:
	$(call need,RUBY)
	$(ISOLATED) $(RUBY) -w test/run.rb

# The family corpus at the root SERIALIZE_CORPUS_DIR names, under the floor.
conformance:
	$(call need,RUBY_FLOOR)
	SERIALIZE_CORPUS_DIR="$(SERIALIZE_CORPUS_DIR)" $(ISOLATED) $(RUBY_FLOOR) -w test/run.rb ConformanceTest#test_corpus

# RuboCop over the library and the tests, as .rubocop.yml configures it.
lint:
	$(call need,RUBOCOP)
	$(ISOLATED) $(RUBOCOP) --format simple --cache false lib test

# The corpus runner over a root with no vectors must go red by that name: a
# runner that ran nothing is never green.
negative-control:
	$(call need,RUBY_FLOOR)
	@rm -rf build/negative-control && mkdir -p build/negative-control/empty
	@if SERIALIZE_CORPUS_DIR=build/negative-control/empty $(ISOLATED) $(RUBY_FLOOR) -w test/run.rb ConformanceTest#test_corpus > build/negative-control/log 2>&1; then \
		cat build/negative-control/log; echo "NEGATIVE CONTROL FAILED: the corpus test passed over a root with no vectors"; exit 1; fi
	@grep -q "is missing or empty" build/negative-control/log || \
		{ cat build/negative-control/log; echo "NEGATIVE CONTROL FAILED: the empty corpus root went red, but not by name"; exit 1; }
	@echo "negative control: a corpus root with no vectors turns the corpus test red, by name"

include make/docs.mk make/roadmap.mk
