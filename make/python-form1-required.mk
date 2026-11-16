# Required fields are present on form 1; the controls plant omissions and
# missing-required errors out of generated and runtime paths.
.PHONY: python-form1-required-negative-control
test-python: python-form1-required-negative-control
python-form1-required-negative-control:
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -run '^(TestPythonForm1Required(NegativeControls|FastControls|GuardControls)|TestPythonRequiredNestedDefaults)$$' ./internal/codegen/pythontable/
