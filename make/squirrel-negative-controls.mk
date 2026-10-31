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
