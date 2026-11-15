# make/python-block.mk — the block form on the Python table leg, read side and
# build side (docs/PORT-PYTHON.md). Included by the Makefile's wildcard include.
#
# The block form's two controls: a float one bit off turns the dump red, and
# the pitch, count, offset and base-alignment clauses each turn the refusal
# battery red; the builder's alignment, count bound and extent rounding each
# turn its probe red. Both run under the pinned interpreters. Registered in
# make/negative-controls.d/python-block-read-negative-control.json and
# make/negative-controls.d/python-block-build-negative-control.json.
.PHONY: python-block-read-negative-control python-block-build-negative-control
test-python: python-block-read-negative-control python-block-build-negative-control

python-block-read-negative-control:
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run TestPythonBlockReadNegativeControls ./internal/codegen/pythontable/

python-block-build-negative-control:
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run TestPythonBlockBuilderNegativeControls ./internal/codegen/pythontable/
