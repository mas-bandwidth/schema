# PACKET WRITE COMPRESSED FLOAT (SPEC §5.1 row 10; PACKET-WIRE-ALGORITHM.md
# §2.5; test/packet-write-cfloat): every shared case runs through the generated
# CFloat writer. A refused case must be refused from a build with asserts on,
# and each lawful case (min, max and an interior value) must write and read
# back to its quantized value. The probe is
# test/packet-write-cfloat/lobster/main.lobster. The control removes the
# emitter's range assert through an overlay and requires exactly the three
# off-range cases to go red.
PACKET_WRITE_CFLOAT_LOBSTER_NEG := build/packet-write-cfloat/lobster-negative

.PHONY: packet-write-cfloat-lobster packet-write-cfloat-lobster-negative-control
packet-write-cfloat-lobster: toolchain-lobster
	SCHEMA_SLOW=1 LOBSTER=$(LOBSTER) go test -count=1 -timeout 600s ./internal/codegen/lobster/ -run '^TestLobsterPacketWriteCFloatRange$$'

packet-write-cfloat-lobster-negative-control: packet-write-cfloat-lobster
	@mkdir -p $(PACKET_WRITE_CFLOAT_LOBSTER_NEG)
	go run ./tools/sabotage -name packet-write-cfloat-lobster-range -out $(PACKET_WRITE_CFLOAT_LOBSTER_NEG)/write.gotext internal/codegen/lobster/write.go
	@printf '{"Replace":{"%s/internal/codegen/lobster/write.go":"%s/%s/write.gotext"}}\n' "$(CURDIR)" "$(CURDIR)" "$(PACKET_WRITE_CFLOAT_LOBSTER_NEG)" > $(PACKET_WRITE_CFLOAT_LOBSTER_NEG)/overlay.json
	@if SCHEMA_SLOW=1 LOBSTER=$(LOBSTER) go test -count=1 -timeout 600s -overlay=$(PACKET_WRITE_CFLOAT_LOBSTER_NEG)/overlay.json ./internal/codegen/lobster/ -run '^TestLobsterPacketWriteCFloatRange$$' > $(PACKET_WRITE_CFLOAT_LOBSTER_NEG)/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: the compressed float range assert was removed and the gate stayed green'; exit 1; fi
	@sed -n 's|.*: FAILED: \([^:]*\):.*|\1|p' $(PACKET_WRITE_CFLOAT_LOBSTER_NEG)/log | LC_ALL=C sort > $(PACKET_WRITE_CFLOAT_LOBSTER_NEG)/failed
	@printf 'above-max\nbelow-min\nlargest-finite-float32\n' | LC_ALL=C sort > $(PACKET_WRITE_CFLOAT_LOBSTER_NEG)/want
	@diff -u $(PACKET_WRITE_CFLOAT_LOBSTER_NEG)/want $(PACKET_WRITE_CFLOAT_LOBSTER_NEG)/failed || \
		{ echo 'NEGATIVE CONTROL FAILED: the removed assert did not red exactly the off-range cases'; cat $(PACKET_WRITE_CFLOAT_LOBSTER_NEG)/log; exit 1; }
	@echo 'packet write compressed float Lobster negative control: the removed range assert writes exactly the three off-range cases'

test-lobster: packet-write-cfloat-lobster packet-write-cfloat-lobster-negative-control
