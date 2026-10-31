# make/schema-moo-generated-analyze-strict-gate.mk — the moo lens (the moo
# port's layer 7, docs/PORT-MOO.md; modelled on the Dart strict-analyze gate
# and the Luau analyze-strict lens). Its stem has a dash, so SCHEMA_PORTS never
# reads it as a language; the root Makefile's `include $(wildcard make/*.mk)`
# loads it after make/moo.mk, whose MOO_ANALYZE and toolchain-moo it uses.
#
# THE GATE. moo-analyze --mode=strict over EVERY unit of the schema corpus the
# Makefile's generate targets read (examples, examples128, examples-wide, the
# SCHEMAS_TABLES* directories, the bench corpus units one at a time, and each
# file of test/packet-* and test/tables), generated through the moo leg, and
# the vendored serialize.moo runtime once: no error and NO LINT WARNING.
# moo-analyze exits 0 on a lint warning, so the gate is that its output is
# empty, never the exit code alone. Every generated module must open with
# --!strict. A unit the generator refuses must be named in the test's
# known-refusal list with its reason, and a listed unit that starts generating
# fails the gate, so the list cannot rot. The table backend's output joins the
# corpus when it lands (strictGateGenerate in the test).
#
# THE PIN. moo-analyze has no --version, so the gate holds the analyzer to the
# pin by the evidence `make dist-moo` leaves beside it: the PIN file must name
# MOO_PIN, and the moo in the same directory must report the pinned release.
# Another analyzer is REFUSED by name; a missing one is "tool missing", never a
# skip (toolchain-moo first, then the test's own resolution, which fails under
# SCHEMA_SLOW=1).
#
# One analyzer process over every module: about 3 s for 147 units on the
# studio. The tests live in internal/codegen/moo (strictgate_test.go) so that
# ci-full's emitter row, which installs the moo pin, runs them too.

.PHONY: schema-moo-generated-analyze-strict-gate
schema-moo-generated-analyze-strict-gate: toolchain-moo
	@echo "moo-analyze --mode=strict: the pinned analyzer at $(MOO_ANALYZE)"
	sh test/slowgate/proof schema-moo-generated-analyze-strict-gate \
		'TestMooGeneratedAnalyzeStrictGate' \
		./internal/codegen/moo -run '^TestMooGeneratedAnalyzeStrictGate$$' -count=1

# THE GATE'S CONTROL. One real generated unit analyzed clean through the
# gate's own invocation, then one defect planted in a copy of a generated
# module, twice: a strict type error (the analyzer exits nonzero) and an unused
# local (a lint warning, on which it EXITS 0). Each must red the gate with
# exactly one diagnostic, its own kind, on the planted line.
.PHONY: schema-moo-generated-analyze-strict-gate-negative-control
schema-moo-generated-analyze-strict-gate-negative-control: toolchain-moo
	sh test/slowgate/proof schema-moo-generated-analyze-strict-gate-negative-control \
		'TestMooGeneratedAnalyzeStrictGatePlantedPattern/type-error TestMooGeneratedAnalyzeStrictGatePlantedPattern/lint-warning' \
		./internal/codegen/moo -run '^TestMooGeneratedAnalyzeStrictGatePlantedPattern$$' -count=1

# THE PIN CHECK'S CONTROL. The real analyzer beside a PIN naming another moo
# commit is refused, naming both commits; beside the true PIN it is accepted,
# so the refusal is the PIN and nothing else.
.PHONY: schema-moo-generated-analyze-strict-gate-pin-negative-control
schema-moo-generated-analyze-strict-gate-pin-negative-control: toolchain-moo
	sh test/slowgate/proof schema-moo-generated-analyze-strict-gate-pin-negative-control \
		'TestMooGeneratedAnalyzeStrictGatePinNegativeControl' \
		./internal/codegen/moo -run '^TestMooGeneratedAnalyzeStrictGatePinNegativeControl$$' -count=1

