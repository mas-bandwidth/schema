# make/python-vt-keyed.mk — the enum-keyed array [E]T on the Python table leg
# (docs/PORT-PYTHON.md). Included by the Makefile's wildcard include; the stem
# has a dash, so it is a support fragment and never probed as a --lang port.
#
# The enum-keyed array's control: a copy of the runtime writes the keyed slots
# in descending variant order, and the construct gate must go red on the
# construct whose every slot is present. Its control row is
# make/negative-controls.d/python-vt-keyed-negative-control.json.
.PHONY: python-vt-keyed-negative-control
test-python: python-vt-keyed-negative-control

python-vt-keyed-negative-control:
	SCHEMA_SLOW=1 go test -tags schema_leg_python -run 'TestPythonVtKeyedNegativeControl' ./internal/codegen/pythontable/ -count=1
