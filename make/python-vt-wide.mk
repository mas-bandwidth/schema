# make/python-vt-wide.mk — the wide table kinds (int128 and fixed(I, F)) on the
# Python table leg (docs/PORT-PYTHON.md). Included by the Makefile's wildcard
# include; the stem has a dash, so it is a support fragment and never probed as
# a --lang port.
#
# The wide kinds' negative control: a copy of the emitted runtime moves the
# integer codec's int128 width (every wide construct must go red under both
# pins) and takes a nested table's id reference after its body (the nested
# construct must go red). It needs the pins, so test-python depends on it; its
# control row is make/negative-controls.d/python-table-wide-negative-control.json.
.PHONY: python-table-wide-negative-control
test-python: python-table-wide-negative-control

python-table-wide-negative-control:
	SCHEMA_SLOW=1 go test -tags schema_leg_python -run 'TestPythonVtWideNegativeControl' ./internal/codegen/pythontable/ -count=1
