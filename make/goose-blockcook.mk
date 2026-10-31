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

# THE FORGERY FUZZER (docs/PORTING.md I3): valid block images and the Go
# reference's cooks, mutated (a word of the prologue or a triple, a bit, the
# caller's claimed extent, the base's lead), each refused or opened and held
# WHOLE by an oracle that re-derives every bound from the records as data and
# reads every byte of every row, region and reached node
# (test/conformance/goose/accel.goose, block-fuzz and cook-fuzz). The cook
# half's images are the ones the harness materialises (build/conformance/
# fixtures, the Go leg's tables-go-fuzz shape). SEED and N are the knobs and
# each line prints the seed it ran. Measured 2026-10-29 on the author's
# machine at N=20000: the block half under a second, the cook half 12 s.
GOOSE_ACCEL_FUZZ_SEED ?= $(SEED)
GOOSE_ACCEL_FUZZ_N    ?= 20000

.PHONY: tables-goose-fuzz
tables-goose-fuzz: build/conformance-harness build/conformance-goose
	@test -x "$(GOOSE)" || $(MAKE) dist-goose
	./build/conformance-harness run --drivers /dev/null --work build/conformance > /dev/null 2>&1 || true
	./test/conformance/goose/driver $(GOOSE_MANIFEST) block-fuzz $(GOOSE_ACCEL_FUZZ_SEED) $(GOOSE_ACCEL_FUZZ_N)
	./test/conformance/goose/driver $(GOOSE_MANIFEST) cook-fuzz $(GOOSE_ACCEL_FUZZ_SEED) $(GOOSE_ACCEL_FUZZ_N) build/conformance/fixtures

# ITS NEGATIVE CONTROLS (group goose-blockcook): BOTH halves of one
# accelerator's extent bound removed from a copy of the generated units — the
# block's rows and padding bounds, the cook's data and attribution lengths —
# and the fuzzer must go red on the oracle's extent finding
# (test/conformance/goose/negative-control-fuzz says why one half is not a
# control: the other refuses the same forgery, measured).
.PHONY: tables-goose-fuzz-negative-control
tables-goose-fuzz-negative-control: build/conformance-harness build/conformance-goose
	@test -x "$(GOOSE)" || $(MAKE) dist-goose
	./build/conformance-harness run --drivers /dev/null --work build/conformance > /dev/null 2>&1 || true
	sh test/conformance/goose/negative-control-fuzz block-extent $(GOOSE_ACCEL_FUZZ_SEED) $(GOOSE_ACCEL_FUZZ_N)
	sh test/conformance/goose/negative-control-fuzz cook-extent $(GOOSE_ACCEL_FUZZ_SEED) $(GOOSE_ACCEL_FUZZ_N)

test-goose: tables-goose-block-lead tables-goose-fuzz tables-goose-fuzz-negative-control

# THE ORDER WORD (docs/PORTING.md I7): the half no foreign surface forges, a
# block or a cook whose magic is intact and whose byte-order word says the
# other order. The forgery corpus carries it (block_byte_order,
# cook_byte_order_0/2/3); this control takes each Open's order-word clause out
# of a copy of the generated units and requires exactly those surfaces red,
# with block-foreign and cook-foreign, which reverse the magic, still green.
.PHONY: tables-goose-order-negative-control
tables-goose-order-negative-control: build/conformance-harness build/conformance-goose
	@test -x "$(GOOSE)" || $(MAKE) dist-goose
	sh test/conformance/goose/negative-control-accel block-order
	sh test/conformance/goose/negative-control-accel cook-order-word
