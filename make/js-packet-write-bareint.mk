# make/js-packet-write-bareint.mk — the packet write of a bare intN / uintN
# (SPEC §5.1 row 11) in the JavaScript leg: the shared list
# (test/packet-write-bareint/cases.txt) through both tiers in both builds,
# plus this leg's own cases: a value of the wrong JavaScript type for its
# width (a Number where a BigInt is required, a string, a boolean, null, an
# object), uint128, and array / union-arm reaches.
#
# The negative control takes each tier's check out in turn through an overlay
# of the emitter (tools/sabotage): first the whole width check, then only the
# BigInt type test of the wide widths, and requires the gate red by name.

build/packet-write-bareint/js/.stamp: bin/schema test/packet-write-bareint/BareInt.schema test/packet-write-bareint/js/Shapes.schema make/js-packet-write-bareint.mk
	./bin/schema generate --lang js --out build/packet-write-bareint/js/bareint test/packet-write-bareint/BareInt.schema
	./bin/schema generate --lang js --out build/packet-write-bareint/js/shapes test/packet-write-bareint/js/Shapes.schema
	@touch $@

# $(1): the sabotage name; $(2): the emitter file it edits (flat or functions);
# $(3): the tier the gate must go red in; $(4): the FAILED line it must print;
# $(5): what the control shows.
define packet_write_bareint_js_control
	@mkdir -p build/packet-write-bareint/js-negative-$(1)
	go run ./tools/sabotage -name $(1) \
		-out build/packet-write-bareint/js-negative-$(1)/$(2).gotext internal/codegen/js/$(2).go
	@printf '{"Replace":{"%s/internal/codegen/js/$(2).go":"%s/build/packet-write-bareint/js-negative-$(1)/$(2).gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/packet-write-bareint/js-negative-$(1)/overlay.json
	go build -overlay=build/packet-write-bareint/js-negative-$(1)/overlay.json -o build/packet-write-bareint/js-negative-$(1)/schema ./cmd/schema
	./build/packet-write-bareint/js-negative-$(1)/schema generate --lang js --out build/packet-write-bareint/js-negative-$(1)/generated/bareint test/packet-write-bareint/BareInt.schema
	./build/packet-write-bareint/js-negative-$(1)/schema generate --lang js --out build/packet-write-bareint/js-negative-$(1)/generated/shapes test/packet-write-bareint/js/Shapes.schema
	$(NODE) --check build/packet-write-bareint/js-negative-$(1)/generated/bareint/BareIntFlat.js
	$(NODE) --check build/packet-write-bareint/js-negative-$(1)/generated/bareint/BareInt.js
	@if $(NODE) test/packet-write-bareint/js/main.mjs test/packet-write-bareint/cases.txt "$(CURDIR)/build/packet-write-bareint/js-negative-$(1)/generated" > build/packet-write-bareint/js-negative-$(1)/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: $(1) passed in the $(3) tier'; exit 1; fi
	@grep -Fq '$(4)' build/packet-write-bareint/js-negative-$(1)/log || \
		{ echo 'NEGATIVE CONTROL FAILED: JavaScript failed for another reason'; cat build/packet-write-bareint/js-negative-$(1)/log; exit 1; }
	@if grep -F 'FAILED: ' build/packet-write-bareint/js-negative-$(1)/log | grep -Fvq 'FAILED: bareint checked/$(3) '; then \
		cat build/packet-write-bareint/js-negative-$(1)/log; echo 'NEGATIVE CONTROL FAILED: a check outside the $(3) tier went red'; exit 1; fi
	@echo 'packet write bare integer JavaScript negative control: $(5)'
endef

.PHONY: packet-write-bareint-js packet-write-bareint-js-negative-control
packet-write-bareint-js: build/packet-write-bareint/js/.stamp
	$(NODE) test/packet-write-bareint/js/main.mjs test/packet-write-bareint/cases.txt build/packet-write-bareint/js
	NODE_ENV=production $(NODE) test/packet-write-bareint/js/main.mjs test/packet-write-bareint/cases.txt build/packet-write-bareint/js

packet-write-bareint-js-negative-control: packet-write-bareint-js
	$(call packet_write_bareint_js_control,packet-write-bareint-js-flat-width,flat,flat,FAILED: bareint checked/flat int8-above-max: wrote a value its width refuses,the flat width check removal reds int8-above-max)
	$(call packet_write_bareint_js_control,packet-write-bareint-js-runtime-width,functions,runtime,FAILED: bareint checked/runtime int8-above-max: wrote a value its width refuses,the runtime width check removal reds int8-above-max)
	$(call packet_write_bareint_js_control,packet-write-bareint-js-flat-bigint-type,flat,flat,FAILED: bareint checked/flat uint64-numeric-string: wrote a value its width refuses,the flat BigInt type test removal reds uint64-numeric-string)
	$(call packet_write_bareint_js_control,packet-write-bareint-js-runtime-bigint-type,functions,runtime,FAILED: bareint checked/runtime int64-numeric-string: wrote a value its width refuses,the runtime BigInt type test removal reds int64-numeric-string)

test-js: packet-write-bareint-js packet-write-bareint-js-negative-control
