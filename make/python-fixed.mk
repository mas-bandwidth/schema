# make/python-fixed.mk — the Python leg's fixed form (docs/PORT-PYTHON.md,
# THE LAYERS: P5; internal/codegen/python/fixed.go, fixedline.go and
# runtime/_schema/fixed.py). Included by the Makefile's wildcard include; the
# stem has a dash, so it is a support fragment and never probed as a --lang
# port.
#
# The judge is the C++ reference's fixed-form corpus (build/fixedform-corpus,
# make tables-fixedform-corpus): every reference file read under both pinned
# interpreters with its manifest's values landing and the save written back
# byte for byte, every versioning row's two columns, and every refusal by its
# name. SCHEMA_REQUIRE_CORPUS=1 makes an absent corpus a failure, never a skip.
# make test-python runs the whole package; the targets below run the gate
# and its controls on their own.

.PHONY: tables-python-fixed tables-python-fixed-negative-controls bench-python-fixed-shape
tables-python-fixed: python-toolchain-check build/fixedform-corpus/.stamp
	SCHEMA_REQUIRE_CORPUS=1 go test ./internal/codegen/python -count=1 -run 'TestPythonFixed|TestPythonKeyedArray|TestPythonRefusesVariableTables'

# THE CONTROLS: the byte oracle (an integer store skipped), the versioning
# match (the id match broken), the ordinal and bool tests (each removed from
# the emitted identity reader and from the runtime's plan in turn) and the
# keyed array's refusal (removed): each must turn its gate red.
tables-python-fixed-negative-controls: python-toolchain-check build/fixedform-corpus/.stamp
	SCHEMA_REQUIRE_CORPUS=1 go test ./internal/codegen/python -count=1 -run 'NegativeControl' -v 2>&1 | \
		tee build/python-fixed-controls.log | grep -E '^(--- |ok|FAIL)'
	@if grep -q '^--- FAIL' build/python-fixed-controls.log; then echo "NEGATIVE CONTROL FAILED: see build/python-fixed-controls.log"; exit 1; fi

test-python: build/fixedform-corpus/.stamp

# D1 BY MEASUREMENT: the plan against the straight-line identity reader, on
# the reference's own files. It uses the clock, so it is never part of make
# test. The two units are copied out of the tree first: tables/examples
# carries a lock, and a subset of it would be held to the whole lock.
bench-python-fixed-shape: python-toolchain-check build/schema-python build/fixedform-corpus/.stamp
	@rm -rf build/bench-python-fixed && mkdir -p build/bench-python-fixed/src
	@cp tables/examples/Pack.schema tables/examples/Keyed.schema build/bench-python-fixed/src/
	./$(PYTHON_SCHEMA) generate --lang python --out build/bench-python-fixed/generated test/tables/FX1.schema
	./$(PYTHON_SCHEMA) generate --lang python --out build/bench-python-fixed/generated build/bench-python-fixed/src
	$(PYTHON) -I bench/python/fixed_shape.py build/bench-python-fixed/generated build/fixedform-corpus 2000
	$(PYTHON_FLOOR) -I bench/python/fixed_shape.py build/bench-python-fixed/generated build/fixedform-corpus 2000
