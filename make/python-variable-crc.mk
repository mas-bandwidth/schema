.PHONY: python-variable-crc-negative-control
python-variable-crc-negative-control:
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonVariableIntegrityCRCNegativeControl$$' ./internal/codegen/pythontable/
test-python: python-variable-crc-negative-control
