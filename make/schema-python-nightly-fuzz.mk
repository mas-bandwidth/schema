# The full seeded Python verdict pipe runs in the nightly lane.
.PHONY: schema-python-nightly-fuzz
schema-python-nightly-fuzz:
	$(MAKE) tables-python-wire-fuzz N=500000 PYTHON_WIRE_FUZZ_DIR=build/python-wirefuzz-nightly/default
	$(MAKE) tables-python-wire-fuzz SEED=2 N=500000 PYTHON_WIRE_FUZZ_DIR=build/python-wirefuzz-nightly/seed-2
