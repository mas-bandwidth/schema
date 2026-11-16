# Python form 3, its live C++ byte oracle and both artifact controls.
.PHONY: python-fixed-form python-fixed-form-negative-control
test-python: python-fixed-form python-fixed-form-negative-control
python-fixed-form:
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonFixedForm($$|Mixed$$|Strict$$|Lineage$$|BirthDefaults$$|WriterBounds$$)' ./internal/codegen/pythontable/
python-fixed-form-negative-control:
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonFixedFormNegativeControl$$' ./internal/codegen/pythontable/
