# The message-form golden and its generated-runtime negative control.
.PHONY: python-message python-message-negative-control
python-message: python-toolchain-check deps-go
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonMessage' ./internal/codegen/pythontable/
python-message-negative-control: python-toolchain-check deps-go
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonMessageNegativeControl$$' ./internal/codegen/pythontable/
test-python: python-message python-message-negative-control
