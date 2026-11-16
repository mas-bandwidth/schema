# Scalar fixed-form value rows against the live C++ reference.
.PHONY: python-fixed-proof-values python-fixed-proof-values-negative-control
test-python: python-fixed-proof-values python-fixed-proof-values-negative-control

python-fixed-proof-values: dist-python
	SCHEMA_SLOW=1 go test -count=1 -tags schema_leg_python -run '^TestPythonFixedProofValues(Types|ReadRules|FloatBounds)?$$' ./internal/codegen/pythontable/

python-fixed-proof-values-negative-control: dist-python
	SCHEMA_SLOW=1 go test -count=1 -tags schema_leg_python -run '^TestPythonFixedProofValues(NegativeControl|ReadRules|FloatBounds)$$' ./internal/codegen/pythontable/
