# THE PACKET-WRITE BARE-INTEGER WIDTH CHECK (docs/SPEC.md §5.1 row 11,
# docs/PACKET-WIRE-ALGORITHM.md §2.4): test/packet-write-bareint's shared cases
# through the generated writer, every refused case tripped by the signed
# narrow field's debug assert and every boundary twin written and read back at
# its declared width. The control removes that assert through an emitter
# overlay and requires the signed out-of-width cases red by name.
PACKET_WRITE_BAREINT_LOBSTER_NEG := build/packet-write-bareint/lobster-negative

.PHONY: packet-write-bareint-lobster packet-write-bareint-lobster-negative-control
packet-write-bareint-lobster: toolchain-lobster
	SCHEMA_SLOW=1 go test -count=1 -run '^TestLobsterPacketWriteBareInt$$' ./test/packet-write-bareint/lobster

packet-write-bareint-lobster-negative-control: packet-write-bareint-lobster
	@mkdir -p $(PACKET_WRITE_BAREINT_LOBSTER_NEG)
	go run ./tools/sabotage -name packet-write-bareint-lobster-width \
		-out $(PACKET_WRITE_BAREINT_LOBSTER_NEG)/write.gotext internal/codegen/lobster/write.go
	@printf '{"Replace":{"%s/internal/codegen/lobster/write.go":"%s/$(PACKET_WRITE_BAREINT_LOBSTER_NEG)/write.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > $(PACKET_WRITE_BAREINT_LOBSTER_NEG)/overlay.json
	@if SCHEMA_SLOW=1 go test -count=1 -overlay=$(PACKET_WRITE_BAREINT_LOBSTER_NEG)/overlay.json \
		-run '^TestLobsterPacketWriteBareInt$$' ./test/packet-write-bareint/lobster \
		> $(PACKET_WRITE_BAREINT_LOBSTER_NEG)/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: removing the signed narrow width assert left the packet write gate green'; exit 1; fi
	@grep -Fq 'FAILED: int8-above-max: wrote a value the width refuses' $(PACKET_WRITE_BAREINT_LOBSTER_NEG)/log || \
		{ echo 'NEGATIVE CONTROL FAILED: the Lobster packet write gate failed for another reason'; cat $(PACKET_WRITE_BAREINT_LOBSTER_NEG)/log; exit 1; }
	@echo 'packet write bare integer Lobster negative control: removing the signed narrow width assert reds int8-above-max'

test-lobster: packet-write-bareint-lobster packet-write-bareint-lobster-negative-control
