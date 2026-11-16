# Keep the complete local Evolution test; CI delegates its existing subtests.
PYTHON_CI_SUPPORT_GROUPS += python-ci-vt-evolution-values python-ci-vt-evolution-wire python-ci-vt-evolution-write
.PHONY: python-ci-vt-evolution-values python-ci-vt-evolution-wire python-ci-vt-evolution-write
python-ci-vt-evolution-values: python-toolchain-check
	SCHEMA_SLOW=1 go test -v -count=1 -timeout 120s -tags schema_leg_python -run '^TestPythonVtEvolution$$/^(defaults_at|defaults_off|rename_table|rename_field|rename_variant)$$' ./internal/codegen/pythontable/
python-ci-vt-evolution-wire: python-toolchain-check
	SCHEMA_SLOW=1 go test -v -count=1 -timeout 120s -tags schema_leg_python -run '^TestPythonVtEvolution$$/^wire_[0-9]+_.*$$' ./internal/codegen/pythontable/
python-ci-vt-evolution-write: python-toolchain-check
	SCHEMA_SLOW=1 go test -v -count=1 -timeout 120s -tags schema_leg_python -run '^TestPythonVtEvolution$$/^write_[0-9]+_.*$$' ./internal/codegen/pythontable/
