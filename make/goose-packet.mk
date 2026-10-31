# make/goose-packet.mk — the Goose PACKET WIRE's gates (docs/PORT-GOOSE.md,
# THE PACKET WIRE; internal/codegen/goose/wire.go). Included by the Makefile's
# wildcard include. The examples/ and examples128/ wire goldens are held by
# the go test test-goose already runs (TestGooseExamplesAgreeWithTheWireGoldens,
# TestGooseLudicrousAgreesWithTheWireGoldens); the shared packet gates the
# nine main legs carry are here, each with its negative control, which builds
# the compiler through an overlay of one named sabotage (tools/sabotage,
# packet_goose.go) and must fail exactly the rows that rule holds:
#
#   packet-utf8-goose      test/packet-text: the narrow string corpus and every
#                          bit flip against the C++ reader
#   packet-wide-goose      test/packet-wide: the wstring corpus, the six interop
#                          cases and the contracts
#   packet-defaults-goose  test/packet-defaults: constructors and the eight C++
#                          byte and bit pins, reused storage
#   packet-finite-goose    test/packet-finite: SPEC §4.3's finite floats, both
#                          directions, a control per direction
#
# The target lives behind the schema_leg_goose tag until the port joins the
# builtins, so every generation here uses build/schema-goose.

.PHONY: goose-packet-ready
goose-packet-ready:
	@test -x "$(GOOSE)" || $(MAKE) dist-goose
	go build -tags schema_leg_goose -o build/schema-goose ./cmd/schema

# $(1) the control directory, $(2) the sabotage, $(3) the emitter file it
# edits: writes $(1)/schema, the compiler with that one rule removed.
define goose-packet-sabotaged
	@mkdir -p $(1)
	go run ./tools/sabotage -name $(2) -out $(1)/emitter.gotext $(3)
	@printf '{"Replace":{"%s/$(3)":"%s/$(1)/emitter.gotext"}}\n' "$(CURDIR)" "$(CURDIR)" > $(1)/overlay.json
	go build -tags schema_leg_goose -overlay=$(1)/overlay.json -o $(1)/schema ./cmd/schema
endef

# ---- packet text: SPEC §4.7's UTF-8 and interior-null refusals ----

.PHONY: packet-utf8-goose packet-utf8-goose-negative-control
packet-utf8-goose: goose-packet-ready build/packet-text/cpp/driver build/packet-text/harness
	@rm -rf build/packet-text/goose && mkdir -p build/packet-text/goose
	./build/schema-goose generate --lang goose --out build/packet-text/goose test/packet-text/Narrow.schema
	cp test/goose/driverlib.goose test/packet-text/goose/main.goose build/packet-text/goose/
	./build/packet-text/harness env $(GOOSE) run build/packet-text/goose/main.goose

packet-utf8-goose-negative-control: packet-utf8-goose
	$(call goose-packet-sabotaged,build/packet-text/goose-negative,packet-utf8-goose-read,internal/codegen/goose/read.go)
	@rm -rf build/packet-text/goose-negative/generated && mkdir -p build/packet-text/goose-negative/generated
	./build/packet-text/goose-negative/schema generate --lang goose --out build/packet-text/goose-negative/generated test/packet-text/Narrow.schema
	cp test/goose/driverlib.goose test/packet-text/goose/main.goose build/packet-text/goose-negative/generated/
	@if ./build/packet-text/harness -mutations-only env $(GOOSE) run build/packet-text/goose-negative/generated/main.goose > build/packet-text/goose-negative/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: Goose UTF-8 removal passed'; exit 1; fi
	@grep -Fq 'FAILED: packet-text verdict on ' build/packet-text/goose-negative/log || { cat build/packet-text/goose-negative/log; exit 1; }
	@echo 'packet UTF-8 Goose negative control: removed read validation fails bit-flip agreement'

# ---- packet wide: SPEC §4.12's code units and surrogate pairing ----

.PHONY: packet-wide-goose packet-wide-goose-negative-control
packet-wide-goose: goose-packet-ready build/packet-wide/source/WideText.schema build/packet-wide/cpp/driver build/packet-text/harness
	@rm -rf build/packet-wide/goose && mkdir -p build/packet-wide/goose
	./build/schema-goose generate --lang goose --out build/packet-wide/goose build/packet-wide/source/WideText.schema
	./build/schema-goose generate --lang goose --out build/packet-wide/goose test/packet-wide/Shapes.schema
	cp test/goose/driverlib.goose test/packet-wide/goose/main.goose build/packet-wide/goose/
	$(GOOSE) run build/packet-wide/goose/main.goose -- --contracts
	./build/packet-text/harness -wide -corpus testdata/conformance/text/wstring.txt -oracle build/packet-wide/cpp/driver env $(GOOSE) run build/packet-wide/goose/main.goose

packet-wide-goose-negative-control: packet-wide-goose
	$(call goose-packet-sabotaged,build/packet-wide/goose-negative,packet-wide-goose-pairing,internal/codegen/goose/read.go)
	@rm -rf build/packet-wide/goose-negative/generated && mkdir -p build/packet-wide/goose-negative/generated
	./build/packet-wide/goose-negative/schema generate --lang goose --out build/packet-wide/goose-negative/generated build/packet-wide/source/WideText.schema
	./build/packet-wide/goose-negative/schema generate --lang goose --out build/packet-wide/goose-negative/generated test/packet-wide/Shapes.schema
	cp test/goose/driverlib.goose test/packet-wide/goose/main.goose build/packet-wide/goose-negative/generated/
	@if ./build/packet-text/harness -wide -corpus testdata/conformance/text/wstring.txt -oracle build/packet-wide/cpp/driver -mutations-only env $(GOOSE) run build/packet-wide/goose-negative/generated/main.goose > build/packet-wide/goose-negative/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: Goose wide pairing removal passed'; exit 1; fi
	@grep -Fq 'FAILED: packet-text verdict on ' build/packet-wide/goose-negative/log || { cat build/packet-wide/goose-negative/log; exit 1; }
	@echo 'packet wide Goose negative control: removed pairing fails bit-flip agreement'

