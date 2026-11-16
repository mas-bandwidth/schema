# Fixed-form evolution and its generated-artifact controls. The original
# local aggregate remains available; CI and test-python use bounded rows.
PYTHON_FIXED_EVOLUTION_AGGREGATE := ^TestPythonFixedProofEvolution(Values|Strict|Numeric|WideningCorpus|WriterBounds|Hostile|HostileEnvelope|HostileShapes|HostileSweep|RetirementControl)?$$
PYTHON_FIXED_EVOLUTION_ROWS := base values strict-a strict-b numeric widen-a widen-b writer-bounds hostile hostile-envelope hostile-shapes hostile-sweep retirement
PYTHON_FIXED_EVOLUTION_TARGETS := $(addprefix python-fixed-proof-evolution-row-,$(PYTHON_FIXED_EVOLUTION_ROWS))
PYTHON_FIXED_EVOLUTION_PATTERN_base := ^TestPythonFixedProofEvolution$$
PYTHON_FIXED_EVOLUTION_PATTERN_values := ^TestPythonFixedProofEvolutionValues$$
PYTHON_FIXED_EVOLUTION_PATTERN_strict-a := ^TestPythonFixedProofEvolutionStrict$$/^(file-envelope|interoperability|plan-execution)$$
PYTHON_FIXED_EVOLUTION_PATTERN_strict-b := ^TestPythonFixedProofEvolutionStrict$$/^(field-evolution|retired-runtime|retirement)$$
PYTHON_FIXED_EVOLUTION_PATTERN_numeric := ^TestPythonFixedProofEvolutionNumeric$$
PYTHON_FIXED_EVOLUTION_PATTERN_widen-a := ^TestPythonFixedProofEvolutionWideningCorpus$$/^(int_widen|uint_widen|float_widen|array_elem_widen)$$
PYTHON_FIXED_EVOLUTION_PATTERN_widen-b := ^TestPythonFixedProofEvolutionWideningCorpus$$/^(range_widen|cfloat_range_widen|union_arm_payload_widen)$$
PYTHON_FIXED_EVOLUTION_PATTERN_writer-bounds := ^TestPythonFixedProofEvolutionWriterBounds$$
PYTHON_FIXED_EVOLUTION_PATTERN_hostile := ^TestPythonFixedProofEvolutionHostile$$
PYTHON_FIXED_EVOLUTION_PATTERN_hostile-envelope := ^TestPythonFixedProofEvolutionHostileEnvelope$$
PYTHON_FIXED_EVOLUTION_PATTERN_hostile-shapes := ^TestPythonFixedProofEvolutionHostileShapes$$
PYTHON_FIXED_EVOLUTION_PATTERN_hostile-sweep := ^TestPythonFixedProofEvolutionHostileSweep$$
PYTHON_FIXED_EVOLUTION_PATTERN_retirement := ^TestPythonFixedProofEvolutionRetirementControl$$
PYTHON_CI_SUPPORT_GROUPS += $(PYTHON_FIXED_EVOLUTION_TARGETS) python-fixed-proof-evolution-negative-control python-fixed-evolution-ci-coverage
.PHONY: python-fixed-proof-evolution python-fixed-proof-evolution-negative-control python-fixed-evolution-ci-coverage python-fixed-evolution-ci-patterns $(PYTHON_FIXED_EVOLUTION_TARGETS)
python-fixed-proof-evolution: dist-python tables-fixedform-corpus
	SCHEMA_SLOW=1 SCHEMA_REQUIRE_CORPUS=1 go test -count=1 -timeout 600s -tags schema_leg_python -run '$(PYTHON_FIXED_EVOLUTION_AGGREGATE)' ./internal/codegen/pythontable/
$(PYTHON_FIXED_EVOLUTION_TARGETS): dist-python tables-fixedform-corpus
	SCHEMA_SLOW=1 SCHEMA_REQUIRE_CORPUS=1 go test -v -count=1 -timeout 600s -tags schema_leg_python -run '$(PYTHON_FIXED_EVOLUTION_PATTERN_$(@:python-fixed-proof-evolution-row-%=%))' ./internal/codegen/pythontable/
python-fixed-evolution-ci-patterns:
	@printf '%s\n' 'aggregate $(PYTHON_FIXED_EVOLUTION_AGGREGATE)' $(foreach row,$(PYTHON_FIXED_EVOLUTION_ROWS),'$(row) $(PYTHON_FIXED_EVOLUTION_PATTERN_$(row))')
python-fixed-evolution-ci-coverage:
	SCHEMA_SLOW=1 go test -count=1 -tags schema_leg_python -run '^TestPythonFixedEvolutionCICoverage$$' ./internal/ci/
python-fixed-proof-evolution-negative-control: dist-python
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonFixedProofEvolution(NegativeControl|Hostile|HostileEnvelope|HostileShapes)$$' ./internal/codegen/pythontable/
test-python: $(PYTHON_FIXED_EVOLUTION_TARGETS) python-fixed-proof-evolution-negative-control python-fixed-evolution-ci-coverage
