PYTHON_CI_SUPPORT_GROUPS += python-array-root-negative-control

.PHONY: python-array-root-negative-control
python-array-root-negative-control:
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -run '^TestPythonArrayRootNegativeControl$$' ./internal/codegen/pythontable/
test-python: python-array-root-negative-control
