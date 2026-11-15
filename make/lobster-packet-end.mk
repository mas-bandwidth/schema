# ---- packet end: SPEC §5's one end check after the top-level object ----
#
# packet-end-lobster reads the shared vectors under testdata/wire/packet-end
# (test/packet-end/End.schema) through the generated public ReadEnd:
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
