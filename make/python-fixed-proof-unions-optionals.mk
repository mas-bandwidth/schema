# Fixed-form union and optional rows against the live C++ reference.
.PHONY: python-fixed-proof-unions-optionals python-fixed-proof-unions-optionals-negative-control
python-fixed-proof-unions-optionals: dist-python
	SCHEMA_SLOW=1 SCHEMA_REQUIRE_CORPUS=1 go test -count=1 -v -timeout 600s -tags schema_leg_python -run '^TestPythonFixedProofUnionsOptionals$$' ./internal/codegen/pythontable/

python-fixed-proof-unions-optionals-negative-control: dist-python
	SCHEMA_SLOW=1 SCHEMA_REQUIRE_CORPUS=1 go test -count=1 -v -timeout 600s -tags schema_leg_python -run '^TestPythonFixedProofUnionsOptionals$$/^controls$$' ./internal/codegen/pythontable/

test-python: python-fixed-proof-unions-optionals python-fixed-proof-unions-optionals-negative-control
