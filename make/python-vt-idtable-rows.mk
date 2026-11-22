# The id-table-wire roadmap row over the conformance manifest
# (internal/codegen/pythontable, vtidtable_rows_test.go). python-vt-idtable-rows
# holds every manifest case luatable's vtcells_test.go names for the row to the
# one read on read-then-write under both pinned interpreters: each instance
# saves back byte for byte, and each report row answers the Go engine's
# verdict, the plain load by Decode's reason and the variable reader by
# DecodeVariable's.
#
# python-vt-idtable-rows-negative-control: in copies of the emitted runtime,
# one fault at a time, the fixed save's layout hash is one bit off, the fixed
# read's record-hash test is inverted, and the variable read refuses an escape
# it cannot name; the cases each covers must go red. Both need the pins, so
# test-python depends on them, and the control is registered in
# make/negative-controls.d/python-vt-idtable-rows-negative-control.json.
.PHONY: python-vt-idtable-rows python-vt-idtable-rows-negative-control
python-vt-idtable-rows: deps-go python-toolchain-check
	SCHEMA_SLOW=1 $(GOTESTRUN) -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonVtIdtableRows$$' ./internal/codegen/pythontable/

python-vt-idtable-rows-negative-control: deps-go python-toolchain-check
	SCHEMA_SLOW=1 $(GOTESTRUN) -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonVtIdtableRowsNegativeControl$$' ./internal/codegen/pythontable/

test-python: python-vt-idtable-rows python-vt-idtable-rows-negative-control
PYTHON_CI_SUPPORT_GROUPS += python-vt-idtable-rows python-vt-idtable-rows-negative-control
