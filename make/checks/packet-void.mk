# SPEC §4.8 / #503: packet payload-free arms carry exactly their tag bits.
#
# ONE SHARED CORPUS (2026-11-24). testdata/wire/packet-void/cases.txt names
# every case once; the pins beside it are the Go oracle's bytes
# (packet-void-pins) and the hand-made refusals (its README.md). Every leg's
# driver, test/packet-void/<leg>/, reads cases.txt and answers each case on a
# line of its own; tools/casegate runs the driver and refuses a case with no
# answer, a FAILED answer, and an owed line (owed.txt) for a case the leg
# answers ok. internal/inputfamilies.Check refuses a leg with no
# packet-void-<leg> target and no `<leg> all owed:` line.
PACKET_VOID_SCHEMA := test/packet-void/Void.schema
PACKET_VOID_PINS := testdata/wire/packet-void
CASEGATE := build/casegate

build/casegate: $(wildcard tools/casegate/*.go)
	@mkdir -p build
	go build -o $@ ./tools/casegate

# packet_void_gate: $(1) the leg, then the driver command.
packet_void_gate = $(CASEGATE) -name packet-void-$(1) -leg $(1) -cases $(PACKET_VOID_PINS)/cases.txt -owed $(PACKET_VOID_PINS)/owed.txt --

# The Go oracle writes every accept pin; the refuse pins are hand-made.
# Pin updates are explicit; ordinary runs only compare checked-in bytes.
build/packet-void/go/.stamp: bin/schema $(PACKET_VOID_SCHEMA) make/checks/packet-void.mk
	@rm -rf build/packet-void/go && mkdir -p build/packet-void/go
	./bin/schema generate --lang go --out build/packet-void/go/packetvoid $(PACKET_VOID_SCHEMA)
	@printf 'module packetvoid\n\ngo 1.23\n' > build/packet-void/go/packetvoid/go.mod
	@touch $@

.PHONY: packet-void-go packet-void-pins
packet-void-go: build/packet-void/go/.stamp build/casegate
	cd test/packet-void/go && go build -o ../../../build/packet-void/go/driver .
	$(call packet_void_gate,go) ./build/packet-void/go/driver $(PACKET_VOID_PINS)

packet-void-pins: build/packet-void/go/.stamp
	cd test/packet-void/go && go run . ../../../$(PACKET_VOID_PINS) -write-pins

build/packet-void/cpp/.stamp: bin/schema $(PACKET_VOID_SCHEMA)
	@mkdir -p build/packet-void/cpp
	./bin/schema generate --lang cpp --out build/packet-void/cpp $(PACKET_VOID_SCHEMA)
	@touch $@

build/packet-void/c/.stamp: bin/schema $(PACKET_VOID_SCHEMA)
	@mkdir -p build/packet-void/c
	./bin/schema generate --lang c --out build/packet-void/c $(PACKET_VOID_SCHEMA)
	@touch $@

build/packet-void/cpp-test: build/packet-void/cpp/.stamp test/packet-void/cpp/main.cpp $(SERIALIZE)/serialize.h
	$(CXX) $(CXXFLAGS) -pedantic-errors -Ibuild/packet-void/cpp test/packet-void/cpp/main.cpp -o $@

build/packet-void/c-test: build/packet-void/c/.stamp test/packet-void/c/main.c $(SERIALIZE_C)/serialize.h $(SERIALIZE_C)/serialize.c
	$(CC) -std=c99 -Wall -Wextra -Werror -Wtype-limits $(C_TAUTOLOGICAL) \
		-O2 -ffp-contract=off -Ibuild/packet-void/c -I$(SERIALIZE_C) \
		test/packet-void/c/main.c $(SERIALIZE_C)/serialize.c -o $@ -lm

.PHONY: packet-void-cpp packet-void-c
packet-void-cpp: build/packet-void/cpp-test build/casegate
	$(call packet_void_gate,cpp) ./build/packet-void/cpp-test $(PACKET_VOID_PINS)

packet-void-c: build/packet-void/c-test build/casegate
	$(call packet_void_gate,c) ./build/packet-void/c-test $(PACKET_VOID_PINS)

test: packet-void-cpp
test-c: packet-void-c
test-go: packet-void-go

# Each control builds an emitter copy, then the unchanged driver. The
# sabotage adds one bit to a void arm on just the write or the read path:
# the unaffected direction must answer every case ok, and the altered one
# must fail mixed-ping on its exact bit count. The read uses the Go oracle's
# pin, so a matching writer/reader error cannot hide.
define packet_void_prepare
	@mkdir -p build/packet-void/$(1)-$(2)
	go run ./tools/sabotage -name packet-void-$(2)-$(1) \
		-out build/packet-void/$(1)-$(2)/emitter.gotext $(3)
	@printf '{"Replace":{"%s/$(3)":"%s/build/packet-void/$(1)-$(2)/emitter.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/packet-void/$(1)-$(2)/overlay.json
	go build -overlay=build/packet-void/$(1)-$(2)/overlay.json -o build/packet-void/$(1)-$(2)/schema ./cmd/schema
	./build/packet-void/$(1)-$(2)/schema generate --lang $(1) \
		--out build/packet-void/$(1)-$(2)/generated $(PACKET_VOID_SCHEMA)
endef

define packet_void_expect
	./build/packet-void/$(1)-$(2)/test $(PACKET_VOID_PINS) $(if $(filter write,$(2)),read,write) > build/packet-void/$(1)-$(2)/other.log
	@if grep -q FAILED build/packet-void/$(1)-$(2)/other.log; then \
		echo 'NEGATIVE CONTROL FAILED: $(1) $(2) sabotage broke the other direction'; cat build/packet-void/$(1)-$(2)/other.log; exit 1; fi
	./build/packet-void/$(1)-$(2)/test $(PACKET_VOID_PINS) $(2) > build/packet-void/$(1)-$(2)/runtime.log
	@grep -Fxq 'case mixed-ping FAILED: $(2) has 3 bits, want 2' build/packet-void/$(1)-$(2)/runtime.log || \
		{ echo 'NEGATIVE CONTROL FAILED: an extra void $(2) bit in $(1) did not fail mixed-ping on its bit count'; cat build/packet-void/$(1)-$(2)/runtime.log; exit 1; }
	@grep -Fx 'case mixed-ping FAILED: $(2) has 3 bits, want 2' build/packet-void/$(1)-$(2)/runtime.log
endef

.PHONY: packet-void-cpp-write-negative-control packet-void-cpp-read-negative-control
.PHONY: packet-void-c-write-negative-control packet-void-c-read-negative-control

packet-void-cpp-write-negative-control packet-void-cpp-read-negative-control: packet-void-cpp
	$(call packet_void_prepare,cpp,$(if $(findstring -write-,$@),write,read),internal/codegen/cpp/functions.go)
	$(CXX) $(CXXFLAGS) -pedantic-errors -Ibuild/packet-void/cpp-$(if $(findstring -write-,$@),write,read)/generated \
		test/packet-void/cpp/main.cpp -o build/packet-void/cpp-$(if $(findstring -write-,$@),write,read)/test
	$(call packet_void_expect,cpp,$(if $(findstring -write-,$@),write,read))

packet-void-c-write-negative-control packet-void-c-read-negative-control: packet-void-c
	$(call packet_void_prepare,c,$(if $(findstring -write-,$@),write,read),internal/codegen/c/c.go)
	$(CC) -std=c99 -Wall -Wextra -Werror -Wtype-limits $(C_TAUTOLOGICAL) \
		-O2 -ffp-contract=off -Ibuild/packet-void/c-$(if $(findstring -write-,$@),write,read)/generated -I$(SERIALIZE_C) \
		test/packet-void/c/main.c $(SERIALIZE_C)/serialize.c -o build/packet-void/c-$(if $(findstring -write-,$@),write,read)/test -lm
	$(call packet_void_expect,c,$(if $(findstring -write-,$@),write,read))

test: packet-void-cpp-write-negative-control packet-void-cpp-read-negative-control
test-c: packet-void-c-write-negative-control packet-void-c-read-negative-control

# Every leg runs its packet-void driver per pull request, in its verify row
# (cpp, c, python and ruby list theirs in make/verify-*.mk).
_verify-go-conformance: packet-void-go
_verify-rust-conformance: packet-void-rust
_verify-js-conformance: packet-void-js
_verify-cs-conformance: packet-void-cs
_verify-unity-conformance: packet-void-unity
_verify-java-conformance: packet-void-java
_verify-dart-conformance: packet-void-dart
_verify-elixir-conformance: packet-void-elixir
_verify-lua-conformance: packet-void-lua
_verify-luau-conformance: packet-void-luau
_verify-moo-conformance: packet-void-moo
_verify-squirrel-conformance: packet-void-squirrel
_verify-goose-conformance: packet-void-goose
_verify-lobster-conformance: packet-void-lobster
