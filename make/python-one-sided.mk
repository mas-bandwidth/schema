.PHONY: python-one-sided-negative-control
python-one-sided-negative-control:
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonOneSidedRangeNegativeControl$$' ./internal/codegen/pythontable/
test-python: python-one-sided-negative-control
