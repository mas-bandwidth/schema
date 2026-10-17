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
# interpreter.
.PHONY: test-luau update-goldens-luau
test-luau: toolchain-luau
	go test -count=1 -timeout 600s ./internal/codegen/luau/... ./internal/codegen/luautable/...
update-goldens-luau: ;

# build/conformance-luau is the manifest's units generated for luau, one
# directory per unit. The schema_leg_luau tag is the target's until the port
# joins the builtins (compiler/target_luau.go). A unit the target refuses is
# recorded as its refusal line, never dropped in silence.
LUAU_MANIFEST := testdata/conformance/tables/MANIFEST.txt
build/conformance-luau: $(LUAU_MANIFEST) make/luau.mk
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

TEST_LEGS         += test-luau
TOOLCHAIN_LEGS    += luau
TOOLCHAIN_PINS_luau := LUAU
CONFORMANCE_LEGS  += $(call unless_skipped,luau,build/conformance-luau)
BENCH_TABLES_LEGS += generated/bench/tables/luau/.stamp
GOLDENS_LEGS      += update-goldens-luau
