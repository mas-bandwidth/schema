# make/squirrel-negative-controls.mk — the Squirrel port's in-package negative
# controls as make targets, the shape the nine ports give theirs (make/go.mk's
# tables-go-accessor-negative-control, make/dart.mk's text-differential
# control): one target per Test*NegativeControl, run alone through
# test/slowgate/proof (SCHEMA_SLOW=1, a gate skip refused, the test's own
# --- PASS line required), so tools/negativecontrols enumerates each one and
# make/negative-controls.json places it in a group. Each test plants its defect
# in memory or in a scratch copy and requires the gate it watches to go red on
# exactly its rows; the target passes when the control does.
#
# TestSquirrelWireFuzzNegativeControl is already a target,
# schema-squirrel-nightly-fuzz-negative-control (make/schema-squirrel-nightly-fuzz.mk).
# `make test-squirrel` still runs every one of these tests in its package
# proof; these targets are the controls' own handles.

SQUIRREL_NC_DEPS := toolchain-squirrel dist-squirrel build/fixedform-corpus/.stamp $(SERIALIZE_GO_RUNTIME)/go.mod

# squirrel_nc TARGET TEST PACKAGE: the one-test proof call.
define squirrel_nc
	sh test/slowgate/proof $(1) '$(2)' -count=1 -timeout 600s -run '^$(2)$$$$' ./$(3)/
endef

.PHONY: tables-squirrel-type-wire-read-negative-control tables-squirrel-type-wire-write-negative-control \
	tables-squirrel-fixed-form-negative-control tables-squirrel-json-negative-control \
	tables-squirrel-load-negative-control tables-squirrel-message-form-negative-control \
	tables-squirrel-save-negative-control tables-squirrel-base64-text-negative-control

tables-squirrel-type-wire-read-negative-control: $(SQUIRREL_NC_DEPS)
	$(call squirrel_nc,$@,TestSquirrelTypeWireReadNegativeControl,internal/codegen/squirrel)
tables-squirrel-type-wire-write-negative-control: $(SQUIRREL_NC_DEPS)
	$(call squirrel_nc,$@,TestSquirrelTypeWireWriteNegativeControl,internal/codegen/squirrel)
tables-squirrel-fixed-form-negative-control: $(SQUIRREL_NC_DEPS)
	$(call squirrel_nc,$@,TestSquirrelFixedFormNegativeControl,internal/codegen/squirreltable)
tables-squirrel-json-negative-control: $(SQUIRREL_NC_DEPS)
	$(call squirrel_nc,$@,TestSquirrelJsonNegativeControl,internal/codegen/squirreltable)
tables-squirrel-load-negative-control: $(SQUIRREL_NC_DEPS)
	$(call squirrel_nc,$@,TestSquirrelTableLoadNegativeControl,internal/codegen/squirreltable)
tables-squirrel-message-form-negative-control: $(SQUIRREL_NC_DEPS)
	$(call squirrel_nc,$@,TestSquirrelMessageFormNegativeControl,internal/codegen/squirreltable)
tables-squirrel-save-negative-control: $(SQUIRREL_NC_DEPS)
	$(call squirrel_nc,$@,TestSquirrelTableSaveNegativeControl,internal/codegen/squirreltable)
tables-squirrel-base64-text-negative-control: $(SQUIRREL_NC_DEPS)
	$(call squirrel_nc,$@,TestSquirrelBase64TextNegativeControl,internal/codegen/squirreltable)

# The 3.0.0 register's Squirrel controls (docs/PORTING.md M5, M6, M12, M14, M19
# and the builder's count refusal): each plants its defect in a scratch copy of
# one emitted runtime and requires its gate to go red on its own rows.
.PHONY: tables-squirrel-keyed-negative-control tables-squirrel-save-identity-negative-control \
	tables-squirrel-block-byte-order-negative-control tables-squirrel-json-label-negative-control \
	tables-squirrel-map-sort-negative-control tables-squirrel-count-stop-negative-control

tables-squirrel-keyed-negative-control: $(SQUIRREL_NC_DEPS)
	$(call squirrel_nc,$@,TestSquirrelKeyedNegativeControls,internal/codegen/squirreltable)
tables-squirrel-save-identity-negative-control: $(SQUIRREL_NC_DEPS)
	$(call squirrel_nc,$@,TestSquirrelTableSaveIdentityNegativeControl,internal/codegen/squirreltable)
