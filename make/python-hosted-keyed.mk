.PHONY: python-hosted-keyed
# The real mixed unit's positional packet list and table-keyed wrappers.
test-python: python-hosted-keyed
python-hosted-keyed:
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -run '^TestPythonHostedPacketKeyed' ./internal/codegen/pythontable/
