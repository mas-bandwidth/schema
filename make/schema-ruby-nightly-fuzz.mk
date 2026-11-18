# The full seeded Ruby verdict pipe runs in the nightly lane.
.PHONY: schema-ruby-nightly-fuzz
schema-ruby-nightly-fuzz:
	$(MAKE) tables-ruby-wire-fuzz N=500000 RUBY_WIRE_FUZZ_DIR=build/ruby-wirefuzz-nightly/default
	$(MAKE) tables-ruby-wire-fuzz SEED=2 N=500000 RUBY_WIRE_FUZZ_DIR=build/ruby-wirefuzz-nightly/seed-2
