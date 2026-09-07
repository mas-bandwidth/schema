build/packet-text/cpp/.stamp: bin/schema test/packet-text/Narrow.schema
	./bin/schema generate --lang cpp --out build/packet-text/cpp test/packet-text/Narrow.schema
	@touch $@

build/packet-text/cpp/driver: build/packet-text/cpp/.stamp test/packet-text/driver.c
	$(CXX) $(CXXFLAGS) -x c++ -Ibuild/packet-text/cpp test/packet-text/driver.c -o $@

build/packet-text/harness: test/packet-text/harness/main.go
	@mkdir -p build/packet-text
	go build -o $@ ./test/packet-text/harness

# Stage only the packet file: examples-wide also carries table kind 33 and
# its baseline, whose support is a separate row from this packet sweep.
build/packet-wide/source/WideText.schema: examples-wide/WideText.schema
	@mkdir -p build/packet-wide/source
	cp $< $@

build/packet-wide/cpp/.stamp: bin/schema build/packet-wide/source/WideText.schema
	./bin/schema generate --lang cpp --out build/packet-wide/cpp build/packet-wide/source/WideText.schema
	@touch $@

build/packet-wide/cpp/driver: build/packet-wide/cpp/.stamp test/packet-wide/driver.c
	$(CXX) $(CXXFLAGS) -x c++ -Ibuild/packet-wide/cpp test/packet-wide/driver.c -o $@


# The first-nine packet update is one reproducible integration gate. Each
# negative control depends on its positive checks; C++ is their wire oracle.
PACKET_WIRE_PORTS := c rust go cs java js dart elixir
.PHONY: packet-wide-nine packet-wire-nine
packet-wide-nine: $(addprefix packet-wide-,$(addsuffix -negative-control,$(PACKET_WIRE_PORTS)))
packet-wire-nine: packet-wide-nine $(addprefix packet-utf8-,$(addsuffix -negative-control,$(PACKET_WIRE_PORTS))) $(addprefix packet-defaults-,$(addsuffix -negative-control,$(PACKET_WIRE_PORTS)))
