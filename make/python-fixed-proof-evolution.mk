# Fixed-form evolution and its generated-artifact controls.
.PHONY: python-fixed-proof-evolution python-fixed-proof-evolution-negative-control
python-fixed-proof-evolution: dist-python tables-fixedform-corpus
	SCHEMA_SLOW=1 SCHEMA_REQUIRE_CORPUS=1 go test -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonFixedProofEvolution(Values|Strict|Numeric|WideningCorpus|WriterBounds|Hostile|HostileSweep|RetirementControl)?$$' ./internal/codegen/pythontable/

python-fixed-proof-evolution-negative-control: dist-python
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonFixedProofEvolutionNegativeControl$$' ./internal/codegen/pythontable/

test-python: python-fixed-proof-evolution python-fixed-proof-evolution-negative-control
