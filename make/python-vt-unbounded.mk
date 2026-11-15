# make/python-vt-unbounded.mk — the Python table leg's unbounded arrays
# (docs/PORT-PYTHON.md). Included by the Makefile's wildcard include; the stem
# has a dash, so it is a support fragment and never probed as a --lang port.
#
# The unbounded array's negative controls: each read refusal of the array
# codec, the write's element validation and its length cap are removed one at
# a time from a COPY of the generated runtime, and the gate case that watches
# each must go red by name. It needs the pins, so test-python runs it beside
# the table tests, and make/negative-controls.d schedules it in the
# python-table-core group.
test-python: python-vt-unbounded-negative-control

.PHONY: python-vt-unbounded-negative-control
python-vt-unbounded-negative-control:
	SCHEMA_SLOW=1 go test -tags schema_leg_python -run 'TestPythonVtUnboundedNegativeControl' ./internal/codegen/pythontable/ -count=1
