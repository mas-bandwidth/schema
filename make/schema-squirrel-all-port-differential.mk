# make/schema-squirrel-all-port-differential.mk — THE SQUIRREL ALL-PORT
# DIFFERENTIAL (ROADMAP.md, docs/roadmap.sexp, group
# schema-per-language-lenses). Squirrel has no static checker of its own over
# the generated output, so this is its lens: every registered port answers the
# SAME conformance corpus and the SAME vectors, and Squirrel's accept/refuse
# verdict, refusal reason and decoded value must equal every other port's.
#
# The Makefile's wildcard include pulls this in; it names no language. The
# corpus is the harness's (testdata/conformance/tables), never a second one.
#
# IT IS HEAVY: it builds every leg and runs every driver, so it is nightly.
# The pure-Go controls below are the fast half and run on every push.

SQUIRREL_DIFFERENTIAL_WORK ?= build/conformance-differential
SQUIRREL_DIFFERENTIAL_TEST  = ./internal/ci -run '^TestSchemaSquirrelAllPortDifferential$$'
SQUIRREL_DIFFERENTIAL_FAST  = ./internal/ci -run '^TestSquirrelAllPortDifferential'

# The whole lens. `toolchain` refuses by name when a port's pin is missing;
# `$(CONFORMANCE_LEGS)` is every leg's generated conformance tree; the harness
# writes one answer per (port, surface, case), and the Go gate compares them.
#
# build/conformance-squirrel IS NAMED HERE, not left to $(CONFORMANCE_LEGS):
# the wildcard include reads this file before make/squirrel.mk, so the list does
# not yet carry the leg this whole lens is a lens on — without it the driver
# answers every case `.absent` and the comparison has nothing to compare.
# internal/ci/schema_squirrel_all_port_differential_test.go refuses that empty
# lens rather than passing it.
.PHONY: schema-squirrel-all-port-differential
schema-squirrel-all-port-differential: build/conformance-harness build/conformance-cpp build/conformance-squirrel build/schema_test_cook toolchain $(CONFORMANCE_LEGS)
	@rm -rf $(SQUIRREL_DIFFERENTIAL_WORK)
	@$(CONFORMANCE_ENV) ./build/conformance-harness run --work $(SQUIRREL_DIFFERENTIAL_WORK) \
		$(if $(SKIPPED_LEGS),--skip $(subst $(skip_space),$(skip_comma),$(strip $(SKIPPED_LEGS))))
	@SCHEMA_REQUIRE_CORPUS=1 SCHEMA_SQUIRREL_DIFFERENTIAL_WORK=$(SQUIRREL_DIFFERENTIAL_WORK) \
		nice -n 19 go test -count=1 -timeout 600s $(SQUIRREL_DIFFERENTIAL_TEST)

# The fast half: the comparison and its planted-bad-pattern control, on a
# t.TempDir() tree, with no toolchain and no corpus. This is what the push
# trigger runs.
.PHONY: schema-squirrel-all-port-differential-fast
schema-squirrel-all-port-differential-fast:
	@nice -n 19 go test -count=1 -timeout 600s $(SQUIRREL_DIFFERENTIAL_FAST)

# THE PLANTED-BAD-PATTERN CONTROL. A scratch copy of the answer tree gets one
# byte of Squirrel's wire answer changed; the comparison must go red and name
# the case (wire/Scene, byte 3). With the plant out the same tree is green.
# Registered in make/negative-controls.json (group squirrel).
.PHONY: schema-squirrel-all-port-differential-negative-control
schema-squirrel-all-port-differential-negative-control:
	@nice -n 19 go test -count=1 -timeout 600s ./internal/ci \
		-run '^TestSquirrelAllPortDifferentialDetectsADivergence$$'
	@echo 'squirrel all-port differential negative control: the planted wire byte is caught and named'
