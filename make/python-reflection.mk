# Reflection's corpus comparison and fault-injection controls.
# Strict mypy over every reflection unit is split into bounded per-commit rows
# (PYTHON_REFLECTION_STRICT_SHARD selects units by index modulo the shard
# count); python-reflection still runs every shard, and the package test with
# no selector runs every unit.
PYTHON_REFLECTION_STRICT_ROWS := $(addprefix python-ci-reflection-strict-,0 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20 21 22 23)
.PHONY: python-reflection python-reflection-core python-reflection-negative-control python-ci-reflection $(PYTHON_REFLECTION_STRICT_ROWS)
PYTHON_CI_SUPPORT_GROUPS += python-ci-reflection $(PYTHON_REFLECTION_STRICT_ROWS)
python-ci-reflection: python-reflection-core python-reflection-negative-control
test-python: python-reflection-core python-reflection-negative-control $(PYTHON_REFLECTION_STRICT_ROWS)

python-reflection: python-reflection-core $(PYTHON_REFLECTION_STRICT_ROWS)

python-reflection-core: python-toolchain-check
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -run '^TestPythonReflection' -skip '^TestPythonReflectionStrictTypes$$' ./internal/codegen/pythontable/
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -run '^TestPythonReflectionDriver' ./compiler/

$(PYTHON_REFLECTION_STRICT_ROWS): python-ci-reflection-strict-%: python-toolchain-check
	SCHEMA_SLOW=1 PYTHON_REFLECTION_STRICT_SHARD=$* go test -v -count=1 -timeout 600s -run '^TestPythonReflectionStrictTypes$$' ./internal/codegen/pythontable/

python-reflection-negative-control: python-toolchain-check
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -run '^TestPythonReflection(CorpusNegativeControl|KindHoleNegativeControl|VocabularyNegativeControl|FieldVocabularyNegativeControl|LayoutNegativeControl|ResetLargeArraysNegativeControl|AccessorNegativeControl|AnnotationAgreementNegativeControl|DescriptorDocsNegativeControl)' ./internal/codegen/pythontable/
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -run '^TestPythonReflectionDriver(NegativeControl|PacketUnionNegativeControl)' ./compiler/
