# THE WRITE-REFUSAL-PACKET SURFACE, C++ LEG (docs/SPEC.md §5.1; the Go leg is
# TestWriteRefusalPacket). The shared cases under test/packet-write-refusal/
# each change one field of a default packet and write it in a debug child, so
# an abort is the refusal; a lawful twin at the same boundary must be written.
PACKET_WRITE_REFUSAL_CPP := build/packet-write-refusal/cpp
PACKET_WRITE_REFUSAL_CPP_NEGATIVE := build/packet-write-refusal/cpp-negative
PACKET_WRITE_REFUSAL_SCHEMA := test/packet-write-refusal/RefusalPacket.schema
PACKET_WRITE_REFUSAL_CASES := test/packet-write-refusal/cases.txt

build/packet-write-refusal/cpp/.stamp: bin/schema test/packet-write-refusal/RefusalPacket.schema make/cpp-packet-write-refusal.mk
	@mkdir -p $(PACKET_WRITE_REFUSAL_CPP)
	./bin/schema generate --lang cpp --out $(PACKET_WRITE_REFUSAL_CPP) $(PACKET_WRITE_REFUSAL_SCHEMA)
	@touch $@

build/packet-write-refusal/cpp/driver: build/packet-write-refusal/cpp/.stamp test/packet-write-refusal/cpp/main.cpp make/cpp-packet-write-refusal.mk
	$(CXX) $(CXXFLAGS) -I$(PACKET_WRITE_REFUSAL_CPP) test/packet-write-refusal/cpp/main.cpp -o $@

.PHONY: packet-write-refusal-cpp
packet-write-refusal-cpp: build/packet-write-refusal/cpp/driver
	./$(PACKET_WRITE_REFUSAL_CPP)/driver $(PACKET_WRITE_REFUSAL_CASES)

test: packet-write-refusal-cpp

# ITS CONTROL moves the emitted range assert upper bound by one through tools/sabotage,
# builds the same driver against that copy, and requires exactly the one-past cases to
# go red (the ranged scalar, count, constant and reserved field) while the rest and every written twin stay green.
.PHONY: packet-write-refusal-cpp-negative-control
packet-write-refusal-cpp-negative-control: packet-write-refusal-cpp
	@rm -rf $(PACKET_WRITE_REFUSAL_CPP_NEGATIVE) && mkdir -p $(PACKET_WRITE_REFUSAL_CPP_NEGATIVE)
	go run ./tools/sabotage -name packet-write-refusal-cpp-upper \
		-out $(PACKET_WRITE_REFUSAL_CPP_NEGATIVE)/functions.gotext internal/codegen/cpp/functions.go
	@printf '{"Replace":{"%s/internal/codegen/cpp/functions.go":"%s/%s/functions.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" "$(PACKET_WRITE_REFUSAL_CPP_NEGATIVE)" > $(PACKET_WRITE_REFUSAL_CPP_NEGATIVE)/overlay.json
	go run -overlay=$(PACKET_WRITE_REFUSAL_CPP_NEGATIVE)/overlay.json ./cmd/schema generate --lang cpp \
		--out $(PACKET_WRITE_REFUSAL_CPP_NEGATIVE)/generated $(PACKET_WRITE_REFUSAL_SCHEMA)
	@grep -q 'SABOTAGED: upper bound moved by one' $(PACKET_WRITE_REFUSAL_CPP_NEGATIVE)/generated/RefusalPacketWire.h || \
		{ echo "NEGATIVE CONTROL FAILED: the upper-bound sabotage did not apply"; exit 1; }
	$(CXX) $(CXXFLAGS) -I$(PACKET_WRITE_REFUSAL_CPP_NEGATIVE)/generated test/packet-write-refusal/cpp/main.cpp \
		-o $(PACKET_WRITE_REFUSAL_CPP_NEGATIVE)/driver
	@if ./$(PACKET_WRITE_REFUSAL_CPP_NEGATIVE)/driver $(PACKET_WRITE_REFUSAL_CASES) > $(PACKET_WRITE_REFUSAL_CPP_NEGATIVE)/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the packet write-refusal gate stayed green with the range assert upper bound moved"; \
		cat $(PACKET_WRITE_REFUSAL_CPP_NEGATIVE)/log; exit 1; \
	fi
	@sed -n 's/^FAILED: packet-write-refusal-cpp: \([^:]*\):.*/\1/p' $(PACKET_WRITE_REFUSAL_CPP_NEGATIVE)/log | LC_ALL=C sort > $(PACKET_WRITE_REFUSAL_CPP_NEGATIVE)/failed
	@printf '%s\n' const_wrong count_high reserved_wrong scalar_high > $(PACKET_WRITE_REFUSAL_CPP_NEGATIVE)/expected
	@diff -u $(PACKET_WRITE_REFUSAL_CPP_NEGATIVE)/expected $(PACKET_WRITE_REFUSAL_CPP_NEGATIVE)/failed || \
		{ echo "NEGATIVE CONTROL FAILED: moving the upper bound did not fail exactly the one-past cases"; cat $(PACKET_WRITE_REFUSAL_CPP_NEGATIVE)/log; exit 1; }
	@grep -m1 '^FAILED: packet-write-refusal-cpp:' $(PACKET_WRITE_REFUSAL_CPP_NEGATIVE)/log
	@echo "negative control: moving the range assert upper bound turns the C++ write-refusal-packet gate RED on exactly the four one-past cases (a text_long length is also stopped by the zero-byte assert)"
