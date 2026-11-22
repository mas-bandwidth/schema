PYTHON_CI_SUPPORT_GROUPS += python-variable-crc-negative-control
.PHONY: python-variable-crc-negative-control
python-variable-crc-negative-control:
	SCHEMA_SLOW=1 $(GOTESTRUN) -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonVariableIntegrityCRCNegativeControl$$' ./internal/codegen/pythontable/
test-python: python-variable-crc-negative-control
