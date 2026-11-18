# The fixed-class row on the Python leg (internal/codegen/pythontable,
# vtfixedclass_rows_test.go). python-vt-fixed-class-rows holds luatable's
# fixed-class manifest corpus, the tabledemo roots, the V1/V2 evolution seams,
# the P1 chain and the form-byte refusals, to the one read on read-then-write
# under both pinned interpreters.
#
# python-vt-fixed-class-rows-negative-control moves the fixed file's last
# header byte in a copy of the emitted runtime and requires every accepted
# instance row to go red by name. Both need the pins, so test-python depends on
# them, and the control is registered in
# make/negative-controls.d/python-vt-fixed-class-rows-negative-control.json.
.PHONY: python-vt-fixed-class-rows python-vt-fixed-class-rows-negative-control
python-vt-fixed-class-rows: deps-go python-toolchain-check
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonVtFixedClassRows$$' ./internal/codegen/pythontable/

python-vt-fixed-class-rows-negative-control: deps-go python-toolchain-check
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonVtFixedClassRowsNegativeControl$$' ./internal/codegen/pythontable/

test-python: python-vt-fixed-class-rows python-vt-fixed-class-rows-negative-control
PYTHON_CI_SUPPORT_GROUPS += python-vt-fixed-class-rows python-vt-fixed-class-rows-negative-control
