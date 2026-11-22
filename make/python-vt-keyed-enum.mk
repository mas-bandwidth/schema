# The enum-valued keyed array control corrupts a generated package and its
# runtime, and requires the corresponding wire and validation probes to fail.
.PHONY: python-vt-keyed-enum-negative-control
PYTHON_CI_SUPPORT_GROUPS += python-ci-vt-keyed-enum
python-ci-vt-keyed-enum: python-vt-keyed-enum-negative-control
test-python: python-vt-keyed-enum-negative-control

python-vt-keyed-enum-negative-control: python-toolchain-check
	SCHEMA_SLOW=1 $(GOTESTRUN) -count=1 -timeout 600s -run 'TestPythonVtKeyedEnumNegativeControlFunctional' ./internal/codegen/pythontable/
