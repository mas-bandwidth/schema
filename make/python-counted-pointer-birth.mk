# The counted pointer-array birth elision control.
.PHONY: python-counted-pointer-birth-negative-control
PYTHON_CI_SUPPORT_GROUPS += python-counted-pointer-birth-negative-control
test-python: python-counted-pointer-birth-negative-control

python-counted-pointer-birth-negative-control:
	SCHEMA_SLOW=1 $(GOTESTRUN) -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonVtPointerArraysNegativeControl$$/^counted-pointer-birth$$' ./internal/codegen/pythontable/
