# ---------------------------------------------------------------- write text
# The shared packet Write text cases (test/packet-write-text, SPEC §5.1
# contract row 8): every ill-formed string(8) is refused by WriteTextPacket in
# every build, and every well-formed one is written as its canonical packet
# and read back byte exact. cases.luau carries cases.txt in as source (the
# interpreter has no io library).
define luau-packet-write-text-tree
	$(call luau-packet-generate,$(1),test/packet-write-text/Text.schema,$(2))
	cp test/packet-write-text/luau/main.luau $(1)/main.luau
	{ printf 'return [==[\n'; cat test/packet-write-text/cases.txt; printf ']==]\n'; } > $(1)/cases.luau
endef

build/packet-write-text/luau/.stamp: $(LUAU_PACKET_SCHEMA) test/packet-write-text/Text.schema \
	test/packet-write-text/cases.txt test/packet-write-text/luau/main.luau
	$(call luau-packet-write-text-tree,build/packet-write-text/luau)
	@touch $@

.PHONY: packet-write-text-luau packet-write-text-luau-negative-control
packet-write-text-luau: toolchain-luau build/packet-write-text/luau/.stamp
	cd build/packet-write-text/luau && $(LUAU) main.luau
	cd build/packet-write-text/luau && $(LUAU_ANALYZE) *.luau

packet-write-text-luau-negative-control: packet-write-text-luau
	@rm -rf build/packet-write-text/luau-negative && mkdir -p build/packet-write-text/luau-negative
	go run ./tools/sabotage -name packet-write-text-luau-write -out build/packet-write-text/luau-negative/write.gotext internal/codegen/luau/write.go
	@printf '{"Replace":{"%s/internal/codegen/luau/write.go":"%s/build/packet-write-text/luau-negative/write.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/packet-write-text/luau-negative/overlay.json
	go build -overlay=build/packet-write-text/luau-negative/overlay.json -o build/packet-write-text/luau-negative/schema ./cmd/schema
	$(call luau-packet-write-text-tree,build/packet-write-text/luau-negative/generated,./build/packet-write-text/luau-negative/schema)
	@if (cd build/packet-write-text/luau-negative/generated && $(LUAU) main.luau) > build/packet-write-text/luau-negative/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: a writer without text checks passed in Luau'; exit 1; fi
	@grep -Fq 'FAILED: packet-write-text invalid-byte refused' build/packet-write-text/luau-negative/log || \
		{ echo 'NEGATIVE CONTROL FAILED: Luau failed for another reason'; cat build/packet-write-text/luau-negative/log; exit 1; }
	@grep -Fq 'FAILED: packet-write-text interior-null refused' build/packet-write-text/luau-negative/log || \
		{ echo 'NEGATIVE CONTROL FAILED: Luau failed for another reason'; cat build/packet-write-text/luau-negative/log; exit 1; }
	@echo 'packet-write-text Luau negative control: a writer without text checks fails the ill-formed refusals'

test-luau: packet-write-text-luau packet-write-text-luau-negative-control
