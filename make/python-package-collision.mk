# Package exports and their generated-import negative control.
.PHONY: python-package-collision-negative-control
python-package-collision-negative-control: python-toolchain-check
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonPackageNameCollisions$$' ./internal/codegen/pythontable/

test-python: python-package-collision-negative-control
