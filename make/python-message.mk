# The message-form golden and its generated-runtime negative control.
# Runtime files are embedded by the compiler; a runtime edit must rebuild the
# conformance compiler before a surface is compared.
# Each local test-python prerequisite is represented in the CI matrix. Surface
# rows use the fixed-corpus prefix so the planner restores the generated units
# and live C++ reference before invoking the exact same local targets.
PYTHON_MESSAGE_SHARDS := $(addprefix python-message-,0 1)
PYTHON_CI_SUPPORT_GROUPS += $(PYTHON_MESSAGE_SHARDS) python-message-negative-control \
    python-message-write-negative-control python-message-crc-negative-control \
    python-message-onesided-negative-control python-message-substitution-negative-control \
    python-message-arm-shape-negative-control \
    python-message-noncanonical python-message-noncanonical-negative-control \
    python-fixed-message-conformance python-fixed-message-surface-control
.PHONY: python-fixed-message-conformance python-fixed-message-surface-control
python-fixed-message-conformance: python-message-conformance
python-fixed-message-surface-control: python-message-surface-negative-control
build/schema-python: $(shell find internal/codegen/pythontable/runtime/_schema -type f)
# The message tests no other row of this file runs are split into bounded
# shards so cold Go compilation plus one shard fits the two-minute per-commit
# cap. internal/ci's TestPythonMessageCIPlan proves every ^TestPythonMessage
# function in the package source is run by exactly one registered row of this
# file (a shard or a dedicated row), so a new message test fails ci-policy
# until a row claims it. python-message still runs every one of them.
.PHONY: python-message $(PYTHON_MESSAGE_SHARDS) python-message-negative-control
python-message: $(PYTHON_MESSAGE_SHARDS) python-message-negative-control python-message-write-negative-control \
    python-message-crc-negative-control python-message-onesided-negative-control \
    python-message-substitution-negative-control python-message-arm-shape-negative-control
python-message-0: python-toolchain-check deps-go
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonMessage(Bits|RangeInformation|NestedArray|Evolution|FixedKinds|UnionEnum|MixedKinds|ArrayEvolution|PacketValue|HostilePayload|RootIdentity)?$$' ./internal/codegen/pythontable/
python-message-1: python-toolchain-check deps-go
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonMessage(Announcement|PublicNameCollision|KeyedOptional|Damage|HostileAnnouncement|Wide|Widening|ShapeBenchFunctional|ZeroWidthSkip|UnionArray)$$' ./internal/codegen/pythontable/
python-message-negative-control: python-toolchain-check deps-go
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonMessageNegativeControl$$' ./internal/codegen/pythontable/
test-python: $(PYTHON_MESSAGE_SHARDS) python-message-negative-control

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

.PHONY: python-message-arm-shape-negative-control
python-message-arm-shape-negative-control: python-toolchain-check deps-go
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonMessageUnionKeyedArmShape$$' ./internal/codegen/pythontable/
test-python: python-message-arm-shape-negative-control

# The message read's canonical rules (SPEC-TABLES §3.3 (a)-(c)) over the shared
# non-canonical vectors, the Go engine as the oracle, and each refusal's removal
# planted in the generated runtime.
.PHONY: python-message-noncanonical python-message-noncanonical-negative-control
python-message-noncanonical: python-toolchain-check deps-go
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonMessageReadsRefuseNonCanonical$$' ./internal/codegen/pythontable/
python-message-noncanonical-negative-control: python-toolchain-check deps-go
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonMessageReadsRefuseNonCanonicalNegativeControl$$' ./internal/codegen/pythontable/
test-python: python-message-noncanonical python-message-noncanonical-negative-control
