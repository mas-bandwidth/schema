# Union/optional values and controls each retain their live C++ oracle.
PYTHON_FIXED_UNION_ROWS := union-values union-field-arms payload-free-arms union-arrays union-guards \
    optional-values optional-scalars optional-nesting optional-arrays
PYTHON_FIXED_UNION_TARGETS := $(addprefix python-fixed-proof-union-,$(PYTHON_FIXED_UNION_ROWS))
PYTHON_FIXED_UNION_TYPE_GROUPS := 0 1 2 3 4 5 6 7
PYTHON_FIXED_UNION_TYPE_TARGETS := $(addprefix python-fixed-proof-union-types-,$(PYTHON_FIXED_UNION_TYPE_GROUPS))
PYTHON_FIXED_UNION_TYPES_0 := batch-capacity|bits-values|bool-values|byte-buffers|compiled-plans|compressed-floats
PYTHON_FIXED_UNION_TYPES_1 := counted-arrays|definition-hash|enum-values|field-evolution|file-envelope|fixed-arrays
PYTHON_FIXED_UNION_TYPES_2 := fixed-closure|fixed-file-roundtrip|flags-values|float32-values|float64-values|interoperability
PYTHON_FIXED_UNION_TYPES_3 := keyed-arrays|live-extents|nested-fixed-tables|nested-keyed-arrays|nested-types|optional-arrays
PYTHON_FIXED_UNION_TYPES_4 := optional-nesting|optional-scalars|optional-values|payload-free-arms|plan-execution|plan-selection
PYTHON_FIXED_UNION_TYPES_5 := ranged-integers|record-measurement|retired-runtime|retirement|scalar-defaults|signed-128
PYTHON_FIXED_UNION_TYPES_6 := signed-fixed-point|signed-integers|text-bytes-flags-defaults|union-arrays|union-field-arms|union-guards
PYTHON_FIXED_UNION_TYPES_7 := union-values|unsigned-128|unsigned-fixed-point|unsigned-integers|utf8-values|writing
PYTHON_CI_SUPPORT_GROUPS += $(PYTHON_FIXED_UNION_TARGETS) $(PYTHON_FIXED_UNION_TYPE_TARGETS) python-fixed-proof-ci-selection
.PHONY: python-fixed-proof-unions-optionals python-fixed-proof-unions-optionals-negative-control $(PYTHON_FIXED_UNION_TARGETS) $(PYTHON_FIXED_UNION_TYPE_TARGETS) python-fixed-proof-ci-selection python-fixed-proof-ci-patterns
test-python: $(PYTHON_FIXED_UNION_TARGETS) $(PYTHON_FIXED_UNION_TYPE_TARGETS) python-fixed-proof-ci-selection
python-fixed-proof-unions-optionals: $(PYTHON_FIXED_UNION_TARGETS) $(PYTHON_FIXED_UNION_TYPE_TARGETS)
python-fixed-proof-unions-optionals-negative-control: dist-python
	SCHEMA_SLOW=1 SCHEMA_REQUIRE_CORPUS=1 $(GOTESTRUN) -v -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonFixedProofUnionsOptionals$$/^controls$$' ./internal/codegen/pythontable/
$(PYTHON_FIXED_UNION_TARGETS): dist-python
	SCHEMA_SLOW=1 SCHEMA_REQUIRE_CORPUS=1 $(GOTESTRUN) -v -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonFixedProofUnionsOptionals$$/^(values|controls|evolution)$$/^$(@:python-fixed-proof-union-%=%)$$' ./internal/codegen/pythontable/
$(PYTHON_FIXED_UNION_TYPE_TARGETS): dist-python
	SCHEMA_SLOW=1 SCHEMA_REQUIRE_CORPUS=1 $(GOTESTRUN) -v -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonFixedProofUnionsOptionals$$/^types$$/^($(PYTHON_FIXED_UNION_TYPES_$(@:python-fixed-proof-union-types-%=%)))$$' ./internal/codegen/pythontable/
python-fixed-proof-ci-patterns:
	@printf 'values %s\n' $(PYTHON_FIXED_VALUE_ROWS)
	@printf 'union %s\n' $(PYTHON_FIXED_UNION_ROWS)
	@printf 'types %s\n' '$(PYTHON_FIXED_UNION_TYPES_0)' '$(PYTHON_FIXED_UNION_TYPES_1)' '$(PYTHON_FIXED_UNION_TYPES_2)' '$(PYTHON_FIXED_UNION_TYPES_3)' '$(PYTHON_FIXED_UNION_TYPES_4)' '$(PYTHON_FIXED_UNION_TYPES_5)' '$(PYTHON_FIXED_UNION_TYPES_6)' '$(PYTHON_FIXED_UNION_TYPES_7)'
python-fixed-proof-ci-selection:
	SCHEMA_SLOW=1 $(GOTESTRUN) -count=1 -tags schema_leg_python -run '^TestPythonFixedProofCISelection$$' ./internal/codegen/pythontable/
