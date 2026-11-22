PYTHON_CI_SUPPORT_GROUPS += python-fixed-crc-negative-control

.PHONY: python-fixed-crc-negative-control
python-fixed-crc-negative-control:
	SCHEMA_SLOW=1 $(GOTESTRUN) -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonFixedIntegrityCRCNegativeControl$$' ./internal/codegen/pythontable/
test-python: python-fixed-crc-negative-control
