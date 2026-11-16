.PHONY: python-hosted-keyed python-hosted-keyed-negative-control
# The real mixed unit's positional packet list and table-keyed wrappers.
test-python: python-hosted-keyed python-hosted-keyed-negative-control
python-hosted-keyed:
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -run '^TestPythonHostedPacketKeyed($$|TableWire$$)' ./internal/codegen/pythontable/
python-hosted-keyed-negative-control:
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -run '^TestPythonHostedPacketKeyed(NegativeControl|TableWireNegativeControl)$$' ./internal/codegen/pythontable/
