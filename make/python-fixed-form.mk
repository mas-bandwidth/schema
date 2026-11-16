# Python form 3, its live C++ byte oracle and both artifact controls.
.PHONY: python-fixed-form python-fixed-form-negative-control
test-python: python-fixed-form python-fixed-form-negative-control
python-fixed-form:
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonFixedForm($$|Mixed$$|Strict$$|Lineage$$|BirthDefaults$$|WriterBounds$$|GuardedDefaults$$|PacketKeyed$$)' ./internal/codegen/pythontable/
python-fixed-form-negative-control:
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonFixedFormNegativeControl$$' ./internal/codegen/pythontable/

.PHONY: python-fixed-form-proof python-fixed-form-bench
test-python: python-fixed-form-proof
python-fixed-form-proof: build/fixedform-corpus/.stamp
	SCHEMA_SLOW=1 SCHEMA_REQUIRE_CORPUS=1 go test -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonFixedProofSuite$$' ./internal/codegen/pythontable/
python-fixed-form-bench:
	SCHEMA_SLOW=1 go test -v -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonFixedFormBenchFunctional$$' ./internal/codegen/pythontable/

.PHONY: python-fixed-form-conformance python-fixed-form-conformance-negative-control
test-python: python-fixed-form-conformance python-fixed-form-conformance-negative-control
python-fixed-form-conformance: build/conformance-python
	SCHEMA_SLOW=1 SCHEMA_REQUIRE_CORPUS=1 go test -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonFixedFormWriteRefusal$$' ./internal/codegen/pythontable/

.PHONY: python-fixed-writer-lineage python-fixed-writer-lineage-negative-control
test-python: python-fixed-writer-lineage python-fixed-writer-lineage-negative-control
python-fixed-writer-lineage:
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonFixedWriterLock$$' ./compiler/

python-fixed-form-conformance-negative-control: build/conformance-python
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonFixedFormWriteRefusalNegativeControl$$' ./internal/codegen/pythontable/

python-fixed-writer-lineage-negative-control:
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonFixedWriterLockNegativeControl$$' ./compiler/

.PHONY: python-fixed-core-values python-fixed-core-values-negative-control
test-python: python-fixed-core-values python-fixed-core-values-negative-control
python-fixed-core-values:
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonFixedCoreValues$$' ./internal/codegen/pythontable/
python-fixed-core-values-negative-control:
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonFixedCoreValuesNegativeControl$$' ./internal/codegen/pythontable/
