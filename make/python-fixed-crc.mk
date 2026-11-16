.PHONY: python-fixed-crc-negative-control
python-fixed-crc-negative-control:
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonFixedIntegrityCRCNegativeControl$$' ./internal/codegen/pythontable/
test-python: python-fixed-crc-negative-control
