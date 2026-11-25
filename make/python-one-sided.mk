PYTHON_CI_SUPPORT_GROUPS += python-one-sided-negative-control

.PHONY: python-one-sided-negative-control
python-one-sided-negative-control:
	$(GO_TEST) -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonOneSidedRangeNegativeControl$$' ./internal/codegen/pythontable/
test-python: python-one-sided-negative-control
