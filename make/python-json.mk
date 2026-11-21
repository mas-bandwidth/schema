# The Python table text walk and its field-key negative control.
.PHONY: python-json python-json-negative-control

PYTHON_CI_SUPPORT_GROUPS += python-json python-json-negative-control

test-python: python-json python-json-negative-control

python-json: python-toolchain-check
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -run '^TestPythonJson' ./internal/codegen/pythontable/

python-json-negative-control: python-toolchain-check
	SCHEMA_SLOW=1 go test -count=1 -run '^TestPythonJsonFieldMatchNegativeControl$$' ./internal/codegen/pythontable/
