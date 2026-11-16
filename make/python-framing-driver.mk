# The full conformance matrix runs the Python framing adapter; this probe
# pins its accepted, no-value and refusal paths plus a planted refusal fault.
PYTHON_CI_SUPPORT_GROUPS += python-ci-framing-driver
.PHONY: python-ci-framing-driver python-framing-driver python-framing-driver-negative-control
python-ci-framing-driver: python-framing-driver python-framing-driver-negative-control
python-framing-driver: python-toolchain-check build/conformance-python
	$(PYTHON) -I -B test/conformance/python/framing-probe.py
	$(PYTHON_FLOOR) -I -B test/conformance/python/framing-probe.py
python-framing-driver-negative-control: python-toolchain-check build/conformance-python
	$(PYTHON) -I -B test/conformance/python/framing-probe.py --negative
	$(PYTHON_FLOOR) -I -B test/conformance/python/framing-probe.py --negative
test-python: python-framing-driver python-framing-driver-negative-control
