PYTHON_CI_SUPPORT_GROUPS += python-array-root-negative-control

.PHONY: python-array-root-negative-control
python-array-root-negative-control:
	SCHEMA_SLOW=1 $(GOTESTRUN) -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonArrayRootNegativeControl$$' ./internal/codegen/pythontable/
test-python: python-array-root-negative-control
