# docs/USAGE.md's Python fragments (docs/PORTING.md I12): each
# `<!-- python-usage <unit> -->` fragment runs against its unit's generated
# package under both pinned interpreters, and the control plants a renamed
# documented method or runtime class in a copy of that package and requires
# the fragment to fail naming it.
PYTHON_CI_SUPPORT_GROUPS += tables-python-usage tables-python-usage-negative-control

.PHONY: tables-python-usage tables-python-usage-negative-control
tables-python-usage: python-toolchain-check
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonUsageFragmentsRun$$' ./internal/ci/

tables-python-usage-negative-control: python-toolchain-check
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonUsageFragmentsRunNegativeControl$$' ./internal/ci/

test-python: tables-python-usage tables-python-usage-negative-control
