# THE PACKET WRITE-REFUSAL GATE (docs/SPEC.md §5.1, the `write-refusal-packet`
# surface): a generated packet type carries one field per contract row, and
# each refused value and its lawful twin at the same boundary go through the
# packet Write. C# is an assert leg, so a refusal is a Debug.Assert answered
# through a throwing TraceListener; a release build carries no write check by
# design. The shared schema and cases live in test/packet-write-refusal/.
PACKET_WRITE_REFUSAL_CS := build/packet-write-refusal/cs
PACKET_WRITE_REFUSAL_CS_NEG := build/packet-write-refusal/cs-negative

build/packet-write-refusal/cs/.stamp: bin/schema test/packet-write-refusal/RefusalPacket.schema make/cs-packet-write-refusal.mk
	@rm -rf $(PACKET_WRITE_REFUSAL_CS) && mkdir -p $(PACKET_WRITE_REFUSAL_CS)
	./bin/schema generate --lang cs --out $(PACKET_WRITE_REFUSAL_CS) test/packet-write-refusal/RefusalPacket.schema
	@touch $@

.PHONY: packet-write-refusal-cs packet-write-refusal-cs-negative-control
packet-write-refusal-cs: build/packet-write-refusal/cs/.stamp
	$(DOTNET) run --project test/packet-write-refusal/cs/packet-write-refusal.csproj -- test/packet-write-refusal/cases.txt
	@echo 'write-refusal-packet C#: every refused value asserted and every lawful twin written'

# The control removes the bits(N) width assert from the emitter through a go
# build overlay, regenerates the shared schema, and requires the driver to go
# red on the wide-bits case by name and on nothing else.
packet-write-refusal-cs-negative-control: packet-write-refusal-cs
	@rm -rf $(PACKET_WRITE_REFUSAL_CS_NEG) && mkdir -p $(PACKET_WRITE_REFUSAL_CS_NEG)/generated
	go run ./tools/sabotage -name packet-write-refusal-cs -out $(PACKET_WRITE_REFUSAL_CS_NEG)/flat.gotext internal/codegen/csharp/flat.go
	@printf '{"Replace":{"%s/internal/codegen/csharp/flat.go":"%s/%s/flat.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" "$(PACKET_WRITE_REFUSAL_CS_NEG)" > $(PACKET_WRITE_REFUSAL_CS_NEG)/overlay.json
	go build -overlay=$(PACKET_WRITE_REFUSAL_CS_NEG)/overlay.json -o $(PACKET_WRITE_REFUSAL_CS_NEG)/schema ./cmd/schema
	./$(PACKET_WRITE_REFUSAL_CS_NEG)/schema generate --lang cs --out $(PACKET_WRITE_REFUSAL_CS_NEG)/generated test/packet-write-refusal/RefusalPacket.schema
	$(DOTNET) build test/packet-write-refusal/cs/packet-write-refusal.csproj \
		-p:RefusalDir="$(CURDIR)/$(PACKET_WRITE_REFUSAL_CS_NEG)/generated" -o $(PACKET_WRITE_REFUSAL_CS_NEG)/checker --nologo
	@if $(DOTNET) $(PACKET_WRITE_REFUSAL_CS_NEG)/checker/packet-write-refusal.dll test/packet-write-refusal/cases.txt > $(PACKET_WRITE_REFUSAL_CS_NEG)/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: packet-write-refusal-cs stayed green with the bits(N) write check removed'; \
		cat $(PACKET_WRITE_REFUSAL_CS_NEG)/log; exit 1; fi
	@test "$$(grep -c '^FAILED: ' $(PACKET_WRITE_REFUSAL_CS_NEG)/log)" = 1 && \
		grep -Fq 'FAILED: write-refusal-packet bits_wide: was not refused' $(PACKET_WRITE_REFUSAL_CS_NEG)/log || \
		{ echo 'NEGATIVE CONTROL FAILED: removing the bits(N) check did not red exactly bits_wide'; cat $(PACKET_WRITE_REFUSAL_CS_NEG)/log; exit 1; }
	@grep '^FAILED: ' $(PACKET_WRITE_REFUSAL_CS_NEG)/log
	@echo 'write-refusal-packet C# negative control: removing the bits(N) write check turns bits_wide red and nothing else'

test-cs: packet-write-refusal-cs packet-write-refusal-cs-negative-control
