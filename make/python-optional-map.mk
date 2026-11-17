PYTHON_CI_SUPPORT_GROUPS += python-optional-map-negative-control
# Optional map controls plant presence and graph-numbering faults in a copy.
.PHONY: python-optional-map-negative-control
PYTHON_CI_SUPPORT_GROUPS += python-ci-optional-map
python-ci-optional-map: python-optional-map-negative-control
test-python: python-optional-map-negative-control

python-optional-map-negative-control: python-toolchain-check
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -run 'TestPythonOptionalMapNegativeControlFunctional' ./internal/codegen/pythontable/
