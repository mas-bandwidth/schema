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

# The framing roster exercises Cfg's form-1 wire contract. Its source also
# declares the fixed class, whose public load deliberately selects form 3.
# Generate the same declaration as a variable class for this conformance leg.
build/conformance-python-framing: build/schema-python test/tables/R1.schema
	@test "$$(grep -c '^fixed table Cfg$$' test/tables/R1.schema)" = 1
	@mkdir -p $@/source $@/tblr1
	@sed 's/^fixed table Cfg$$/table Cfg/' test/tables/R1.schema > $@/source/R1.schema
	./build/schema-python generate --lang python --out $@/tblr1 $@/source/R1.schema
build/conformance-python: build/conformance-python-framing
