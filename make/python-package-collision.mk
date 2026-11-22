# Package exports and their generated-import negative control.
.PHONY: python-package-collision-negative-control
PYTHON_CI_SUPPORT_GROUPS += python-package-collision-negative-control
python-package-collision-negative-control: python-toolchain-check
	SCHEMA_SLOW=1 $(GOTESTRUN) -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonPackageNameCollisions$$' ./internal/codegen/pythontable/

test-python: python-package-collision-negative-control
