# Reflection's corpus comparison and fault-injection controls.
.PHONY: python-reflection python-reflection-negative-control
PYTHON_CI_SUPPORT_GROUPS += python-ci-reflection
python-ci-reflection: python-reflection python-reflection-negative-control
test-python: python-reflection python-reflection-negative-control

python-reflection: python-toolchain-check
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -run '^TestPythonReflection' ./internal/codegen/pythontable/
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -run '^TestPythonReflectionDriver' ./compiler/

python-reflection-negative-control: python-toolchain-check
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -run '^TestPythonReflection(CorpusNegativeControl|KindHoleNegativeControl|VocabularyNegativeControl|FieldVocabularyNegativeControl|LayoutNegativeControl|ResetLargeArraysNegativeControl|AccessorNegativeControl|AnnotationAgreementNegativeControl|DescriptorDocsNegativeControl)' ./internal/codegen/pythontable/
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -run '^TestPythonReflectionDriver(NegativeControl|PacketUnionNegativeControl)' ./compiler/