tables-squirrel-block-byte-order-negative-control: $(SQUIRREL_NC_DEPS)
	$(call squirrel_nc,$@,TestSquirrelBlockByteOrderNegativeControl,internal/codegen/squirreltable)
tables-squirrel-json-label-negative-control: $(SQUIRREL_NC_DEPS)
	$(call squirrel_nc,$@,TestSquirrelJsonLabelNegativeControl,internal/codegen/squirreltable)
tables-squirrel-map-sort-negative-control: $(SQUIRREL_NC_DEPS)
	$(call squirrel_nc,$@,TestSquirrelMapWriterSortNegativeControl,internal/codegen/squirreltable)
tables-squirrel-count-stop-negative-control: $(SQUIRREL_NC_DEPS)
	$(call squirrel_nc,$@,TestSquirrelCountRefusalStopsTheBuilderNegativeControl,internal/codegen/squirreltable)
.PHONY: tables-squirrel-block-order-word-negative-control
tables-squirrel-block-order-word-negative-control: $(SQUIRREL_NC_DEPS)
	$(call squirrel_nc,$@,TestSquirrelBlockOrderWordNegativeControl,internal/codegen/squirreltable)

# I13, THE TEXT DIFFERENTIAL (docs/PORTING.md): the emitted save and ToJson
# against the compiler's own engine over 200 random float instances, both
# directions; its control writes a float32 at six significant digits.
.PHONY: tables-squirrel-text-differential tables-squirrel-text-differential-negative-control
tables-squirrel-text-differential: $(SQUIRREL_NC_DEPS)
	$(call squirrel_nc,$@,TestSquirrelTextDifferentialAgainstTheGoEngine,internal/codegen/squirreltable)
tables-squirrel-text-differential-negative-control: $(SQUIRREL_NC_DEPS)
	$(call squirrel_nc,$@,TestSquirrelTextDifferentialNegativeControl,internal/codegen/squirreltable)

.PHONY: tables-squirrel-damage-discard tables-squirrel-damage-discard-negative-control
tables-squirrel-damage-discard: toolchain-squirrel dist-squirrel
	sh test/slowgate/proof $@ 'TestSquirrelDamageDiscardsValues TestSquirrelDelegatedDamageDiscardsValue' ./internal/codegen/squirreltable -run '^TestSquirrel(DamageDiscardsValues|DelegatedDamageDiscardsValue)$$' -count=1
tables-squirrel-damage-discard-negative-control: toolchain-squirrel dist-squirrel
	$(call squirrel_nc,$@,TestSquirrelDamageDiscardNegativeControl,internal/codegen/squirreltable)

# M18: union-arm framing hands every payload to the ordinary field reader.
.PHONY: tables-squirrel-arms tables-squirrel-arms-negative-control
tables-squirrel-arms: toolchain-squirrel
	sh test/slowgate/proof $@ 'TestSquirrelArmIsAFieldLine TestSquirrelTableLoadMatchesTheReports TestSquirrelWideTextMatchesTheWire' ./internal/codegen/squirreltable -run '^TestSquirrel(ArmIsAFieldLine|TableLoadMatchesTheReports|WideTextMatchesTheWire)$$' -count=1
tables-squirrel-arms-negative-control:
	sh test/slowgate/proof $@ 'TestSquirrelArmFieldLineNegativeControl' ./internal/codegen/squirreltable -run '^TestSquirrelArmFieldLineNegativeControl$$' -count=1

# J1: the reflective block/cook path agrees with the compiler layout model.
.PHONY: tables-squirrel-descriptor-layout-agreement tables-squirrel-slot-negative-control
tables-squirrel-descriptor-layout-agreement: toolchain-squirrel
	sh test/slowgate/proof $@ 'TestSquirrelDescriptorLayoutAgreement TestSquirrelBlockReadMatchesTheCorpus TestSquirrelCookReadMatchesTheCorpus' ./internal/codegen/squirreltable -run '^TestSquirrel(DescriptorLayoutAgreement|BlockReadMatchesTheCorpus|CookReadMatchesTheCorpus)$$' -count=1
tables-squirrel-slot-negative-control: toolchain-squirrel
	sh test/slowgate/proof $@ 'TestSquirrelDescriptorLayoutNegativeControl' ./internal/codegen/squirreltable -run '^TestSquirrelDescriptorLayoutNegativeControl$$' -count=1
test-squirrel: tables-squirrel-descriptor-layout-agreement tables-squirrel-slot-negative-control
