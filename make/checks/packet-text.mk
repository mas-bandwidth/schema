build/packet-text/cpp/.stamp: bin/schema test/packet-text/Narrow.schema
	./bin/schema generate --lang cpp --out build/packet-text/cpp test/packet-text/Narrow.schema
	@touch $@

build/packet-text/cpp/driver: build/packet-text/cpp/.stamp test/packet-text/driver.c
	$(CXX) $(CXXFLAGS) -x c++ -Ibuild/packet-text/cpp test/packet-text/driver.c -o $@

build/packet-text/harness: test/packet-text/harness/main.go
	@mkdir -p build/packet-text
	go build -o $@ ./test/packet-text/harness

# The packet update is one reproducible integration gate (it began as the
# first nine). Each negative control depends on its positive checks; C++ is
# their wire oracle. It is every target (SCHEMA_PORTS) but these two:
#   cpp      the oracle the others are compared against, not a port under test
#   lobster  no packet-utf8 or packet-defaults gate exists for it yet
PACKET_WIRE_NA    := cpp lobster
PACKET_WIRE_PORTS := $(filter-out $(PACKET_WIRE_NA),$(SCHEMA_PORTS))
.PHONY: packet-wire-nine
packet-wire-nine:  $(addprefix packet-utf8-,$(addsuffix -negative-control,$(PACKET_WIRE_PORTS))) $(addprefix packet-defaults-,$(addsuffix -negative-control,$(PACKET_WIRE_PORTS)))
