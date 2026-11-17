# make/python-vt-evolution-rows.mk — the two evolution roadmap rows
# (defaults-and-table-rename, renames) on the Python table leg, held over every
# conformance manifest instance of the row (docs/PORT-PYTHON.md,
# docs/port-python/table-evolution.md). The control plants a fault in a copy of
# the emitted runtime and of the generated package and requires each row to go
# red by name; its row is make/negative-controls.d/python-vt-evolution-rows-negative-control.json.
.PHONY: python-vt-evolution-rows python-vt-evolution-rows-negative-control
python-vt-evolution-rows: dist-python
	SCHEMA_SLOW=1 go test -count=1 -timeout 120s -tags schema_leg_python -run '^TestPythonVtEvolutionRows$$' ./internal/codegen/pythontable/
python-vt-evolution-rows-negative-control: dist-python
	SCHEMA_SLOW=1 go test -count=1 -timeout 120s -tags schema_leg_python -run '^TestPythonVtEvolutionRowsNegativeControl$$' ./internal/codegen/pythontable/
test-python: python-vt-evolution-rows python-vt-evolution-rows-negative-control
