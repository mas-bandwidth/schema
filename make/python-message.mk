# The message-form golden and its generated-runtime negative control.
# Runtime files are embedded by the compiler; a runtime edit must rebuild the
# conformance compiler before a surface is compared.
build/schema-python: $(shell find internal/codegen/pythontable/runtime/_schema -type f)
.PHONY: python-message python-message-negative-control
python-message: python-toolchain-check deps-go
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonMessage' ./internal/codegen/pythontable/
python-message-negative-control: python-toolchain-check deps-go
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonMessageNegativeControl$$' ./internal/codegen/pythontable/
test-python: python-message python-message-negative-control

.PHONY: python-message-write-negative-control
python-message-write-negative-control: python-toolchain-check deps-go
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonMessageWriteNegativeControl$$' ./internal/codegen/pythontable/
test-python: python-message-write-negative-control

.PHONY: python-message-conformance
python-message-conformance: python-toolchain-check build/conformance-python tables-fixedform-corpus
	test/conformance/python/message-gate
test-python: python-message-conformance

.PHONY: python-message-surface-negative-control
python-message-surface-negative-control: python-toolchain-check build/conformance-python tables-fixedform-corpus
	test/conformance/python/message-negative-control
test-python: python-message-surface-negative-control

.PHONY: python-message-crc-negative-control
python-message-crc-negative-control: python-toolchain-check deps-go
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonMessageCRC$$' ./internal/codegen/pythontable/
test-python: python-message-crc-negative-control

.PHONY: python-message-onesided-negative-control
python-message-onesided-negative-control: python-toolchain-check deps-go
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonMessageOneSided$$' ./internal/codegen/pythontable/
test-python: python-message-onesided-negative-control

.PHONY: python-message-substitution-negative-control
python-message-substitution-negative-control: python-toolchain-check deps-go
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonMessageSubstituted(Enum|Union|Key)$$' ./internal/codegen/pythontable/
test-python: python-message-substitution-negative-control
