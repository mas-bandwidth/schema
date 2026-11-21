# make/python-vt-unbounded-rows.mk — the Python table leg's unbounded-arrays
# roadmap row (port/python/vt/unbounded-arrays; docs/port-python/table-unbounded.md).
# Included by the Makefile's wildcard include; the stem has a dash, so it is a
# support fragment and never probed as a --lang port.
#
# TestPythonVtUnboundedRows holds every manifest instance of the row, and its
# Go-engine construct, to the one read on read-then-write under both pinned
# interpreters. Its negative control plants the save's count, the read's
# count and the read's trailing-bytes check in a COPY of the emitted array
# kind modules and requires the rows each fault reaches red by name;
# make/negative-controls.d schedules it in the python-table-core group.
PYTHON_CI_SUPPORT_GROUPS += python-ci-vt-unbounded-rows
.PHONY: python-ci-vt-unbounded-rows python-vt-unbounded-rows python-vt-unbounded-rows-negative-control
python-ci-vt-unbounded-rows: python-vt-unbounded-rows python-vt-unbounded-rows-negative-control
python-vt-unbounded-rows: python-toolchain-check
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -run '^TestPythonVtUnboundedRows$$' ./internal/codegen/pythontable/
python-vt-unbounded-rows-negative-control: python-toolchain-check
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -run '^TestPythonVtUnboundedRowsNegativeControl$$' ./internal/codegen/pythontable/
test-python: python-vt-unbounded-rows python-vt-unbounded-rows-negative-control
