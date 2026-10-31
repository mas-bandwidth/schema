# make/luau-packet.mk — the Luau leg's packet-runtime gates, beside the nine
# ports' own (test/packet-text, test/packet-wide, test/packet-defaults):
#
#   packet-utf8-luau      the shared string corpus and every bit flip of it,
#                         verdicts, code units, consumed bits and canonical
#                         bytes against the C++ oracle (build/packet-text/harness)
#   packet-wide-luau      the wstring corpus and the six SPEC §4.12 interop
#                         cases the same way, then the writer contract and the
#                         composition rules (--contracts)
#   packet-defaults-luau  constructors, Init, the eight C++ pins of
#                         testdata/wire/packet-defaults and reused storage
#
# Each gate ends with luau-analyze (--!strict, zero diagnostics) over the
# generated modules and its driver.
#
# Each has a negative control: one named edit (tools/sabotage) applied to the
# Luau generator through a go build -overlay, regenerated, and required to
# fail its exact marker. The Luau drivers cannot read stdin (the interpreter
# has no io library, docs/PORT-LUAU.md THE HARNESS): the harness talks to
# test/packet-text/luau/stdin.sh, which carries the lines in as source.
# test-luau depends on all six.

LUAU_PACKET_SCHEMA := build/packet-luau/schema-luau
LUAU_PACKET_STDIN  := test/packet-text/luau/stdin.sh

$(LUAU_PACKET_SCHEMA): $(GO_SOURCES)
	@mkdir -p $(dir $@)
	go build -o $@ ./cmd/schema

# luau-packet-generate DIR SCHEMA [SCHEMA_BIN]: a fresh generated directory.
define luau-packet-generate
	rm -rf $(1) && mkdir -p $(1)
	$(or $(3),$(LUAU_PACKET_SCHEMA)) generate --lang luau --out $(1) $(2)
endef

# ---------------------------------------------------------------- UTF-8
build/packet-text/luau/.stamp: $(LUAU_PACKET_SCHEMA) test/packet-text/Narrow.schema test/packet-text/luau/main.luau
	$(call luau-packet-generate,build/packet-text/luau,test/packet-text/Narrow.schema)
	cp test/packet-text/luau/main.luau build/packet-text/luau/main.luau
	@touch $@

.PHONY: packet-utf8-luau packet-utf8-luau-negative-control
packet-utf8-luau: toolchain-luau build/packet-text/luau/.stamp build/packet-text/cpp/driver build/packet-text/harness
	./build/packet-text/harness sh $(LUAU_PACKET_STDIN) build/packet-text/luau main.luau
	cd build/packet-text/luau && $(LUAU_ANALYZE) *.luau

packet-utf8-luau-negative-control: packet-utf8-luau
	@rm -rf build/packet-text/luau-negative && mkdir -p build/packet-text/luau-negative
	go run ./tools/sabotage -name packet-utf8-luau-read -out build/packet-text/luau-negative/read.gotext internal/codegen/luau/read.go
	@printf '{"Replace":{"%s/internal/codegen/luau/read.go":"%s/build/packet-text/luau-negative/read.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/packet-text/luau-negative/overlay.json
	go build -overlay=build/packet-text/luau-negative/overlay.json -o build/packet-text/luau-negative/schema ./cmd/schema
	$(call luau-packet-generate,build/packet-text/luau-negative/generated,test/packet-text/Narrow.schema,./build/packet-text/luau-negative/schema)
	cp test/packet-text/luau/main.luau build/packet-text/luau-negative/generated/main.luau
	@if ./build/packet-text/harness -mutations-only sh $(LUAU_PACKET_STDIN) build/packet-text/luau-negative/generated main.luau \
		> build/packet-text/luau-negative/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: Luau UTF-8 removal passed'; exit 1; fi
	@grep -Fq 'FAILED: packet-text verdict on ' build/packet-text/luau-negative/log || \
		{ cat build/packet-text/luau-negative/log; exit 1; }
	@echo 'packet UTF-8 Luau negative control: removed read validation fails bit-flip agreement'

# ---------------------------------------------------------------- wide text
build/packet-wide/luau/.stamp: $(LUAU_PACKET_SCHEMA) build/packet-wide/source/WideText.schema test/packet-wide/Shapes.schema test/packet-wide/luau/main.luau
	$(call luau-packet-generate,build/packet-wide/luau,build/packet-wide/source/WideText.schema)
	$(call luau-packet-generate,build/packet-wide/luau/shapes,test/packet-wide/Shapes.schema)
	cp test/packet-wide/luau/main.luau build/packet-wide/luau/main.luau
	@touch $@

