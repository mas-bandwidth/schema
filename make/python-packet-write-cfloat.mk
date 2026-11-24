# PACKET WRITE COMPRESSED FLOAT, the Python leg (SPEC §5.1 row 10;
# PACKET-WIRE-ALGORITHM.md §2.5): test/packet-write-cfloat/python/main.py runs
# every shared case of test/packet-write-cfloat/cases.txt through the generated
# writer, on the current and the floor interpreter. Python refuses in every
# build; a written case reads back and, where the case pins it, writes the
# exact wire.
.PHONY: packet-write-cfloat-python
packet-write-cfloat-python: build/schema-python python-toolchain-check build/mustrun
	@rm -rf build/packet-write-cfloat/python && mkdir -p build/packet-write-cfloat/python
	./build/schema-python generate --lang python --out build/packet-write-cfloat/python/generated test/packet-write-cfloat/PacketCFloat.schema
	$(MUSTRUN) -name packet-write-cfloat-python -count 12 -- $(PYTHON) -W error -I -B test/packet-write-cfloat/python/main.py build/packet-write-cfloat/python/generated test/packet-write-cfloat/cases.txt
	$(MUSTRUN) -name packet-write-cfloat-python-floor -count 12 -- $(PYTHON_FLOOR) -W error -I -B test/packet-write-cfloat/python/main.py build/packet-write-cfloat/python/generated test/packet-write-cfloat/cases.txt
