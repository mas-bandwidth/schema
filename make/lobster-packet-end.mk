# ---- packet end: SPEC §5's one end check after the top-level object ----
#
# packet-end-lobster reads the shared vectors under testdata/wire/packet-end
# (test/packet-end/PacketEnd.schema) through the generated public ReadEnd:
# TestLobsterPacketEnd generates the schema and runs the driver
# test/packet-end/lobster/main.lobster under the pinned toolchain. The exact
# packet is accepted as 3; a whole trailing byte and set padding in the final
# byte are refused. The control removes the emitter's end call through a
# go test -overlay (tools/sabotage) and requires the trailing and padding rows
# red by name, and the exact row green.
.PHONY: packet-end-lobster packet-end-lobster-negative-control
packet-end-lobster: toolchain-lobster
	SCHEMA_SLOW=1 go test -count=1 -run '^TestLobsterPacketEnd$$' ./internal/codegen/lobster/

packet-end-lobster-negative-control: packet-end-lobster
	@rm -rf build/packet-end/lobster-negative && mkdir -p build/packet-end/lobster-negative
	go run ./tools/sabotage -name packet-end-lobster -out build/packet-end/lobster-negative/read.gotext internal/codegen/lobster/read.go
	@printf '{"Replace":{"%s/internal/codegen/lobster/read.go":"%s/build/packet-end/lobster-negative/read.gotext"}}\n' "$(CURDIR)" "$(CURDIR)" > build/packet-end/lobster-negative/overlay.json
	@if SCHEMA_SLOW=1 go test -count=1 -overlay=build/packet-end/lobster-negative/overlay.json -run '^TestLobsterPacketEnd$$' ./internal/codegen/lobster/ > build/packet-end/lobster-negative/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: the end check was removed and the Lobster packet-end driver stayed green'; exit 1; fi
	@grep -Fq 'FAILED: trailing' build/packet-end/lobster-negative/log || { cat build/packet-end/lobster-negative/log; exit 1; }
	@grep -Fq 'FAILED: padding' build/packet-end/lobster-negative/log || { cat build/packet-end/lobster-negative/log; exit 1; }
	@if grep -Fq 'FAILED: exact' build/packet-end/lobster-negative/log; then \
		cat build/packet-end/lobster-negative/log; echo 'NEGATIVE CONTROL FAILED: the exact packet went red as well'; exit 1; fi
	@echo 'packet end Lobster negative control: the removed end call accepts trailing bytes and set padding'

test-lobster: packet-end-lobster packet-end-lobster-negative-control

# ---- packet void: SPEC §4.8's payload-free union arms ----
#
# packet-void-lobster is the leg's half of test/packet-void: TestLobsterPacketVoid
# generates test/packet-void/Void.schema and runs the driver
# test/packet-void/lobster/main.lobster under the pinned toolchain with
# testdata/wire/packet-void/cases.txt and every pin beside it; internal/casegate
# judges its "case <name> ok|FAILED" lines (the Python leg's shape: the driver
# runs inside the Go test). The control adds one bit to the Mixed void arm on
# just the write or just the read path of the generated code and requires
# mixed-ping red by name.
.PHONY: packet-void-lobster packet-void-lobster-negative-control
packet-void-lobster: toolchain-lobster
	SCHEMA_SLOW=1 go test -count=1 -v -run '^TestLobsterPacketVoid$$' ./internal/codegen/lobster/

packet-void-lobster-negative-control: packet-void-lobster
	SCHEMA_SLOW=1 go test -count=1 -run '^TestLobsterPacketVoidNegativeControls$$' ./internal/codegen/lobster/
