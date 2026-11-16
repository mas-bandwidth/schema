# Scalar fixed-form value rows against the live C++ reference.
PYTHON_FIXED_VALUE_ROWS := bool-values signed-integers unsigned-integers signed-128 unsigned-128 \
    ranged-integers bits-values float32-values float64-values compressed-floats \
    signed-fixed-point unsigned-fixed-point flags-values enum-values scalar-defaults
PYTHON_FIXED_VALUE_TARGETS := $(addprefix python-fixed-proof-value-,$(PYTHON_FIXED_VALUE_ROWS))
PYTHON_FIXED_VALUE_EXTRA := python-fixed-proof-value-read-rules python-fixed-proof-value-float-bounds
PYTHON_CI_SUPPORT_GROUPS += $(PYTHON_FIXED_VALUE_TARGETS) $(PYTHON_FIXED_VALUE_EXTRA)
.PHONY: python-fixed-proof-values python-fixed-proof-values-negative-control $(PYTHON_FIXED_VALUE_TARGETS) $(PYTHON_FIXED_VALUE_EXTRA)
test-python: $(PYTHON_FIXED_VALUE_TARGETS) $(PYTHON_FIXED_VALUE_EXTRA)
python-fixed-proof-values: $(PYTHON_FIXED_VALUE_TARGETS) $(PYTHON_FIXED_VALUE_EXTRA)
python-fixed-proof-values-negative-control: dist-python
	SCHEMA_SLOW=1 go test -count=1 -tags schema_leg_python -run '^TestPythonFixedProofValues(NegativeControl|ReadRules|FloatBounds)$$' ./internal/codegen/pythontable/
$(PYTHON_FIXED_VALUE_TARGETS): dist-python
	SCHEMA_SLOW=1 go test -v -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonFixedProofValues(Types|NegativeControl)?$$/^$(@:python-fixed-proof-value-%=%)$$' ./internal/codegen/pythontable/
python-fixed-proof-value-read-rules: dist-python
	SCHEMA_SLOW=1 go test -v -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonFixedProofValuesReadRules$$' ./internal/codegen/pythontable/
python-fixed-proof-value-float-bounds: dist-python
	SCHEMA_SLOW=1 go test -v -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonFixedProofValuesFloatBounds$$' ./internal/codegen/pythontable/
