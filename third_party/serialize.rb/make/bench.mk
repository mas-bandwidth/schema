# The hot operations (bits, ints, floats, bytes) written and read under RUBY,
# with YJIT off and then on. A measurement, not a test: it reads the clock,
# so it is never part of make test. `make bench` needs a RUBY built with YJIT.

.PHONY: bench bench-choices
bench:
	$(call need,RUBY)
	$(ISOLATED) $(RUBY) -w test/bench.rb
	$(ISOLATED) $(RUBY) -w --yjit test/bench.rb

# The representation choices DESIGN.md gives speed as the reason for, each
# candidate reduced to its inner loop, YJIT off and then on.
bench-choices:
	$(call need,RUBY)
	$(ISOLATED) $(RUBY) -w test/bench_choices.rb
	$(ISOLATED) $(RUBY) -w --yjit test/bench_choices.rb