# LAYER 1's CONTROLS, AS MAKE TARGETS, so make/negative-controls.json can run
# them on their own row (they also ride `make test-moo` and ci-full's emitter
# row, where every internal/codegen/moo test runs under SCHEMA_SLOW=1).
#
# packet-wire-moo-negative-control: the type-wire golden gate's control. One
# bool written twice in the generated writer, so exactly the goldens that
# carry that field must stop matching the C++ reference's bytes.
.PHONY: packet-wire-moo-negative-control
packet-wire-moo-negative-control: toolchain-moo
	sh test/slowgate/proof packet-wire-moo-negative-control \
		'TestMooTypeWireWriteNegativeControl' \
		./internal/codegen/moo -run '^TestMooTypeWireWriteNegativeControl$$' -count=1

# packet-analyze-moo-negative-control: layer 1's analyzer control, an unused
# local planted in a generated module, which the analyzer must name.
.PHONY: packet-analyze-moo-negative-control
packet-analyze-moo-negative-control: toolchain-moo
	sh test/slowgate/proof packet-analyze-moo-negative-control \
		'TestMooAnalyzerGateNegativeControl' \
		./internal/codegen/moo -run '^TestMooAnalyzerGateNegativeControl$$' -count=1

# packet-wire-moo-emitter-negative-control: the same golden gate, sabotaged in
# the EMITTER rather than in a generated file (docs/PORTING.md I2's method):
# tools/sabotage writes a copy of internal/codegen/moo/wire.go whose bool write
# rides twice, `go test -overlay` builds the golden gate against it, and the
# gate must go red on EXACTLY the seven C++-pinned goldens whose instances
# write a bool (examples/Types.schema's ShipCreate, RigidBody and InputPacket,
# examples/Wire.schema's ProbeArray and TestData, examples/Joins.schema), with
# the other seven still matching. No tracked file is written.
MOO_WIRE_NC_DIR := build/packet-wire-moo-negative
MOO_WIRE_NC_RED := inputpacket joins probearray rigidbody_at_rest rigidbody_moving shipcreate_flags testdata
.PHONY: packet-wire-moo-emitter-negative-control
packet-wire-moo-emitter-negative-control: toolchain-moo
	@rm -rf $(MOO_WIRE_NC_DIR) && mkdir -p $(MOO_WIRE_NC_DIR)
	go run ./tools/sabotage -name packet-wire-moo-bool-twice \
		-out $(MOO_WIRE_NC_DIR)/wire.gotext internal/codegen/moo/wire.go
	@printf '{"Replace":{"%s/internal/codegen/moo/wire.go":"%s/$(MOO_WIRE_NC_DIR)/wire.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > $(MOO_WIRE_NC_DIR)/overlay.json
	@if SCHEMA_SLOW=1 go test -overlay=$(MOO_WIRE_NC_DIR)/overlay.json -count=1 -v ./internal/codegen/moo \
			-run '^TestMooTypeWireWritesTheGoldens$$' > $(MOO_WIRE_NC_DIR)/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: the moo golden gate stayed green with the bool written twice'; \
		cat $(MOO_WIRE_NC_DIR)/log; exit 1; fi
	@grep -q -- '--- FAIL: TestMooTypeWireWritesTheGoldens (' $(MOO_WIRE_NC_DIR)/log || \
		{ echo 'NEGATIVE CONTROL FAILED: the moo golden gate did not run to a verdict'; cat $(MOO_WIRE_NC_DIR)/log; exit 1; }
	@got=$$(sed -n 's/.*wire golden \([a-z_]*\): moo bytes.*/\1/p' $(MOO_WIRE_NC_DIR)/log | sort | tr '\n' ' ' | sed 's/ $$//'); \
	if [ "$$got" != "$(MOO_WIRE_NC_RED)" ]; then \
		echo "NEGATIVE CONTROL FAILED: the doubled bool redded [$$got], want exactly [$(MOO_WIRE_NC_RED)]"; \
		cat $(MOO_WIRE_NC_DIR)/log; exit 1; fi
	@echo 'packet wire moo negative control: a bool written twice in the emitter reds exactly the seven goldens that write a bool'
