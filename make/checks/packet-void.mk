# SPEC §4.8 / #503: packet payload-free arms carry exactly their tag bits.
# The readers consume hand-calculated constants, independent of the writers.
build/packet-void/cpp/.stamp: bin/schema test/packet-void/Void.schema
	@mkdir -p build/packet-void/cpp
	./bin/schema generate --lang cpp --out build/packet-void/cpp test/packet-void/Void.schema
	@touch $@

build/packet-void/c/.stamp: bin/schema test/packet-void/Void.schema
	@mkdir -p build/packet-void/c
	./bin/schema generate --lang c --out build/packet-void/c test/packet-void/Void.schema
	@touch $@

build/packet-void/cpp-test: build/packet-void/cpp/.stamp test/packet-void/cpp_main.cpp $(SERIALIZE)/serialize.h
	$(CXX) $(CXXFLAGS) -pedantic-errors -Ibuild/packet-void/cpp test/packet-void/cpp_main.cpp -o $@

build/packet-void/c-test: build/packet-void/c/.stamp test/packet-void/c_main.c $(SERIALIZE_C)/serialize.h $(SERIALIZE_C)/serialize.c
	$(CC) -std=c99 -Wall -Wextra -Werror -Wtype-limits $(C_TAUTOLOGICAL) \
		-O2 -ffp-contract=off -Ibuild/packet-void/c -I$(SERIALIZE_C) \
		test/packet-void/c_main.c $(SERIALIZE_C)/serialize.c -o $@ -lm

.PHONY: packet-void-cpp packet-void-c
packet-void-cpp: build/packet-void/cpp-test
	./build/packet-void/cpp-test

packet-void-c: build/packet-void/c-test
	./build/packet-void/c-test

test: packet-void-cpp
test-c: packet-void-c

# Each control builds an emitter copy, then the unchanged consumer harness.
# Only an exact runtime assertion after a successful operation satisfies it.
# A matching writer/reader error cannot hide: the read uses a literal oracle,
# and the control also runs the unchanged direction and requires it to pass.
define packet_void_prepare
	@mkdir -p build/packet-void/$(1)-$(2)
	go run ./tools/sabotage -name packet-void-$(2)-$(1) \
		-out build/packet-void/$(1)-$(2)/emitter.gotext $(3)
	@printf '{"Replace":{"%s/$(3)":"%s/build/packet-void/$(1)-$(2)/emitter.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/packet-void/$(1)-$(2)/overlay.json
	go build -overlay=build/packet-void/$(1)-$(2)/overlay.json -o build/packet-void/$(1)-$(2)/schema ./cmd/schema
	./build/packet-void/$(1)-$(2)/schema generate --lang $(1) \
		--out build/packet-void/$(1)-$(2)/generated test/packet-void/Void.schema
endef

define packet_void_expect
	./build/packet-void/$(1)-$(2)/test $(if $(filter write,$(2)),read,write)
	@if ./build/packet-void/$(1)-$(2)/test $(2) > build/packet-void/$(1)-$(2)/runtime.log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: extra void $(2) bit passed in $(1)'; exit 1; fi
	@grep -Fxq 'FAILED: packet-void $(2) has tag bits only' build/packet-void/$(1)-$(2)/runtime.log || \
		{ echo 'NEGATIVE CONTROL FAILED: $(1) $(2) failed for another reason'; cat build/packet-void/$(1)-$(2)/runtime.log; exit 1; }
	@cat build/packet-void/$(1)-$(2)/runtime.log
endef

.PHONY: packet-void-cpp-write-negative-control packet-void-cpp-read-negative-control
.PHONY: packet-void-c-write-negative-control packet-void-c-read-negative-control

packet-void-cpp-write-negative-control packet-void-cpp-read-negative-control: packet-void-cpp
	$(call packet_void_prepare,cpp,$(if $(findstring -write-,$@),write,read),internal/codegen/cpp/functions.go)
	$(CXX) $(CXXFLAGS) -pedantic-errors -Ibuild/packet-void/cpp-$(if $(findstring -write-,$@),write,read)/generated \
		test/packet-void/cpp_main.cpp -o build/packet-void/cpp-$(if $(findstring -write-,$@),write,read)/test
	$(call packet_void_expect,cpp,$(if $(findstring -write-,$@),write,read))

packet-void-c-write-negative-control packet-void-c-read-negative-control: packet-void-c
	$(call packet_void_prepare,c,$(if $(findstring -write-,$@),write,read),internal/codegen/c/c.go)
	$(CC) -std=c99 -Wall -Wextra -Werror -Wtype-limits $(C_TAUTOLOGICAL) \
		-O2 -ffp-contract=off -Ibuild/packet-void/c-$(if $(findstring -write-,$@),write,read)/generated -I$(SERIALIZE_C) \
		test/packet-void/c_main.c $(SERIALIZE_C)/serialize.c -o build/packet-void/c-$(if $(findstring -write-,$@),write,read)/test -lm
	$(call packet_void_expect,c,$(if $(findstring -write-,$@),write,read))

test: packet-void-cpp-write-negative-control packet-void-cpp-read-negative-control
test-c: packet-void-c-write-negative-control packet-void-c-read-negative-control
