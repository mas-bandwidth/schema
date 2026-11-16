# The Python table text walk and its field-key negative control.
.PHONY: python-json python-json-negative-control

test-python: python-json python-json-negative-control

python-json: python-toolchain-check
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonJson' ./internal/codegen/pythontable/

python-json-negative-control: python-toolchain-check
	SCHEMA_SLOW=1 go test -count=1 -tags schema_leg_python -run '^TestPythonJsonFieldMatchNegativeControl$$' ./internal/codegen/pythontable/
