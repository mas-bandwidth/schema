PYTHON_CI_SUPPORT_GROUPS += python-forms-metadata-negative-control
.PHONY: python-forms-metadata-negative-control
python-forms-metadata-negative-control:
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonFormsBlockMetadataNegativeControl$$' ./internal/codegen/pythontable/
test-python: python-forms-metadata-negative-control
