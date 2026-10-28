# make/luau.mk — the LUAU leg (docs/CONTRIBUTING.md, "Adding a language";
# docs/PORT-LUAU.md is the port's design).
# Included by the Makefile's wildcard include; the Makefile names no language.
# The leg registers itself at the end of this file.
#
# Luau 0.741, pinned. Generated Luau is self-contained (the runtime modules
# are emitted beside the generated ones), so the interpreter is the only Luau
# dependency. The default points at the repo-local unpacked release; CI may
# install the same version and override with LUAU=luau. To populate dist/
# (gitignored), run `make dist-luau`:
#   Luau 0.741 (luau-lang/luau release, ubuntu x86_64)
#   url:    https://github.com/luau-lang/luau/releases/download/0.741/luau-ubuntu.zip
#   sha256: 134dc762ad26232af83e43f98dec03ff6030dd3a4452f9408b9d50ccea025503
#   unzip into dist/luau-0.741/ (luau, luau-analyze, luau-ast, luau-compile)
LUAU_VERSION  := 0.741
LUAU_ZIP_URL  := https://github.com/luau-lang/luau/releases/download/$(LUAU_VERSION)/luau-ubuntu.zip
LUAU_ZIP_SHA  := 134dc762ad26232af83e43f98dec03ff6030dd3a4452f9408b9d50ccea025503
LUAU_DIST     := $(CURDIR)/dist/luau-$(LUAU_VERSION)
LUAU         ?= $(LUAU_DIST)/luau
LUAU_ANALYZE ?= $(LUAU_DIST)/luau-analyze
export LUAU LUAU_ANALYZE

# dist-luau fetches the pinned release and REFUSES any archive whose sha256 is
# not the pin's: the archive is checked before it is unpacked, so a wrong
# download never reaches dist/.
.PHONY: dist-luau
dist-luau:
	@mkdir -p $(LUAU_DIST) build/dist-luau
	@curl -fsSL -o build/dist-luau/luau-ubuntu.zip $(LUAU_ZIP_URL)
	@got=$$(sha256sum build/dist-luau/luau-ubuntu.zip | cut -d' ' -f1); \
	if [ "$$got" != "$(LUAU_ZIP_SHA)" ]; then \
		echo "dist-luau REFUSES: luau-ubuntu.zip sha256 $$got is not the pin $(LUAU_ZIP_SHA)"; \
		rm -f build/dist-luau/luau-ubuntu.zip; exit 1; fi
	@unzip -o -q build/dist-luau/luau-ubuntu.zip -d $(LUAU_DIST)
	@chmod +x $(LUAU_DIST)/luau $(LUAU_DIST)/luau-analyze $(LUAU_DIST)/luau-ast $(LUAU_DIST)/luau-compile
	@echo "dist-luau: luau $(LUAU_VERSION) at $(LUAU_DIST) sha256 $(LUAU_ZIP_SHA)"

# THE TOOLCHAIN GATE, this leg's half (issue #599; the Makefile's header and
# docs/CONTRIBUTING.md, "Adding a language"): one probe per pin.
.PHONY: toolchain-luau
toolchain-luau:
	@$(call toolchain_probe,luau,LUAU,$(LUAU))

# test-luau runs both Luau packages: the type surface (internal/codegen/luau)
# and the table surface (internal/codegen/luautable), each under the pinned
# interpreter. The luautable tests read the fixed-form corpus, the conformance
# driver's generated modules and the Go runtime, so the leg BUILDS them first
# (docs/PORT-LUAU.md, THE HARNESS): CI and a clean checkout do not stage them.
# SCHEMA_REQUIRE_CORPUS=1 turns a missing oracle into a failure, and the runtime
# reaches the probes as an absolute path whichever checkout the Makefile
# resolved.
.PHONY: test-luau update-goldens-luau
test-luau: toolchain-luau tables-fixedform-corpus \
	packet-finite-luau packet-finite-luau-negative-control \
	$(LUAU_CONFORMANCE_DRIVER)/inputs.luau \
	$(LUAU_CONFORMANCE_DRIVER)/units.luau \
	$(SERIALIZE_GO_RUNTIME)/go.mod
	SCHEMA_REQUIRE_CORPUS=1 SERIALIZE_GO=$(abspath $(SERIALIZE_GO_RUNTIME)) \
		go test -count=1 -timeout 600s ./internal/codegen/luau/... ./internal/codegen/luautable/...
update-goldens-luau: ;

