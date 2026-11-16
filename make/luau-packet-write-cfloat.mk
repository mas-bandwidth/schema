
# ---------------------------------------------------------------- compressed float write
define luau-cfloat-tree
	$(call luau-packet-generate,$(1),test/packet-write-cfloat/CFloat.schema,$(2))
	cp test/packet-write-cfloat/luau/main.luau $(1)/main.luau
	awk 'BEGIN {print "return {"} /^[^#[:space:]]/ {v=$$2; if(v=="NaN") v="(0/0)"; else if(v=="+Inf") v="math.huge"; else if(v=="-Inf") v="-math.huge"; expected=($$4=="reads-back" ? $$5 : $$2); if(expected=="NaN" || expected=="+Inf" || expected=="-Inf") expected="0"; sub(/^\+/, "", expected); printf "{ name = \"%s\", value = %s, written = %s, expected = %s },\n", $$1,v,($$3=="written" ? "true":"false"),expected} END {print "}"}' test/packet-write-cfloat/cases.txt > $(1)/cases.luau
endef

.PHONY: packet-write-cfloat-luau packet-write-cfloat-luau-negative-control
packet-write-cfloat-luau: toolchain-luau build/packet-luau/schema-luau
	$(call luau-cfloat-tree,build/packet-write-cfloat/luau)
	cd build/packet-write-cfloat/luau && $(LUAU) main.luau
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
