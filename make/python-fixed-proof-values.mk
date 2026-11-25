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
$(PYTHON_FIXED_VALUE_TARGETS): dist-python
python-fixed-proof-value-read-rules: dist-python
python-fixed-proof-value-float-bounds: dist-python
