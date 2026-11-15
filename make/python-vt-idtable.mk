# The id-table wire's enum and escape kinds (internal/codegen/pythontable,
# idtable_test.go). python-vt-idtable holds Go-engine constructs that use the
# enum kind alone and with the base's kinds to the engine's verdict and bytes
# on read-then-write, under both pinned interpreters.
#
# python-vt-idtable-negative-control: in copies of the emitted runtime, one
# fault at a time, the enum codec references the wrong variant id, drops its
# read-side past-table check or validates nothing, the payload-free skip
# accepts a nonzero L, the unknown-enum skip drops its past-table check, and an
# unknown enum array is skipped without walking its elements; the interpreter
# probe must go red by name under each. Both need the pins, so test-python
# depends on them, and the control is registered in
# make/negative-controls.d/python-vt-idtable-negative-control.json.
.PHONY: python-vt-idtable python-vt-idtable-negative-control
python-vt-idtable:
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonVtIdtable$$' ./internal/codegen/pythontable/

python-vt-idtable-negative-control:
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonVtIdtableNegativeControl$$' ./internal/codegen/pythontable/

test-python: python-vt-idtable python-vt-idtable-negative-control
