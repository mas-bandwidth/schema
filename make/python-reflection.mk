# Reflection's corpus comparison and fault-injection controls.
.PHONY: python-reflection python-reflection-negative-control
PYTHON_CI_SUPPORT_GROUPS += python-ci-reflection python-reflection-ci-corpus
python-ci-reflection: PYTHON_REFLECTION_CI=1
python-ci-reflection: python-reflection python-reflection-negative-control
test-python: python-reflection python-reflection-negative-control

python-reflection: python-toolchain-check
	@if [ -n "$(PYTHON_REFLECTION_CI)" ]; then \
	  names=$$(make -s python-reflection-ci-names | paste -sd '|' -); \
	  test -n "$$names"; SCHEMA_SLOW=1 go test -count=1 -timeout 600s -run "^($$names)$$" ./internal/codegen/pythontable/; \
	else SCHEMA_SLOW=1 go test -count=1 -timeout 600s -run '^TestPythonReflection' ./internal/codegen/pythontable/; fi
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -run '^TestPythonReflectionDriver' ./compiler/

python-reflection-negative-control: python-toolchain-check
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -run '^TestPythonReflection(CorpusNegativeControl|KindHoleNegativeControl|VocabularyNegativeControl|FieldVocabularyNegativeControl|LayoutNegativeControl|ResetLargeArraysNegativeControl|AccessorNegativeControl|AnnotationAgreementNegativeControl|DescriptorDocsNegativeControl)' ./internal/codegen/pythontable/
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -run '^TestPythonReflectionDriver(NegativeControl|PacketUnionNegativeControl)' ./compiler/

# The local target remains whole; CI runs the two listing/layout corpus tests
# together here and partitions the strict-types corpus in the planner.
.PHONY: python-reflection-ci-corpus
python-reflection-ci-corpus: python-toolchain-check
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -run '^(TestPythonReflectionCorpus|TestPythonReflectionLayout)$$' ./internal/codegen/pythontable/

.PHONY: python-reflection-ci-names
python-reflection-ci-names:
	@go test -list '^TestPythonReflection' ./internal/codegen/pythontable/ | sed -n '/^TestPythonReflection[A-Za-z0-9_]*$$/p' | sed '/^TestPythonReflectionStrictTypes$$/d; /^TestPythonReflectionCorpus$$/d; /^TestPythonReflectionLayout$$/d'
