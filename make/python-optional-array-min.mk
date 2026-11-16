.PHONY: python-optional-array-min-negative-control

test-python: python-optional-array-min-negative-control

python-optional-array-min-negative-control:
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -run '^TestPythonOptionalArray(PresentMinimum|ClosedElementKind)$$' ./internal/codegen/pythontable/
