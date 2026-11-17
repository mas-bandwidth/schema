# make/python-vt-wide-rows.mk — the wide-scalars row on the Python table leg
# (ROADMAP.md, Variable tables: wide-scalars; docs/PORT-PYTHON.md). Included by
# the Makefile's wildcard include; the stem has a dash, so it is a support
# fragment and never probed as a --lang port.
#
# TestPythonVtWideRows holds every manifest instance whose unit carries a wide
# scalar (fixed(I, F), ufixed(I, F), int128, uint128) to the one read on
# read-then-write under both pinned interpreters: a fixed-declared root as form
# 3, a variable root as form 1. The negative control plants the uint128 width
# and the fixed form's 128-bit read width in a COPY of the emitted runtime and
# requires its rows red by name; it is registered in
# make/negative-controls.d/python-vt-wide-rows-negative-control.json.
PYTHON_CI_SUPPORT_GROUPS += python-vt-wide-rows python-vt-wide-rows-negative-control

.PHONY: python-vt-wide-rows python-vt-wide-rows-negative-control
python-vt-wide-rows: python-toolchain-check
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonVtWideRows$$' ./internal/codegen/pythontable/

python-vt-wide-rows-negative-control: python-toolchain-check
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonVtWideRowsNegativeControl$$' ./internal/codegen/pythontable/

test-python: python-vt-wide-rows python-vt-wide-rows-negative-control
