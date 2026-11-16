.PHONY: python-array-root-negative-control
python-array-root-negative-control:
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonArrayRootNegativeControl$$' ./internal/codegen/pythontable/
test-python: python-array-root-negative-control
