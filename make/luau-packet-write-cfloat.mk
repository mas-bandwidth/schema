
# ---------------------------------------------------------------- compressed float write
define luau-cfloat-tree
	$(call luau-packet-generate,$(1),test/packet-write-cfloat/PacketCFloat.schema,$(2))
	cp test/packet-write-cfloat/luau/main.luau $(1)/main.luau
	awk 'BEGIN {print "return {"} /^[^#[:space:]]/ {v=$$2; if(v=="NaN") v="(0/0)"; else if(v=="+Inf") v="math.huge"; else if(v=="-Inf") v="-math.huge"; expected=$$2; ratio="false"; wire=""; bits="-1"; if(NF%2!=1) {print "malformed case " $$1 > "/dev/stderr"; exit 1} for(i=4;i<NF;i+=2) {if($$i=="reads-back") expected=$$(i+1); else if($$i=="type" && $$(i+1)=="RatioEndpoint") ratio="true"; else if($$i=="wire") wire=tolower($$(i+1)); else if($$i=="bits" && $$(i+1) ~ /^[0-9]+$$/) bits=$$(i+1); else {print "malformed case " $$1 > "/dev/stderr"; exit 1}} if(expected=="NaN" || expected=="+Inf" || expected=="-Inf") expected="0"; sub(/^\+/, "", expected); printf "{ name = \"%s\", value = %s, written = %s, expected = %s, ratio = %s, wire = \"%s\", bits = %s },\n", $$1,v,($$3=="written" ? "true":"false"),expected,ratio,wire,bits} END {print "}"}' test/packet-write-cfloat/cases.txt > $(1)/cases.luau
endef

.PHONY: packet-write-cfloat-luau packet-write-cfloat-luau-negative-control
packet-write-cfloat-luau: toolchain-luau build/packet-luau/schema-luau build/casegate
	$(call luau-cfloat-tree,build/packet-write-cfloat/luau)
	cd build/packet-write-cfloat/luau && $(call packet_write_cfloat_gate,luau,packet-write-cfloat-luau) $(LUAU) main.luau
	cd build/packet-write-cfloat/luau && $(LUAU_ANALYZE) *.luau

packet-write-cfloat-luau-negative-control: packet-write-cfloat-luau
	@mkdir -p build/packet-write-cfloat/luau-negative
	go run ./tools/sabotage -name packet-write-cfloat-luau -out build/packet-write-cfloat/luau-negative/write.gotext internal/codegen/luau/write.go
	@printf '{"Replace":{"%s/internal/codegen/luau/write.go":"%s/build/packet-write-cfloat/luau-negative/write.gotext"}}\n' "$(CURDIR)" "$(CURDIR)" > build/packet-write-cfloat/luau-negative/overlay.json
	go build -overlay=build/packet-write-cfloat/luau-negative/overlay.json -o build/packet-write-cfloat/luau-negative/schema ./cmd/schema
	$(call luau-cfloat-tree,build/packet-write-cfloat/luau-negative/generated,./build/packet-write-cfloat/luau-negative/schema)
	@if (cd build/packet-write-cfloat/luau-negative/generated && $(LUAU) main.luau) > build/packet-write-cfloat/luau-negative/log 2>&1; then echo 'NEGATIVE CONTROL FAILED: compressed float check removed'; exit 1; fi
	@grep -Fq 'FAILED: packet-write-cfloat verdict on ' build/packet-write-cfloat/luau-negative/log || { cat build/packet-write-cfloat/luau-negative/log; exit 1; }
	@echo 'packet-write-cfloat Luau negative control: removed check fails verdict'

test-luau: packet-write-cfloat-luau packet-write-cfloat-luau-negative-control
