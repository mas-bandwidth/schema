# Required fields are present on form 1; the controls plant omissions and
# missing-required errors out of generated and runtime paths.
.PHONY: python-form1-required-negative-control
test-python: python-form1-required-negative-control
python-form1-required-negative-control:
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -run '^(TestPythonForm1Required(NegativeControls|FastControls|GuardControls)|TestPythonRequiredNestedDefaults)$$' ./internal/codegen/pythontable/

# The public shape matrix runs in eight disjoint slices; no cell is skipped.
PYTHON_CI_SUPPORT_GROUPS += $(addprefix python-ci-required-shape-,0 1 2 3 4 5 6 7)
.PHONY: $(addprefix python-ci-required-shape-,0 1 2 3 4 5 6 7)
$(addprefix python-ci-required-shape-,0 1 2 3 4 5 6 7): python-ci-required-shape-%:
	SCHEMA_PYTHON_REQUIRED_SHARD=$* sh test/slowgate/proof test-python-required-shard-$* 'TestPythonRequiredPublicShapes' -tags schema_leg_python ./compiler -run '^TestPythonRequiredPublicShapes$$' -count=1
