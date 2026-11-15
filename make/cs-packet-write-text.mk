# THE PACKET-WRITE TEXT GATE (docs/SPEC.md §5.1 row 8): the packet writer's
# string(N) used bytes hold no zero byte and are well-formed UTF-8. C# is an
# assert leg, so the check is a Debug.Assert answered through a throwing
# TraceListener; a release build carries no write check by design. The shared
# schema (test/packet-write-text/Text.schema) is generated and the shared cases
# (test/packet-write-text/cases.txt) are written and read back.
PACKET_WRITE_TEXT_CS := build/packet-write-text/cs
PACKET_WRITE_TEXT_CS_NEG := build/packet-write-text/cs-negative

build/packet-write-text/cs/.stamp: bin/schema test/packet-write-text/Text.schema make/cs-packet-write-text.mk
	@rm -rf $(PACKET_WRITE_TEXT_CS) && mkdir -p $(PACKET_WRITE_TEXT_CS)
	./bin/schema generate --lang cs --out $(PACKET_WRITE_TEXT_CS) test/packet-write-text/Text.schema
	@touch $@

.PHONY: packet-write-text-cs packet-write-text-cs-negative-control
packet-write-text-cs: build/packet-write-text/cs/.stamp
	$(DOTNET) run --project test/packet-write-text/cs/packet-write-text.csproj -- test/packet-write-text/cases.txt
	@echo 'packet write text C#: every ill-formed case asserted and every well-formed twin round-tripped'

# The control removes the write-side assert from the emitter through a go build
# overlay, regenerates the shared schema, and requires the driver to go red on
# the ill-formed cases by name while every well-formed twin stays accepted.
packet-write-text-cs-negative-control: packet-write-text-cs
	@rm -rf $(PACKET_WRITE_TEXT_CS_NEG) && mkdir -p $(PACKET_WRITE_TEXT_CS_NEG)/generated
	go run ./tools/sabotage -name packet-write-text-cs -out $(PACKET_WRITE_TEXT_CS_NEG)/functions.gotext internal/codegen/csharp/functions.go
	@printf '{"Replace":{"%s/internal/codegen/csharp/functions.go":"%s/%s/functions.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" "$(PACKET_WRITE_TEXT_CS_NEG)" > $(PACKET_WRITE_TEXT_CS_NEG)/overlay.json
	go build -overlay=$(PACKET_WRITE_TEXT_CS_NEG)/overlay.json -o $(PACKET_WRITE_TEXT_CS_NEG)/schema ./cmd/schema
	./$(PACKET_WRITE_TEXT_CS_NEG)/schema generate --lang cs --out $(PACKET_WRITE_TEXT_CS_NEG)/generated test/packet-write-text/Text.schema
	$(DOTNET) build test/packet-write-text/cs/packet-write-text.csproj \
		-p:TextDir="$(CURDIR)/$(PACKET_WRITE_TEXT_CS_NEG)/generated" -o $(PACKET_WRITE_TEXT_CS_NEG)/checker --nologo
	@if $(DOTNET) $(PACKET_WRITE_TEXT_CS_NEG)/checker/packet-write-text.dll test/packet-write-text/cases.txt > $(PACKET_WRITE_TEXT_CS_NEG)/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: packet-write-text-cs stayed green with the string(N) write check removed'; \
		cat $(PACKET_WRITE_TEXT_CS_NEG)/log; exit 1; fi
	@grep -Fq 'FAILED: interior-null was not refused' $(PACKET_WRITE_TEXT_CS_NEG)/log || \
		{ echo 'NEGATIVE CONTROL FAILED: the ill-formed writes were not refused by name'; cat $(PACKET_WRITE_TEXT_CS_NEG)/log; exit 1; }
	@grep '^FAILED: ' $(PACKET_WRITE_TEXT_CS_NEG)/log
	@echo 'packet write text C# negative control: removing the string(N) write check turns the ill-formed cases red'

test-cs: packet-write-text-cs packet-write-text-cs-negative-control
