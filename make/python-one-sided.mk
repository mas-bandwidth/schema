PYTHON_CI_SUPPORT_GROUPS += python-one-sided-negative-control

.PHONY: python-one-sided-negative-control
python-one-sided-negative-control:
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -run '^TestPythonOneSidedRangeNegativeControl$$' ./internal/codegen/pythontable/
test-python: python-one-sided-negative-control
