# make/squirrel-packet-wide.mk — the SQUIRREL leg of the packet wide-text check
# (test/packet-wide/README.md; SPEC §4.12). The Makefile's wildcard include
# pulls this in.
#
# The leg is test/packet-wide/squirrel/main.nut, one $(SQ) process: its
# `contracts` mode holds the writer's length refusals, the reader's surrogate
# pairing, the untouched reused tail, an unaligned conditional and its branch
# zeroing, fixed and counted arrays, the born count and a repeated union tag;
# its `corpus` mode answers the shared harness's lines, every pinned wstring
# row at bounds 7 and 4 and every single-bit mutation, against the C++ oracle.

build/packet-wide/squirrel/.stamp: bin/schema build/packet-wide/source/WideText.schema test/packet-wide/Shapes.schema make/squirrel-packet-wide.mk
	@rm -rf build/packet-wide/squirrel
	./bin/schema generate --lang squirrel --out build/packet-wide/squirrel build/packet-wide/source/WideText.schema
	./bin/schema generate --lang squirrel --out build/packet-wide/squirrel/shapes test/packet-wide/Shapes.schema
	@touch $@

.PHONY: packet-wide-squirrel packet-wide-squirrel-negative-control
packet-wide-squirrel: build/packet-wide/squirrel/.stamp build/packet-wide/cpp/driver build/packet-text/harness toolchain-squirrel
	@mkdir -p build/packet-wide/squirrel-contracts
	@# sq exits 0 on a runtime error, so a contract that throws is caught by its stderr
	"$(SQ)" test/packet-wide/squirrel/main.nut contracts 2> build/packet-wide/squirrel-contracts/stderr
	@if [ -s build/packet-wide/squirrel-contracts/stderr ]; then cat build/packet-wide/squirrel-contracts/stderr >&2; exit 1; fi
	./build/packet-text/harness -wide -corpus testdata/conformance/text/wstring.txt -oracle build/packet-wide/cpp/driver "$(SQ)" test/packet-wide/squirrel/main.nut corpus

# THE PAIRING CONTROL: the generated reader stops refusing a low surrogate
# that does not follow a high one (and a high one followed by anything else),
# so the mutated corpus must disagree with the C++ oracle.
packet-wide-squirrel-negative-control: packet-wide-squirrel
	@rm -rf build/packet-wide/squirrel-negative
	@mkdir -p build/packet-wide/squirrel-negative
	go run ./tools/sabotage -name packet-wide-squirrel-pairing -out build/packet-wide/squirrel-negative/read.gotext internal/codegen/squirrel/read.go
	@printf '{"Replace":{"%s/internal/codegen/squirrel/read.go":"%s/build/packet-wide/squirrel-negative/read.gotext"}}\n' "$(CURDIR)" "$(CURDIR)" > build/packet-wide/squirrel-negative/overlay.json
	go run -overlay=build/packet-wide/squirrel-negative/overlay.json ./cmd/schema generate --lang squirrel --out build/packet-wide/squirrel-negative/gen build/packet-wide/source/WideText.schema
	@if ./build/packet-text/harness -wide -corpus testdata/conformance/text/wstring.txt -oracle build/packet-wide/cpp/driver -mutations-only "$(SQ)" test/packet-wide/squirrel/main.nut corpus "$(CURDIR)/build/packet-wide/squirrel-negative/gen" > build/packet-wide/squirrel-negative/log 2>&1; then echo 'NEGATIVE CONTROL FAILED: Squirrel wide pairing removal passed'; exit 1; fi
	@grep -Fq 'FAILED: packet-text verdict on ' build/packet-wide/squirrel-negative/log || { cat build/packet-wide/squirrel-negative/log; exit 1; }
	@echo 'packet wide Squirrel negative control: removed pairing fails bit-flip agreement'

test-squirrel: packet-wide-squirrel packet-wide-squirrel-negative-control
