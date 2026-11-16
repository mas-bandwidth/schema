# Negative control for array element widening.
.PHONY: python-array-widen-negative-control
python-array-widen-negative-control:
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonArrayWideningNegativeControl$$' ./internal/codegen/pythontable/
test-python: python-array-widen-negative-control
PYTHON_CI_SUPPORT_GROUPS += python-array-widen-negative-control
