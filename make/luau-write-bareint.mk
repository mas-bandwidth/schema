# ---------------------------------------------------------------- bare integers
# The shared bare-integer write cases (test/packet-write-bareint, SPEC §5.1
# row 11): a value outside a bare intN/uintN field's width, or not an
# integer, is refused by the generated writer (false, never a raise); each
# width's min and max is written and read back exactly. cases.luau carries
# the shared list in as source (the interpreter has no io library).
define luau-packet-write-bareint-tree
	$(call luau-packet-generate,$(1),test/packet-write-bareint/BareInt.schema,$(2))
	cp test/packet-write-bareint/luau/main.luau $(1)/main.luau
	{ printf 'return {\n'; \
	  awk '!/^#/ && NF == 4 { printf "\t{ name = \"%s\", type = \"%s\", value = \"%s\", verdict = \"%s\" },\n", $$1, $$2, $$3, $$4 }' \
		test/packet-write-bareint/cases.txt; \
	  printf '}\n'; } > $(1)/cases.luau
endef

build/packet-write-bareint/luau/.stamp: $(LUAU_PACKET_SCHEMA) test/packet-write-bareint/BareInt.schema \
	test/packet-write-bareint/cases.txt test/packet-write-bareint/luau/main.luau
	$(call luau-packet-write-bareint-tree,build/packet-write-bareint/luau)
	@touch $@

.PHONY: packet-write-bareint-luau packet-write-bareint-luau-negative-control
packet-write-bareint-luau: toolchain-luau build/packet-write-bareint/luau/.stamp
	cd build/packet-write-bareint/luau && $(LUAU) main.luau
	cd build/packet-write-bareint/luau && $(LUAU_ANALYZE) *.luau

# The control removes the width test of a bare signed field of 32 bits or
# fewer: an out-of-width value must then write, and the gate must name that
# refusal.
packet-write-bareint-luau-negative-control: packet-write-bareint-luau
	@rm -rf build/packet-write-bareint/luau-negative && mkdir -p build/packet-write-bareint/luau-negative
	go run ./tools/sabotage -name packet-write-bareint-luau-width -out build/packet-write-bareint/luau-negative/write.gotext internal/codegen/luau/write.go
	@printf '{"Replace":{"%s/internal/codegen/luau/write.go":"%s/build/packet-write-bareint/luau-negative/write.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/packet-write-bareint/luau-negative/overlay.json
	go build -overlay=build/packet-write-bareint/luau-negative/overlay.json -o build/packet-write-bareint/luau-negative/schema ./cmd/schema
	$(call luau-packet-write-bareint-tree,build/packet-write-bareint/luau-negative/generated,./build/packet-write-bareint/luau-negative/schema)
	@if (cd build/packet-write-bareint/luau-negative/generated && $(LUAU) main.luau) > build/packet-write-bareint/luau-negative/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: a removed bare-integer width test passed in Luau'; exit 1; fi
	@grep -Fq 'FAILED: packet-write-bareint int8-above-max refused' build/packet-write-bareint/luau-negative/log || \
		{ echo 'NEGATIVE CONTROL FAILED: Luau failed for another reason'; cat build/packet-write-bareint/luau-negative/log; exit 1; }
	@echo 'packet-write-bareint Luau negative control: a removed width test fails the out-of-width refusal'

test-luau: packet-write-bareint-luau packet-write-bareint-luau-negative-control
