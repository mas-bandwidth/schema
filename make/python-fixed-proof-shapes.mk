# Fixed-form shape proofs against the live C++ reference.
# Each row keeps its value suite, file/writer controls and independent semantic
# probe together. The aggregate remains the local/nightly entry point; CI runs
# the same assertions in bounded jobs rather than one two-minute job.
PYTHON_FIXED_SHAPE_ROWS := utf8-values byte-buffers nested-types nested-fixed-tables \
    fixed-arrays counted-arrays keyed-arrays nested-keyed-arrays text-bytes-flags-defaults
PYTHON_FIXED_SHAPE_TARGETS := $(addprefix python-fixed-proof-shapes-,$(PYTHON_FIXED_SHAPE_ROWS)) \
    python-fixed-proof-shapes-text python-fixed-proof-shapes-array-bounds
PYTHON_CI_SUPPORT_GROUPS += $(PYTHON_FIXED_SHAPE_TARGETS) python-fixed-proof-shapes-negative-control
.PHONY: python-fixed-proof-shapes $(PYTHON_FIXED_SHAPE_TARGETS) python-fixed-proof-shapes-negative-control
test-python: $(PYTHON_FIXED_SHAPE_TARGETS) python-fixed-proof-shapes-negative-control
python-fixed-proof-shapes: $(PYTHON_FIXED_SHAPE_TARGETS)
$(addprefix python-fixed-proof-shapes-,$(PYTHON_FIXED_SHAPE_ROWS)):
	SCHEMA_SLOW=1 go test -v -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonFixedProofShapes$$/^(value-suite|negative-controls|$(@:python-fixed-proof-shapes-%=%))$$/^$(@:python-fixed-proof-shapes-%=%)$$' ./internal/codegen/pythontable/
python-fixed-proof-shapes-text:
	SCHEMA_SLOW=1 go test -v -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonFixedProofShapesText$$' ./internal/codegen/pythontable/
python-fixed-proof-shapes-array-bounds:
	SCHEMA_SLOW=1 go test -v -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonFixedProofShapesArrayBounds$$' ./internal/codegen/pythontable/
python-fixed-proof-shapes-negative-control:
	SCHEMA_SLOW=1 go test -v -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonFixedProofShapesNegativeControl$$' ./internal/codegen/pythontable/
