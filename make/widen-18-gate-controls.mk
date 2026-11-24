# make/widen-18-gate-controls.mk — the shared gates' negative controls, on EVERY
# leg (2026-11-24).
#
# The shared gates (tablegen, bitphase, canonical, message-fuzz, and the four
# sabotage classes packet, fixed, variable and retain) run on all 18 legs, but
# each had one negative control, and it sabotaged the JavaScript emitter: a
# gate proven red for JavaScript is proven red for no other leg. A leg whose
# driver compared its output with itself would pass every gate and every
# control. So each gate has a control per leg, taken from the leg list
# (VERIFY_ALL_18_LEGS), and each one plants the ORACLE wrong, the same way for
# every leg, through SCHEMA_GATE_PLANT (test/verify-bitphase/plant.go,
# test/verify-canonical/plant.go, test/verify-tablegen/plant.go,
# test/conformance/harness/plant.go): the leg's own gate target is run, and
# tools/gatecontrol requires it red, on the plant, on the planted case. The
# JavaScript emitter sabotages stay as they were (make/verify-widen-18.mk,
# make/verify-vm.mk): they prove the emitter side; these prove every leg's
# comparison.
#
#   verify-<gate>-negative-control-<leg>   one gate, one leg
#   verify-<gate>-negative-control-every-leg
#                                          one gate, all 18 legs (nightly,
#                                          make/negative-controls.json)
#
# NIGHTLY ONLY. Each control runs its leg's whole gate target; two of them
# (tablegen on go, canonical on python) took 5m35s on a bench. That is not
# light, so no pull request runs them (the owner's rule, 2026-11-15: heavy is
# nightly only). certify.yml's negative-controls-nightly runs all 144, one job
# per gate, from the gate-controls-<gate> groups.
#
# THIS FILE SORTS AFTER make/verify-widen-18.mk on purpose: the Makefile
# includes make/*.mk in name order, and the rules below are made at parse time
# from VERIFY_ALL_18_LEGS, which that file defines.
ifndef VERIFY_ALL_18_LEGS
$(error make/widen-18-gate-controls.mk is read before make/verify-widen-18.mk defines VERIFY_ALL_18_LEGS)
endif

VERIFY_GATE_CONTROLS := bitphase canonical tablegen message-fuzz sabotage-packet sabotage-fixed sabotage-variable sabotage-retain
GATECONTROL = go run ./tools/gatecontrol

define verify_gate_control_rules
.PHONY: $(foreach g,$(VERIFY_GATE_CONTROLS),verify-$(g)-negative-control-$(1))
verify-bitphase-negative-control-$(1):
	$$(GATECONTROL) -leg $(1) -plant bitphase -target verify-bitphase-$(1) -refuse 'BITPHASE_OK $(1) '
verify-sabotage-packet-negative-control-$(1):
	$$(GATECONTROL) -leg $(1) -plant packet -target verify-bitphase-$(1) -refuse 'BITPHASE_OK $(1) '
verify-canonical-negative-control-$(1):
	$$(GATECONTROL) -leg $(1) -plant canonical -target verify-canonical-$(1) -refuse 'CANONICAL_OK $(1) '
verify-tablegen-negative-control-$(1):
	$$(GATECONTROL) -leg $(1) -plant tablegen -target verify-tablegen-$(1) -refuse 'TABLEGEN_OK $(1) '
verify-message-fuzz-negative-control-$(1):
	$$(GATECONTROL) -leg $(1) -plant message-fuzz -target _verify-$(1)-fuzz-message -want 'message-fuzz DIVERGENCE'
verify-sabotage-fixed-negative-control-$(1):
	$$(GATECONTROL) -leg $(1) -plant fixed -target _verify-$(1)-conformance -want '$(1) / report: fixedevo_v1_as_v3'
verify-sabotage-variable-negative-control-$(1):
	$$(GATECONTROL) -leg $(1) -plant variable -target _verify-$(1)-conformance -want '$(1) / report: a1_arm_body_as_a2'
verify-sabotage-retain-negative-control-$(1):
	$$(GATECONTROL) -leg $(1) -plant retain -target _verify-$(1)-conformance -want '$(1) / retain-save: retain_node_record'
endef

$(foreach L,$(VERIFY_ALL_18_LEGS),$(eval $(call verify_gate_control_rules,$(L))))

# every leg's control of one gate (the nightly groups gate-controls-<gate> of
# make/negative-controls.json). Written out, not made by a define, so that
# tools/negativecontrols sees each one and holds it to a group.
.PHONY: verify-bitphase-negative-control-every-leg verify-canonical-negative-control-every-leg verify-tablegen-negative-control-every-leg verify-message-fuzz-negative-control-every-leg verify-sabotage-packet-negative-control-every-leg verify-sabotage-fixed-negative-control-every-leg verify-sabotage-variable-negative-control-every-leg verify-sabotage-retain-negative-control-every-leg
verify-bitphase-negative-control-every-leg: $(foreach L,$(VERIFY_ALL_18_LEGS),verify-bitphase-negative-control-$(L))
verify-canonical-negative-control-every-leg: $(foreach L,$(VERIFY_ALL_18_LEGS),verify-canonical-negative-control-$(L))
verify-tablegen-negative-control-every-leg: $(foreach L,$(VERIFY_ALL_18_LEGS),verify-tablegen-negative-control-$(L))
verify-message-fuzz-negative-control-every-leg: $(foreach L,$(VERIFY_ALL_18_LEGS),verify-message-fuzz-negative-control-$(L))
verify-sabotage-packet-negative-control-every-leg: $(foreach L,$(VERIFY_ALL_18_LEGS),verify-sabotage-packet-negative-control-$(L))
verify-sabotage-fixed-negative-control-every-leg: $(foreach L,$(VERIFY_ALL_18_LEGS),verify-sabotage-fixed-negative-control-$(L))
verify-sabotage-variable-negative-control-every-leg: $(foreach L,$(VERIFY_ALL_18_LEGS),verify-sabotage-variable-negative-control-$(L))
verify-sabotage-retain-negative-control-every-leg: $(foreach L,$(VERIFY_ALL_18_LEGS),verify-sabotage-retain-negative-control-$(L))
