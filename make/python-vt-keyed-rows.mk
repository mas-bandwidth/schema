# make/python-vt-keyed-rows.mk — the enum-keyed array's roadmap row on the
# Python table leg (port/python/vt/enum-keyed-arrays; docs/PORT-PYTHON.md).
# Included by the Makefile's wildcard include; the stem has a dash, so it is a
# support fragment and never probed as a --lang port.
#
# TestPythonVtKeyedRows holds every manifest instance and report row whose root
# reaches a keyed array, their form-1 copies and luatable's construct to the
# one read on read-then-write under both pinned interpreters. Its control plants
# a keyed fault in a copy of the runtime (the variable writer's slot order, the
# fixed reader's slot order) and requires the rows each reaches to go red by
# name; the control row is
# make/negative-controls.d/python-vt-keyed-rows-negative-control.json. Both
# targets are registered as CI support rows of the per-commit Python plan, as
# every test-python prerequisite must be (internal/ci
# TestPythonCISupportCoversEveryPrerequisite).
.PHONY: python-vt-keyed-rows python-vt-keyed-rows-negative-control
PYTHON_CI_SUPPORT_GROUPS += python-vt-keyed-rows python-vt-keyed-rows-negative-control
test-python: python-vt-keyed-rows python-vt-keyed-rows-negative-control

python-vt-keyed-rows: python-toolchain-check
	SCHEMA_SLOW=1 $(GOTESTRUN) -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonVtKeyedRows$$' ./internal/codegen/pythontable/

python-vt-keyed-rows-negative-control: python-toolchain-check
	SCHEMA_SLOW=1 $(GOTESTRUN) -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonVtKeyedRowsNegativeControl$$' ./internal/codegen/pythontable/
