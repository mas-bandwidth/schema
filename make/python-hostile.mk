# make/python-hostile.mk — the PYTHON leg's shared hostile-corpus runner and
# its bounded memory/time gate (docs/HOSTILE-CORPUS.md, schema-shared-hostile-
# corpus; docs/SPEC-TABLES.md §16.9.4). The tests live in
# internal/codegen/pythontable/hostile_corpus_test.go and
# internal/codegen/pythontable/resource_bounds_test.go; both run the generated
# package under the pinned interpreters (pyrun, make dist-python).
#
# Each gate has its planted-bad-pattern control: the corpus control admits a
# form byte the fixed-form reader does not carry in a scratch copy of the
# emitted runtime, and the resource control raises the scanner's depth cap in a
# scratch copy, each required red by name.

.PHONY: schema-python-runs-hostile-corpus schema-python-runs-hostile-corpus-negative-control
schema-python-runs-hostile-corpus: python-toolchain-check
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -run '^(TestPythonRunsHostileCorpus|TestPythonHostileScorerOutcomes)$$' ./internal/codegen/pythontable/

schema-python-runs-hostile-corpus-negative-control: python-toolchain-check
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -run '^TestPythonHostileCorpusPlantedReader$$' ./internal/codegen/pythontable/

.PHONY: schema-python-resource-bounds schema-python-resource-bounds-negative-control
schema-python-resource-bounds: python-toolchain-check
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -run '^TestPythonResourceBoundsHostileShapes$$' ./internal/codegen/pythontable/

schema-python-resource-bounds-negative-control: python-toolchain-check
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -run '^TestPythonResourceBoundsPlantedPattern$$' ./internal/codegen/pythontable/

# THE CI SUPPORT ROW: the four targets are one row of the Python leg's
# support matrix, so a direct test-python prerequisite never falls outside it.
PYTHON_CI_SUPPORT_GROUPS += python-hostile
.PHONY: python-hostile
python-hostile: schema-python-runs-hostile-corpus schema-python-runs-hostile-corpus-negative-control schema-python-resource-bounds schema-python-resource-bounds-negative-control

test-python: schema-python-runs-hostile-corpus schema-python-runs-hostile-corpus-negative-control schema-python-resource-bounds schema-python-resource-bounds-negative-control
