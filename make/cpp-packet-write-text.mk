# THE PACKET WRITE TEXT CONTRACT (docs/SPEC.md §5.1 row 8). The shared cases
# under test/packet-write-text/ are each written in a debug child, so an abort
# is the refusal; a written twin is written, read back and written again, byte
# exact. The schema beside the driver is the shared shape under a type name no
# target's name check refuses.
PACKET_WRITE_TEXT_CPP := build/packet-write-text/cpp
PACKET_WRITE_TEXT_CPP_NEGATIVE := build/packet-write-text/cpp-negative
PACKET_WRITE_TEXT_CPP_SCHEMA := test/packet-write-text/cpp/TextPacket.schema
PACKET_WRITE_TEXT_CPP_CASES := test/packet-write-text/cases.txt

build/packet-write-text/cpp/.stamp: bin/schema test/packet-write-text/cpp/TextPacket.schema make/cpp-packet-write-text.mk
	@mkdir -p $(PACKET_WRITE_TEXT_CPP)
	./bin/schema generate --lang cpp --out $(PACKET_WRITE_TEXT_CPP) $(PACKET_WRITE_TEXT_CPP_SCHEMA)
	@touch $@

build/packet-write-text/cpp/driver: build/packet-write-text/cpp/.stamp test/packet-write-text/cpp/main.cpp make/cpp-packet-write-text.mk
	$(CXX) $(CXXFLAGS) -I$(PACKET_WRITE_TEXT_CPP) test/packet-write-text/cpp/main.cpp -o $@

.PHONY: packet-write-text-cpp
packet-write-text-cpp: build/packet-write-text/cpp/driver
	./$(PACKET_WRITE_TEXT_CPP)/driver $(PACKET_WRITE_TEXT_CPP_CASES)

test: packet-write-text-cpp

# ITS CONTROL removes the emitted UTF-8 write assert through tools/sabotage,
# builds the same driver against that copy, and requires exactly the UTF-8
# cases to go red while the zero-byte case and the written twins stay green.
.PHONY: packet-write-text-cpp-negative-control
packet-write-text-cpp-negative-control: packet-write-text-cpp
	@rm -rf $(PACKET_WRITE_TEXT_CPP_NEGATIVE) && mkdir -p $(PACKET_WRITE_TEXT_CPP_NEGATIVE)
	go run ./tools/sabotage -name packet-write-text-cpp-utf8 \
		-out $(PACKET_WRITE_TEXT_CPP_NEGATIVE)/functions.gotext internal/codegen/cpp/functions.go
	@printf '{"Replace":{"%s/internal/codegen/cpp/functions.go":"%s/%s/functions.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" "$(PACKET_WRITE_TEXT_CPP_NEGATIVE)" > $(PACKET_WRITE_TEXT_CPP_NEGATIVE)/overlay.json
	go run -overlay=$(PACKET_WRITE_TEXT_CPP_NEGATIVE)/overlay.json ./cmd/schema generate --lang cpp \
		--out $(PACKET_WRITE_TEXT_CPP_NEGATIVE)/generated $(PACKET_WRITE_TEXT_CPP_SCHEMA)
	@grep -q 'SABOTAGED: no string UTF-8 write check' $(PACKET_WRITE_TEXT_CPP_NEGATIVE)/generated/TextPacketWire.h || \
		{ echo "NEGATIVE CONTROL FAILED: the UTF-8 write check sabotage did not apply"; exit 1; }
	$(CXX) $(CXXFLAGS) -I$(PACKET_WRITE_TEXT_CPP_NEGATIVE)/generated test/packet-write-text/cpp/main.cpp \
		-o $(PACKET_WRITE_TEXT_CPP_NEGATIVE)/driver
	@if ./$(PACKET_WRITE_TEXT_CPP_NEGATIVE)/driver $(PACKET_WRITE_TEXT_CPP_CASES) > $(PACKET_WRITE_TEXT_CPP_NEGATIVE)/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the packet write text gate stayed green without the UTF-8 write assert"; \
		cat $(PACKET_WRITE_TEXT_CPP_NEGATIVE)/log; exit 1; \
	fi
	@sed -n 's/^FAILED: packet-write-text-cpp: \([^:]*\):.*/\1/p' $(PACKET_WRITE_TEXT_CPP_NEGATIVE)/log | LC_ALL=C sort > $(PACKET_WRITE_TEXT_CPP_NEGATIVE)/failed
	@printf '%s\n' above-unicode bare-continuation invalid-byte overlong surrogate truncated > $(PACKET_WRITE_TEXT_CPP_NEGATIVE)/expected
	@diff -u $(PACKET_WRITE_TEXT_CPP_NEGATIVE)/expected $(PACKET_WRITE_TEXT_CPP_NEGATIVE)/failed || \
		{ echo "NEGATIVE CONTROL FAILED: removing the UTF-8 write assert did not fail exactly the UTF-8 cases"; cat $(PACKET_WRITE_TEXT_CPP_NEGATIVE)/log; exit 1; }
	@grep -m1 '^FAILED: packet-write-text-cpp:' $(PACKET_WRITE_TEXT_CPP_NEGATIVE)/log
	@echo "negative control: removing the string UTF-8 write assert turns the C++ packet text write gate RED on exactly the UTF-8 cases"

