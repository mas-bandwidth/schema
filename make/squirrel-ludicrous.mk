# make/squirrel-ludicrous.mk — the Squirrel leg's LUDICROUS PACKET PROGRAM,
# the twin of test/luau-ludicrous, test/js-ludicrous and
# test/ludicrous_main.cpp: examples128 generated for Squirrel, every pinned
# instance written byte for byte against the C++-pinned goldens in
# testdata/wire, read back, the defaults, branch zeroing and hostile
# rejections. The nightly readiness run (test/conformance/nightly-readiness
# squirrel) executes the program as its packet-ludicrous phase; `make
# test-squirrel` runs it with its control.
#
#   squirrel-ludicrous                   the program, then the lint gate's
#                                        compile-and-canonical-text check over it
#   squirrel-ludicrous-negative-control  the same program over a copy of the
#                                        goldens with one byte of
#                                        ludicrous_state flipped: it must go red
#                                        on exactly that golden

build/squirrel-ludicrous/.stamp: bin/schema $(SCHEMAS128)
	@rm -rf build/squirrel-ludicrous && mkdir -p build/squirrel-ludicrous
	./bin/schema generate --lang squirrel --out build/squirrel-ludicrous examples128
	@touch $@

.PHONY: squirrel-ludicrous squirrel-ludicrous-negative-control
squirrel-ludicrous: toolchain-squirrel build/squirrel-ludicrous/.stamp
	"$(SQ)" test/squirrel-ludicrous/main.nut build/squirrel-ludicrous testdata/wire
	sh make/squirrel-lint.sh "$(SQ)" test/squirrel-ludicrous

squirrel-ludicrous-negative-control: toolchain-squirrel build/squirrel-ludicrous/.stamp
	@rm -rf build/squirrel-ludicrous-negative && mkdir -p build/squirrel-ludicrous-negative/wire
	@cp testdata/wire/ludicrous_state.bin testdata/wire/ludicrous_state_untargeted.bin \
		testdata/wire/degenerate_probe.bin testdata/wire/unsigned_probe.bin build/squirrel-ludicrous-negative/wire/
	@printf '\377' | dd of=build/squirrel-ludicrous-negative/wire/ludicrous_state.bin bs=1 seek=5 conv=notrunc 2>/dev/null
	@if cmp -s testdata/wire/ludicrous_state.bin build/squirrel-ludicrous-negative/wire/ludicrous_state.bin; then \
		echo 'NEGATIVE CONTROL: the flipped golden byte changed nothing'; exit 1; fi
	@if "$(SQ)" test/squirrel-ludicrous/main.nut build/squirrel-ludicrous build/squirrel-ludicrous-negative/wire > build/squirrel-ludicrous-negative/log 2>&1 && \
		! grep -q '^FAILED: ' build/squirrel-ludicrous-negative/log; then \
		echo 'NEGATIVE CONTROL FAILED: a flipped ludicrous_state golden passed the Squirrel ludicrous program'; exit 1; fi
	@grep -Fq 'FAILED: wire golden ludicrous_state:' build/squirrel-ludicrous-negative/log || \
		{ echo 'NEGATIVE CONTROL FAILED: the Squirrel ludicrous program failed for another reason'; cat build/squirrel-ludicrous-negative/log; exit 1; }
	@if grep '^FAILED: ' build/squirrel-ludicrous-negative/log | grep -Fv 'FAILED: wire golden ludicrous_state:' >/dev/null; then \
		cat build/squirrel-ludicrous-negative/log; echo 'NEGATIVE CONTROL FAILED: a check other than the flipped golden went red'; exit 1; fi
	@if grep -qx 'OK' build/squirrel-ludicrous-negative/log; then \
		cat build/squirrel-ludicrous-negative/log; echo 'NEGATIVE CONTROL FAILED: the program printed OK over a flipped golden'; exit 1; fi
	@echo 'squirrel ludicrous negative control: a flipped C++-pinned golden byte fails exactly that golden'

# THE PACKET FINITE GATE, by the name every other leg's carries: the
# positive half of TestSquirrelPacketFiniteNegativeControl's pair
# (internal/codegen/squirrel/finite_test.go), run through the slow-gate proof.
.PHONY: packet-finite-squirrel
packet-finite-squirrel: toolchain-squirrel
	sh test/slowgate/proof packet-finite-squirrel 'TestSquirrelPacketFloatsAreFinite' \
		./internal/codegen/squirrel -run '^TestSquirrelPacketFloatsAreFinite$$' -count=1 -timeout 600s

test-squirrel: squirrel-ludicrous squirrel-ludicrous-negative-control packet-finite-squirrel
