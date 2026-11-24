# PACKET WRITE COMPRESSED FLOAT, the Unity C# leg (SPEC §5.1 row 10;
# PACKET-WIRE-ALGORITHM.md §2.5). Unity C# is an assert leg, so the driver
# runs a Debug build where the range check is a Debug.Assert, answered through
# a throwing TraceListener; a written case reads back to its quantized value
# and, where the case pins it, writes the exact wire.
build/packet-write-cfloat/unity/.stamp: bin/schema test/packet-write-cfloat/PacketCFloat.schema make/unity-packet-write-cfloat.mk
	@rm -rf build/packet-write-cfloat/unity && mkdir -p build/packet-write-cfloat/unity
	./bin/schema generate --lang unity --out build/packet-write-cfloat/unity test/packet-write-cfloat/PacketCFloat.schema
	@touch $@

.PHONY: packet-write-cfloat-unity
packet-write-cfloat-unity: toolchain-unity build/packet-write-cfloat/unity/.stamp build/casegate
	$(DOTNET) build test/packet-write-cfloat/unity/packet-write-cfloat-unity.csproj -c Debug -o build/packet-write-cfloat/unity-debug --nologo -v quiet
	$(call packet_write_cfloat_gate,unity,packet-write-cfloat-unity) $(DOTNET) build/packet-write-cfloat/unity-debug/packet-write-cfloat-unity.dll test/packet-write-cfloat/cases.txt
