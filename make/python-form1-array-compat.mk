# Prior-writer required and optional arrays remain valid after removal.
.PHONY: python-form1-array-compat-negative-control
test-python: python-form1-array-compat-negative-control
python-form1-array-compat-negative-control:
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -run '^TestPythonForm1ArrayPriorWriters$$' ./internal/codegen/pythontable/
