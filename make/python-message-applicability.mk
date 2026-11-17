# Pin the manifest's no-wire message row to actual fixed-only applicability.
PYTHON_CI_SUPPORT_GROUPS += python-ci-message-applicability
.PHONY: python-ci-message-applicability python-message-applicability python-message-applicability-negative-control
python-ci-message-applicability: python-message-applicability python-message-applicability-negative-control
python-message-applicability: python-toolchain-check build/conformance-python
	$(PYTHON) -I -B test/conformance/python/message-applicability-probe.py
	$(PYTHON_FLOOR) -I -B test/conformance/python/message-applicability-probe.py
python-message-applicability-negative-control: python-toolchain-check build/conformance-python
	$(PYTHON) -I -B test/conformance/python/message-applicability-probe.py --negative
	$(PYTHON_FLOOR) -I -B test/conformance/python/message-applicability-probe.py --negative
test-python: python-message-applicability python-message-applicability-negative-control
