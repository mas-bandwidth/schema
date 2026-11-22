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

# THE WRITE CONTRACT's controls on the Python leg (docs/SPEC.md 5.1): Python
# refuses in every build, so each removes one refusal from a COPY of the
# generated tree and requires its write-refusal surface red while the wire (or
# message) surface stays green.
.PHONY: conformance-negative-control-python-write-refusal conformance-negative-control-python-write-refusal-fixed conformance-negative-control-python-write-refusal-message

PYTHON_CI_SUPPORT_GROUPS += conformance-negative-control-python-write-refusal conformance-negative-control-python-write-refusal-fixed conformance-negative-control-python-write-refusal-message

conformance-negative-control-python-write-refusal: python-toolchain-check build/conformance-harness build/conformance-python
	PYTHON=$(PYTHON) sh test/conformance/python/negative-control-write-refusal write-refusal

conformance-negative-control-python-write-refusal-fixed: python-toolchain-check build/conformance-harness build/conformance-python
	PYTHON=$(PYTHON) sh test/conformance/python/negative-control-write-refusal write-refusal-fixed

conformance-negative-control-python-write-refusal-message: python-toolchain-check build/conformance-harness build/conformance-python
	PYTHON=$(PYTHON) sh test/conformance/python/negative-control-write-refusal write-refusal-message

test-python: conformance-negative-control-python-write-refusal conformance-negative-control-python-write-refusal-fixed conformance-negative-control-python-write-refusal-message