# build/conformance-luau is the manifest's units generated for luau, one
# directory per unit. The schema_leg_luau tag is the target's until the port
# joins the builtins (compiler/target_luau.go). A unit the target refuses is
# recorded as its refusal line, never dropped in silence.
LUAU_MANIFEST := testdata/conformance/tables/MANIFEST.txt
# Every .schema file the manifest's units name: a unit path is either a file or
# a directory of them.
LUAU_CONFORMANCE_UNIT_PATHS := $(shell [ -f $(LUAU_MANIFEST) ] && awk '/^unit[ \t]/ { for (i = 3; i <= NF; i++) print $$i }' $(LUAU_MANIFEST))
LUAU_CONFORMANCE_SCHEMAS := $(foreach p,$(LUAU_CONFORMANCE_UNIT_PATHS),$(if $(filter %.schema,$(p)),$(p),$(wildcard $(p)/*.schema)))
build/conformance-luau: $(LUAU_MANIFEST) make/luau.mk \
	$(wildcard internal/codegen/luautable/runtime/*.luau) \
	$(wildcard internal/codegen/luau/runtime/*) \
	$(GO_SOURCES) \
	$(LUAU_CONFORMANCE_SCHEMAS)
	@rm -rf $@ && mkdir -p $@
	go build -tags schema_leg_luau -o build/schema-luau ./cmd/schema
	@grep '^unit ' $(LUAU_MANIFEST) | while read -r _ key paths; do \
		if ./build/schema-luau generate --lang luau --out $@/$$key $$paths >$@/$$key.log 2>&1; then \
			rm -f $@/$$key.log; \
		else \
			echo "luau refuses $$key: $$(head -1 $@/$$key.log)"; mv $@/$$key.log $@/$$key.refused; \
		fi; \
	done
generated/bench/tables/luau/.stamp: ;

# PACKET FINITE FLOATS (SPEC §4.3, serialize format 2.0). The Luau packet target
# enforces the rule through its vendored serialize.luau runtime (v2.1.1): a NaN
# or an infinity traps on write (IMPLEMENTING.md U9, "must be finite"), refuses
# on read on its raw bits as non_finite_float, and -0 and the subnormals ride
# bit exact. test/packet-finite/luau/main.luau runs every bare-float position
# of Finite.schema at both widths, plus the quantized float's write refusal.
# The generator emits the runtime beside the module, so each control sabotages
# that emitted copy (tools/sabotage, one direction at a time) and must fail
# exactly the vectors its .expected names.
build/packet-finite/luau/.stamp: $(GO_SOURCES) $(wildcard internal/codegen/luau/runtime/*) \
	test/packet-finite/luau/Finite.schema test/packet-finite/luau/main.luau
	@rm -rf build/packet-finite/luau && mkdir -p build/packet-finite/luau
	go build -tags schema_leg_luau -o build/packet-finite/luau/schema ./cmd/schema
	./build/packet-finite/luau/schema generate --lang luau --out build/packet-finite/luau/generated test/packet-finite/luau/Finite.schema
	@cp test/packet-finite/luau/main.luau build/packet-finite/luau/generated/main.luau
	@touch $@

.PHONY: packet-finite-luau packet-finite-luau-negative-control
packet-finite-luau: toolchain-luau build/packet-finite/luau/.stamp
	cd build/packet-finite/luau/generated && $(LUAU_ANALYZE) main.luau Finite.luau
	cd build/packet-finite/luau/generated && $(LUAU) main.luau

# $(1) is the direction: read or write.
define packet-finite-luau-control
	@rm -rf build/packet-finite/luau-negative-$(1) && mkdir -p build/packet-finite/luau-negative-$(1)
	@cp -R build/packet-finite/luau/generated build/packet-finite/luau-negative-$(1)/generated
	go run ./tools/sabotage -name packet-finite-luau-$(1) \
		-out build/packet-finite/luau-negative-$(1)/generated/serialize_float.luau build/packet-finite/luau/generated/serialize_float.luau
	@if (cd build/packet-finite/luau-negative-$(1)/generated && $(LUAU) main.luau) > build/packet-finite/luau-negative-$(1)/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: the Luau runtime float $(1) check was removed and the finite gate stayed green'; exit 1; fi
	@sed -n 's|^FAILED: \([^:]*\):.*|\1|p' build/packet-finite/luau-negative-$(1)/log | LC_ALL=C sort > build/packet-finite/luau-negative-$(1)/failed
	@diff -u test/packet-finite/luau/$(1).expected build/packet-finite/luau-negative-$(1)/failed || \
		{ echo 'NEGATIVE CONTROL FAILED: the Luau float $(1) sabotage did not fail exactly its vectors'; cat build/packet-finite/luau-negative-$(1)/log; exit 1; }
	@echo "packet finite Luau negative control ($(1)): the removed runtime check fails exactly $$(wc -l < build/packet-finite/luau-negative-$(1)/failed | tr -d ' ') vectors"
endef

packet-finite-luau-negative-control: packet-finite-luau
	$(call packet-finite-luau-control,read)
	$(call packet-finite-luau-control,write)

# FINITE TABLE FLOATS (docs/SPEC-TABLES.md §3.3; Glenn, 2026-10-27: "Tables go
# finite-only too, same rule as packets"). Every Luau table read refuses a NaN
# or an infinity on its raw bits as non_finite_float (the variable load,
# tolerant and strict; a retained unknown float; the message batch; the fixed
# form; the cook's open), the block accessor latches one and answers +0, every
# writer refuses one (save, measure, the message writers, the fixed save, the
# cook, the JSON writer), and -0 and the subnormals ride exact. The gate is
# TestLuauTablesAreFiniteOnly with TestLuauPlainFloatSpecialValues; its reversed
# witness removes each rule's check from the emitted runtime in turn and the
# probe must red on exactly that rule's rows.
.PHONY: tables-luau-finite-float tables-luau-finite-float-negative-control
tables-luau-finite-float: toolchain-luau
	sh test/slowgate/proof tables-luau-finite-float \
		'TestLuauTablesAreFiniteOnly TestLuauPlainFloatSpecialValues' \
		./internal/codegen/luautable -run '^(TestLuauTablesAreFiniteOnly|TestLuauPlainFloatSpecialValues)$$' -count=1 -timeout 600s
tables-luau-finite-float-negative-control: toolchain-luau
	sh test/slowgate/proof tables-luau-finite-float-negative-control \
		'TestLuauFiniteFloatNegativeControl' \
		./internal/codegen/luautable -run '^TestLuauFiniteFloatNegativeControl$$' -count=1 -timeout 600s
test-luau: tables-luau-finite-float

TEST_LEGS         += test-luau
TOOLCHAIN_LEGS    += luau
TOOLCHAIN_PINS_luau := LUAU
CONFORMANCE_LEGS  += $(call unless_skipped,luau,build/conformance-luau)
BENCH_TABLES_LEGS += generated/bench/tables/luau/.stamp
GOLDENS_LEGS      += update-goldens-luau
