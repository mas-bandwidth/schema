# make/python-vt-unions.mk — table-side unions on the Python table leg
# (docs/PORT-PYTHON.md). Included by the Makefile's wildcard include; the stem
# has a dash, so it is a support fragment and never probed as a --lang port.
#
# python-vt-unions: TestPythonVtUnions holds a union in a field, an array
# element, a nested by-value table and a pointer target to the Go engine's
# verdict under both pinned interpreters: each case wire is read by
# internal/tablewire and by the probe, which must agree on the refusal's
# reason or on the bytes the value writes back; beside them the write
# refusals' paths and rules, a declared non-zero default, and elision.
#
# python-vt-unions-negative-control: TestPythonVtUnionsNegativeControl plants
# one fault per union check in a copy of the generated runtime (the canonical
# arm length, the arm-0 read, the unknown arm's and the unknown field's skip
# checks, damage outranking a mismatch, the arm kind, the payload-free arm's
# L, an arm's trailing bytes, the count bound's reason, the extent cap, the
# counted, fixed and by-value table elision, the write path, the write
# minimum and the write tag) and requires the probe, still running to its
# end, to lose the line that names that check.
.PHONY: python-vt-unions python-vt-unions-negative-control
test-python: python-vt-unions python-vt-unions-negative-control

python-vt-unions: python-toolchain-check
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run 'TestPythonVtUnions$$' ./internal/codegen/pythontable/

python-vt-unions-negative-control: python-toolchain-check
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run 'TestPythonVtUnionsNegativeControl' ./internal/codegen/pythontable/
