# make/python-conformance.mk — the Python leg's conformance targets and emitter
# sabotage controls (docs/PORT-PYTHON.md; docs/PORTING.md I2, I11).

.PHONY: conformance-python conformance-negative-control-python conformance-negative-control-python-walk

PYTHON_CI_SUPPORT_GROUPS += conformance-python conformance-negative-control-python conformance-negative-control-python-walk

conformance-python: python-toolchain-check build/conformance-harness build/conformance-cpp build/schema_test_cook build/conformance-python
	PYTHON=$(PYTHON) ./build/conformance-harness run --only python --work build/conformance-python-current
	PYTHON=$(PYTHON_FLOOR) ./build/conformance-harness run --only python --work build/conformance-python-floor

conformance-negative-control-python: python-toolchain-check build/conformance-harness build/conformance-cpp build/schema_test_cook
	PYTHON=$(PYTHON) sh test/conformance/python/negative-control wire

conformance-negative-control-python-walk: python-toolchain-check build/conformance-harness build/conformance-cpp build/schema_test_cook
	PYTHON=$(PYTHON) sh test/conformance/python/negative-control walk

test-python: conformance-python conformance-negative-control-python conformance-negative-control-python-walk
