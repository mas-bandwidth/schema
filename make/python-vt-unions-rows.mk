# make/python-vt-unions-rows.mk — the union roadmap rows on the Python table
# leg (port/python/vt/union-table-arms, union-arrays, union-arms-any;
# docs/port-python/table-unions.md). Included by the Makefile's wildcard
# include; the stem has a dash, so it is a support fragment and never probed as
# a --lang port.
#
# python-vt-unions-rows: TestPythonVtUnionsRows holds every conformance-manifest
# instance and report row of the three rows, and the union construct beside
# them, to the Go engine's read-then-write under both pinned interpreters, at
# the root's declared form (form 3 at a fixed root).
#
# python-vt-unions-rows-negative-control: TestPythonVtUnionsRowsNegativeControl
# plants union faults in a copy of the generated runtime (the variable arm's L,
# the in-place length-kind arm header, the arm's trailing-bytes check, the
# fixed arm read, and a fixed refusal that writes into the caller's report) and
# requires each named row to go red; TestPythonVtUnionsRowsControlsCoverEveryCase
# requires every case to be named by some control.
.PHONY: python-vt-unions-rows python-vt-unions-rows-negative-control
test-python: python-vt-unions-rows python-vt-unions-rows-negative-control
PYTHON_CI_SUPPORT_GROUPS += python-vt-unions-rows python-vt-unions-rows-negative-control

python-vt-unions-rows: python-toolchain-check
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -run 'TestPythonVtUnionsRows$$' ./internal/codegen/pythontable/

python-vt-unions-rows-negative-control: python-toolchain-check
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -run 'TestPythonVtUnionsRows(NegativeControl|ControlsCoverEveryCase)$$' ./internal/codegen/pythontable/
