# The keyed flags codec and its strict-write negative control.
.PHONY: python-keyed-flags-negative-control python-ci-keyed-flags
python-keyed-flags-negative-control:
	SCHEMA_SLOW=1 $(GOTESTRUN) -count=1 -timeout 600s -run '^TestPythonKeyedFlagsNegativeControl$$' ./internal/codegen/pythontable/
python-ci-keyed-flags:
	SCHEMA_SLOW=1 $(GOTESTRUN) -count=1 -timeout 600s -run '^TestPythonKeyedFlags' ./internal/codegen/pythontable/
PYTHON_CI_SUPPORT_GROUPS += python-ci-keyed-flags python-keyed-flags-negative-control
test-python: python-keyed-flags-negative-control