.PHONY: packet-wide-luau packet-wide-luau-negative-control
packet-wide-luau: toolchain-luau build/packet-wide/luau/.stamp build/packet-wide/cpp/driver build/packet-text/harness
	sh $(LUAU_PACKET_STDIN) build/packet-wide/luau main.luau --contracts </dev/null
	./build/packet-text/harness -wide -corpus testdata/conformance/text/wstring.txt -oracle build/packet-wide/cpp/driver \
		sh $(LUAU_PACKET_STDIN) build/packet-wide/luau main.luau
	cd build/packet-wide/luau && $(LUAU_ANALYZE) *.luau shapes/*.luau

packet-wide-luau-negative-control: packet-wide-luau
	@rm -rf build/packet-wide/luau-negative && mkdir -p build/packet-wide/luau-negative
	go run ./tools/sabotage -name packet-wide-luau-pairing -out build/packet-wide/luau-negative/read.gotext internal/codegen/luau/read.go
	@printf '{"Replace":{"%s/internal/codegen/luau/read.go":"%s/build/packet-wide/luau-negative/read.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/packet-wide/luau-negative/overlay.json
	go build -overlay=build/packet-wide/luau-negative/overlay.json -o build/packet-wide/luau-negative/schema ./cmd/schema
	$(call luau-packet-generate,build/packet-wide/luau-negative/generated,build/packet-wide/source/WideText.schema,./build/packet-wide/luau-negative/schema)
	cp test/packet-wide/luau/main.luau build/packet-wide/luau-negative/generated/main.luau
	@if ./build/packet-text/harness -wide -mutations-only -corpus testdata/conformance/text/wstring.txt -oracle build/packet-wide/cpp/driver \
		sh $(LUAU_PACKET_STDIN) build/packet-wide/luau-negative/generated main.luau > build/packet-wide/luau-negative/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: Luau surrogate pairing removal passed'; exit 1; fi
	@grep -Fq 'FAILED: packet-text verdict on ' build/packet-wide/luau-negative/log || \
		{ cat build/packet-wide/luau-negative/log; exit 1; }
	@echo 'packet wide Luau negative control: removed surrogate pairing fails bit-flip agreement'

# ---------------------------------------------------------------- defaults
# pins.luau carries testdata/wire/packet-defaults in as source: one entry per
# pin, its bytes as hex and its consumed bits.
LUAU_PACKET_DEFAULT_PINS := $(wildcard testdata/wire/packet-defaults/*.bin testdata/wire/packet-defaults/*.bits)
define luau-packet-defaults-tree
	$(call luau-packet-generate,$(1),test/packet-defaults/Defaults.schema,$(2))
	$(or $(2),$(LUAU_PACKET_SCHEMA)) generate --lang luau --out $(1)/plain test/packet-defaults/Plain.schema
	cp test/packet-defaults/luau/main.luau $(1)/main.luau
	{ printf 'return {\n'; \
	  for f in testdata/wire/packet-defaults/*.bin; do \
		n=$$(basename $$f .bin); \
		printf '\t["%s"] = { hex = "%s", bits = %s },\n' "$$n" "$$(od -An -v -tx1 $$f | tr -d ' \n')" "$$(tr -d ' \n' < testdata/wire/packet-defaults/$$n.bits)"; \
	  done; printf '}\n'; } > $(1)/pins.luau
endef

build/packet-defaults/luau/.stamp: $(LUAU_PACKET_SCHEMA) test/packet-defaults/Defaults.schema test/packet-defaults/Plain.schema \
	test/packet-defaults/luau/main.luau $(LUAU_PACKET_DEFAULT_PINS)
	$(call luau-packet-defaults-tree,build/packet-defaults/luau)
	@touch $@

.PHONY: packet-defaults-luau packet-defaults-luau-negative-control
packet-defaults-luau: toolchain-luau build/packet-defaults/luau/.stamp
	cd build/packet-defaults/luau && $(LUAU) main.luau
	cd build/packet-defaults/luau && $(LUAU_ANALYZE) *.luau plain/*.luau

packet-defaults-luau-negative-control: packet-defaults-luau
	@rm -rf build/packet-defaults/luau-negative && mkdir -p build/packet-defaults/luau-negative
	go run ./tools/sabotage -name packet-defaults-luau-constructor-bytes -out build/packet-defaults/luau-negative/luau.gotext internal/codegen/luau/luau.go
	@printf '{"Replace":{"%s/internal/codegen/luau/luau.go":"%s/build/packet-defaults/luau-negative/luau.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/packet-defaults/luau-negative/overlay.json
	go build -overlay=build/packet-defaults/luau-negative/overlay.json -o build/packet-defaults/luau-negative/schema ./cmd/schema
	$(call luau-packet-defaults-tree,build/packet-defaults/luau-negative/generated,./build/packet-defaults/luau-negative/schema)
	@if (cd build/packet-defaults/luau-negative/generated && $(LUAU) main.luau) > build/packet-defaults/luau-negative/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: missing constructor bytes passed in Luau'; exit 1; fi
	@grep -Fq 'FAILED: packet-default constructor bytes' build/packet-defaults/luau-negative/log || \
		{ echo 'NEGATIVE CONTROL FAILED: Luau failed for another reason'; cat build/packet-defaults/luau-negative/log; exit 1; }
	@echo 'packet defaults Luau negative control: missing constructor bytes fail the constructor check'

test-luau: packet-utf8-luau packet-utf8-luau-negative-control \
	packet-wide-luau packet-wide-luau-negative-control \
	packet-defaults-luau packet-defaults-luau-negative-control

# ---------------------------------------------------------------- ludicrous
# test/luau-ludicrous/main.luau, the twin of test/js-ludicrous and
# test/ludicrous_main.cpp: examples128 written byte for byte against the
# C++-pinned goldens, read back, defaults, branch zeroing and hostile
# rejections. pins.luau carries the goldens in as source.
LUAU_LUDICROUS_PINS := ludicrous_state ludicrous_state_untargeted degenerate_probe unsigned_probe
define luau-ludicrous-tree
	$(call luau-packet-generate,$(1),examples128,$(2))
	cp test/luau-ludicrous/main.luau $(1)/main.luau
	{ printf 'return {\n'; \
	  for n in $(LUAU_LUDICROUS_PINS); do \
		printf '\t["%s"] = "%s",\n' "$$n" "$$(od -An -v -tx1 testdata/wire/$$n.bin | tr -d ' \n')"; \
	  done; printf '}\n'; } > $(1)/pins.luau
endef

build/luau-ludicrous/.stamp: $(LUAU_PACKET_SCHEMA) $(SCHEMAS128) test/luau-ludicrous/main.luau \
	$(foreach p,$(LUAU_LUDICROUS_PINS),testdata/wire/$(p).bin)
	$(call luau-ludicrous-tree,build/luau-ludicrous)
	@touch $@

.PHONY: luau-ludicrous luau-ludicrous-negative-control
luau-ludicrous: toolchain-luau build/luau-ludicrous/.stamp
	cd build/luau-ludicrous && $(LUAU) main.luau
	cd build/luau-ludicrous && $(LUAU_ANALYZE) *.luau

# The control removes the writer contract of a fixed(I, F) of 32 bits or
# fewer: the wrong degenerate ufixed raw must then write, and the gate must
# name exactly that refusal.
luau-ludicrous-negative-control: luau-ludicrous
	@rm -rf build/luau-ludicrous-negative && mkdir -p build/luau-ludicrous-negative
	go run ./tools/sabotage -name luau-ludicrous-fixed-contract -out build/luau-ludicrous-negative/write.gotext internal/codegen/luau/write.go
	@printf '{"Replace":{"%s/internal/codegen/luau/write.go":"%s/build/luau-ludicrous-negative/write.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/luau-ludicrous-negative/overlay.json
	go build -overlay=build/luau-ludicrous-negative/overlay.json -o build/luau-ludicrous-negative/schema ./cmd/schema
	$(call luau-ludicrous-tree,build/luau-ludicrous-negative/generated,./build/luau-ludicrous-negative/schema)
	@if (cd build/luau-ludicrous-negative/generated && $(LUAU) main.luau) > build/luau-ludicrous-negative/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: a removed fixed writer contract passed in Luau'; exit 1; fi
	@grep -Fq 'FAILED: a wrong degenerate ufixed raw is REFUSED on write' build/luau-ludicrous-negative/log || \
		{ echo 'NEGATIVE CONTROL FAILED: Luau failed for another reason'; cat build/luau-ludicrous-negative/log; exit 1; }
	@echo 'luau ludicrous negative control: a removed fixed writer contract fails its refusal check'

test-luau: luau-ludicrous luau-ludicrous-negative-control
