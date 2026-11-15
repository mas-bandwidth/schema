# make/python-vt-unions.mk — table-side unions on the Python table leg
# (docs/PORT-PYTHON.md). Included by the Makefile's wildcard include; the stem
# has a dash, so it is a support fragment and never probed as a --lang port.
#
# python-vt-unions: TestPythonVtUnions holds Go-engine constructs that carry a
# union in a field, an array element, a nested table and a pointer target to
# the one read on read-then-write under both pinned interpreters, every
# refusal's reason and the written bytes measured against the Go engine.
#
# python-vt-unions-negative-control: TestPythonVtUnionsNegativeControl plants
# one fault per union check in a copy of the generated runtime (the canonical
# arm length, the refusal reasons, the load of an arm, the trailing-byte check,
# the measure, the arm-0 read, the skipped arm's validation, the count bound
# and the per-element elision) and requires the probe to go red naming that
# check.
.PHONY: python-vt-unions python-vt-unions-negative-control
test-python: python-vt-unions python-vt-unions-negative-control

python-vt-unions: python-toolchain-check
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run 'TestPythonVtUnions$$' ./internal/codegen/pythontable/

python-vt-unions-negative-control: python-toolchain-check
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run 'TestPythonVtUnionsNegativeControl' ./internal/codegen/pythontable/
