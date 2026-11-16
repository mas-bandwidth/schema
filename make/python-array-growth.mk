.PHONY: python-array-growth-negative-control
python-array-growth-negative-control:
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonFixedArrayGrowthNegativeControl$$' ./internal/codegen/pythontable/
test-python: python-array-growth-negative-control
