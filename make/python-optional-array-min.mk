PYTHON_CI_SUPPORT_GROUPS += python-optional-array-min-negative-control
.PHONY: python-optional-array-min-negative-control

test-python: python-optional-array-min-negative-control

python-optional-array-min-negative-control:
	$(GO_TEST) -count=1 -timeout 600s -run '^TestPythonOptionalArray(PresentMinimum|ClosedElementKind)$$' ./internal/codegen/pythontable/
