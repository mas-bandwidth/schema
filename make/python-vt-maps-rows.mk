# The map row over the conformance manifest on the Python table leg
# (docs/port-python/table-maps.md).
#
# python-vt-maps-rows runs TestPythonVtMapsRows under both pinned interpreters
# (internal/codegen/pythontable/vtmaps_rows_test.go): every manifest instance
# and report row whose root reaches a map, and the two Go-engine map
# constructs, read and written back as the engine answers.
#
# python-vt-maps-rows-negative-control plants in a COPY of the emitted
# _schema/kind_map.py the reader's key-order test inverted, then the writer's
# entry count one too many, and the named rows must go red under each.
.PHONY: python-vt-maps-rows python-vt-maps-rows-negative-control
PYTHON_CI_SUPPORT_GROUPS += python-ci-vt-maps-rows
python-ci-vt-maps-rows: python-vt-maps-rows python-vt-maps-rows-negative-control
test-python: python-vt-maps-rows python-vt-maps-rows-negative-control

python-vt-maps-rows: python-toolchain-check
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -run '^TestPythonVtMapsRows$$' ./internal/codegen/pythontable/

python-vt-maps-rows-negative-control: python-toolchain-check
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -run '^TestPythonVtMapsRowsNegativeControl$$' ./internal/codegen/pythontable/
