.PHONY: python-array-growth-negative-control
PYTHON_CI_SUPPORT_GROUPS += python-array-growth-negative-control
python-array-growth-negative-control:
	SCHEMA_SLOW=1 $(GOTESTRUN) -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonFixedArrayGrowthNegativeControl$$' ./internal/codegen/pythontable/
test-python: python-array-growth-negative-control