# ---- packet defaults: SPEC §4.2's string, bytes and flags defaults ----

.PHONY: packet-defaults-goose packet-defaults-goose-negative-control
packet-defaults-goose: goose-packet-ready packet-defaults-cpp
	@rm -rf build/packet-defaults/goose && mkdir -p build/packet-defaults/goose
	./build/schema-goose generate --lang goose --out build/packet-defaults/goose test/packet-defaults/Defaults.schema
	./build/schema-goose generate --lang goose --out build/packet-defaults/goose test/packet-defaults/Plain.schema
	cp test/packet-defaults/goose/main.goose build/packet-defaults/goose/
	env -u SCHEMA_UPDATE_WIRE_GOLDENS $(GOOSE) run build/packet-defaults/goose/main.goose -- testdata/wire/packet-defaults

# Removes only the constructor's byte literal, its length kept: the program
# must compile and fail the exact constructor-bytes marker.
packet-defaults-goose-negative-control: packet-defaults-goose
	$(call goose-packet-sabotaged,build/packet-defaults/goose-negative,packet-defaults-goose-constructor-bytes,internal/codegen/goose/goose.go)
	@rm -rf build/packet-defaults/goose-negative/generated && mkdir -p build/packet-defaults/goose-negative/generated
	./build/packet-defaults/goose-negative/schema generate --lang goose --out build/packet-defaults/goose-negative/generated test/packet-defaults/Defaults.schema
	./build/packet-defaults/goose-negative/schema generate --lang goose --out build/packet-defaults/goose-negative/generated test/packet-defaults/Plain.schema
	cp test/packet-defaults/goose/main.goose build/packet-defaults/goose-negative/generated/
	@if env -u SCHEMA_UPDATE_WIRE_GOLDENS $(GOOSE) run build/packet-defaults/goose-negative/generated/main.goose -- testdata/wire/packet-defaults > build/packet-defaults/goose-negative/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: constructor bytes disappeared and the Goose check stayed green'; exit 1; fi
	@grep -Fxq 'FAILED: packet-default constructor bytes' build/packet-defaults/goose-negative/log || \
		{ echo 'NEGATIVE CONTROL FAILED: failure did not name constructor bytes'; cat build/packet-defaults/goose-negative/log; exit 1; }
	@echo 'packet defaults Goose negative control: missing constructor bytes fail the runtime check'

# ---- packet finite: SPEC §4.3's finite floats ----

.PHONY: packet-finite-goose packet-finite-goose-negative-control
packet-finite-goose: goose-packet-ready
	@rm -rf build/packet-finite/goose && mkdir -p build/packet-finite/goose
	./build/schema-goose generate --lang goose --out build/packet-finite/goose test/packet-finite/Finite.schema
	cp test/packet-finite/goose/main.goose build/packet-finite/goose/
	$(GOOSE) run build/packet-finite/goose/main.goose

# One control per direction: each removes that direction's finite rule and
# must fail exactly that direction's 24 vectors (test/packet-finite/goose/
# <direction>.expected); the other direction and every round trip stay green.
# $(1) is the direction: read or write.
define packet-finite-goose-control
	$(call goose-packet-sabotaged,build/packet-finite/goose-negative-$(1),packet-finite-goose-$(1),internal/codegen/goose/$(1).go)
	@rm -rf build/packet-finite/goose-negative-$(1)/generated && mkdir -p build/packet-finite/goose-negative-$(1)/generated
	./build/packet-finite/goose-negative-$(1)/schema generate --lang goose --out build/packet-finite/goose-negative-$(1)/generated test/packet-finite/Finite.schema
	cp test/packet-finite/goose/main.goose build/packet-finite/goose-negative-$(1)/generated/
	@if $(GOOSE) run build/packet-finite/goose-negative-$(1)/generated/main.goose > build/packet-finite/goose-negative-$(1)/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: the Goose $(1) finite rule was removed and the checker stayed green'; exit 1; fi
	@sed -n 's|^FAILED: \([^:]*\):.*|\1|p' build/packet-finite/goose-negative-$(1)/log | LC_ALL=C sort > build/packet-finite/goose-negative-$(1)/failed
	@diff -u test/packet-finite/goose/$(1).expected build/packet-finite/goose-negative-$(1)/failed || \
		{ echo 'NEGATIVE CONTROL FAILED: the Goose $(1) sabotage did not fail exactly its vectors'; cat build/packet-finite/goose-negative-$(1)/log; exit 1; }
	@echo "packet finite Goose negative control ($(1)): the removed rule fails exactly $$(wc -l < build/packet-finite/goose-negative-$(1)/failed | tr -d ' ') vectors"
endef

packet-finite-goose-negative-control: packet-finite-goose
	$(call packet-finite-goose-control,read)
	$(call packet-finite-goose-control,write)

test-goose: packet-utf8-goose packet-utf8-goose-negative-control packet-wide-goose packet-wide-goose-negative-control \
	packet-defaults-goose packet-defaults-goose-negative-control packet-finite-goose packet-finite-goose-negative-control
