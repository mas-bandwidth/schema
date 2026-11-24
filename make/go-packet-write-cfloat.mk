# PACKET WRITE COMPRESSED FLOAT, the Go leg (SPEC §5.1 row 10;
# PACKET-WIRE-ALGORITHM.md §2.5): test/packet-write-cfloat/go/main.go runs every
# shared case of test/packet-write-cfloat/cases.txt through the generated Go
# writer. Go refuses in every build; a written case reads back to its quantized
# value and, where the case pins it, writes the exact wire.
build/packet-write-cfloat/go/.stamp: bin/schema test/packet-write-cfloat/PacketCFloat.schema make/go-packet-write-cfloat.mk
	@rm -rf build/packet-write-cfloat/go && mkdir -p build/packet-write-cfloat/go
	./bin/schema generate --lang go --out build/packet-write-cfloat/go/cfloat test/packet-write-cfloat/PacketCFloat.schema
	@printf 'module cfloat\n\ngo 1.23\n' > build/packet-write-cfloat/go/cfloat/go.mod
	@touch $@

.PHONY: packet-write-cfloat-go
packet-write-cfloat-go: build/packet-write-cfloat/go/.stamp build/mustrun
	cd test/packet-write-cfloat/go && $(CURDIR)/$(MUSTRUN) -name packet-write-cfloat-go -count 12 -- go run . ../cases.txt

test-go: packet-write-cfloat-go
