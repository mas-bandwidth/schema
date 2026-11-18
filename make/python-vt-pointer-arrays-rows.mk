# make/python-vt-pointer-arrays-rows.mk — the Python leg's variable-class and
# pointer-arrays roadmap rows over the shared conformance corpus
# (docs/port-python/table-pointer-arrays.md). Included by the Makefile's
# wildcard include.
#
# python-vt-pointer-arrays-rows holds every manifest row of the two rows (and
# every manifest row whose root reaches a pointer array) to the Go engine's
# verdict and canonical bytes under both pinned interpreters. Its negative
# control plants each fault in a COPY of the emitted runtime and requires the
# planted rows red by name.
.PHONY: python-vt-pointer-arrays-rows python-vt-pointer-arrays-rows-negative-control python-ci-vt-pointer-arrays-rows
PYTHON_CI_SUPPORT_GROUPS += python-ci-vt-pointer-arrays-rows
python-ci-vt-pointer-arrays-rows: python-vt-pointer-arrays-rows python-vt-pointer-arrays-rows-negative-control
test-python: python-vt-pointer-arrays-rows python-vt-pointer-arrays-rows-negative-control
python-vt-pointer-arrays-rows: python-toolchain-check
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonVtPointerArraysRows$$' ./internal/codegen/pythontable/
python-vt-pointer-arrays-rows-negative-control: python-toolchain-check
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonVtPointerArraysRowsNegativeControl$$' ./internal/codegen/pythontable/
