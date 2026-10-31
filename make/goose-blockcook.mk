# make/goose-blockcook.mk — the Goose leg's two ACCELERATORS: the block form
# (docs/SPEC-TABLES.md §19) and the cook (§7). Included by the Makefile's
# wildcard include beside make/goose.mk, whose pin ($(GOOSE)) and generated
# units (build/conformance-goose) it uses.
#
# The read surfaces and the cook's write side are the conformance harness's
# (test/conformance/goose/accel.goose, run by test/conformance/goose/driver);
# the builder's two-language golden and the block lead gate (docs/PORTING.md
# I5) are TestGooseBlockBuilderMatchesTheGoldens and TestGooseBlockLeadGate in
# internal/codegen/goosetable, which test-goose runs.

# THE BLOCK LEAD GATE through the driver (docs/PORTING.md I5): every block
# image of the manifest at every lead 0..64 past an aligned base, through the
# same program the harness runs.
.PHONY: tables-goose-block-lead
tables-goose-block-lead: build/conformance-goose
	@test -x "$(GOOSE)" || $(MAKE) dist-goose
	./test/conformance/goose/driver $(GOOSE_MANIFEST) block-lead

# THE BLOCK FORM'S NEGATIVE CONTROL (make/negative-controls.json, group
# goose-blockcook): TestGooseBlockNegativeControl drops Open's base-alignment
# clause (the lead gate must go red naming lead 1, the builder stays green)
# and Begin's array-start rounding (the builder must go red against the C++
# goldens, the lead gate stays green).
.PHONY: tables-goose-block-negative-control
tables-goose-block-negative-control:
	@test -x "$(GOOSE)" || $(MAKE) dist-goose
	go test -count=1 -run '^TestGooseBlockNegativeControl$$' ./internal/codegen/goosetable/

# THE ACCELERATORS' HARNESS CONTROLS (group goose-blockcook): one clause of
# the runtime broken in a copy of the generated units, and the harness must
# turn red exactly the surfaces it owns — the cook's reserved words (M8),
# the block's pitch assertion (M7) and the write's byte order (§7.6).
.PHONY: tables-goose-accel-negative-control
tables-goose-accel-negative-control: build/conformance-harness build/conformance-goose
	@test -x "$(GOOSE)" || $(MAKE) dist-goose
	sh test/conformance/goose/negative-control-accel cook-open
	sh test/conformance/goose/negative-control-accel block-pitch
	sh test/conformance/goose/negative-control-accel cook-order

test-goose: tables-goose-block-lead
