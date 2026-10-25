# make/lobster.mk — the LOBSTER leg (docs/CONTRIBUTING.md, "Adding a
# language"; docs/PORT-LOBSTER.md is the port's design).
# Included by the Makefile's wildcard include; the Makefile names no language.
# The leg registers itself at the end of this file.
#
# Lobster v2026.8, pinned (aardappel/lobster tag v2026.8, commit 0921f7dd5745).
# Generated Lobster is self-contained (the runtime modules are emitted beside
# the generated ones), so the toolchain is the only Lobster dependency. The
# default points at the repo-local unpacked release; the fleet installs the
# same pin and may override with LOBSTER=<path>. To populate dist/
# (gitignored), run `make dist-lobster`:
#   url:    https://github.com/aardappel/lobster/releases/download/v2026.8/lobster_linux_release.zip
#   sha256: 2f03fb96fdaf6935469c99e43ffb182cab0d797faec7b26eccc9a732df32ad31
#   unzip into dist/lobster-v2026.8/ (bin/lobster, modules/ beside it)
LOBSTER_VERSION := v2026.8
LOBSTER_ZIP_URL := https://github.com/aardappel/lobster/releases/download/$(LOBSTER_VERSION)/lobster_linux_release.zip
LOBSTER_ZIP_SHA := 2f03fb96fdaf6935469c99e43ffb182cab0d797faec7b26eccc9a732df32ad31
LOBSTER_DIST    := $(CURDIR)/dist/lobster-$(LOBSTER_VERSION)
LOBSTER         ?= $(LOBSTER_DIST)/bin/lobster
export LOBSTER

# dist-lobster fetches the pinned release and REFUSES any archive whose sha256
# is not the pin's, by name: the archive is checked before it is unpacked, so
# a wrong download never reaches dist/.
.PHONY: dist-lobster
dist-lobster:
	@mkdir -p $(LOBSTER_DIST) build/dist-lobster
	@curl -fsSL -o build/dist-lobster/lobster_linux_release.zip $(LOBSTER_ZIP_URL)
	@got=$$(sha256sum build/dist-lobster/lobster_linux_release.zip | cut -d' ' -f1); \
	if [ "$$got" != "$(LOBSTER_ZIP_SHA)" ]; then \
		echo "dist-lobster REFUSES: lobster_linux_release.zip sha256 $$got is not the pin $(LOBSTER_ZIP_SHA)"; \
		rm -f build/dist-lobster/lobster_linux_release.zip; exit 1; fi
	@unzip -o -q build/dist-lobster/lobster_linux_release.zip 'bin/*' 'modules/*' -d $(LOBSTER_DIST)
	@chmod +x $(LOBSTER_DIST)/bin/lobster
	@echo "dist-lobster: lobster $(LOBSTER_VERSION) at $(LOBSTER_DIST) sha256 $(LOBSTER_ZIP_SHA)"

# THE TOOLCHAIN GATE, this leg's half (issue #599; the Makefile's header and
# docs/CONTRIBUTING.md, "Adding a language"): one probe per pin.
.PHONY: toolchain-lobster
toolchain-lobster:
	@$(call toolchain_probe,lobster,LOBSTER,$(LOBSTER))

# test-lobster runs both Lobster packages: the type surface
# (internal/codegen/lobster) and the table surface
# (internal/codegen/lobstertable), each under the pinned toolchain. The
# fixed-form tests read the shared byte-oracle corpus, so it is built first,
# and SCHEMA_REQUIRE_CORPUS=1 makes a missing oracle a failure rather than the
# skip a bare `go test` takes.
.PHONY: test-lobster update-goldens-lobster
test-lobster: toolchain-lobster build/fixedform-corpus/.stamp
	SCHEMA_REQUIRE_CORPUS=1 go test -count=1 -timeout 600s ./internal/codegen/lobster/... ./internal/codegen/lobstertable/...
update-goldens-lobster: ;

# build/conformance-lobster is the manifest's units generated for lobster, one
# directory per unit. The schema_leg_lobster tag is the target's until the
# port joins the builtins (compiler/target_lobster.go). A unit the target
# refuses is recorded as its refusal line, never dropped in silence.
LOBSTER_MANIFEST := testdata/conformance/tables/MANIFEST.txt
build/conformance-lobster: $(LOBSTER_MANIFEST) make/lobster.mk
	@rm -rf $@ && mkdir -p $@
	go build -tags schema_leg_lobster -o build/schema-lobster ./cmd/schema
	@grep '^unit ' $(LOBSTER_MANIFEST) | while read -r _ key paths; do \
		if ./build/schema-lobster generate --lang lobster --out $@/$$key $$paths >$@/$$key.log 2>&1; then \
			rm -f $@/$$key.log; \
		else \
			echo "lobster refuses $$key: $$(head -1 $@/$$key.log)"; mv $@/$$key.log $@/$$key.refused; \
		fi; \
	done
generated/bench/tables/lobster/.stamp: ;

TEST_LEGS         += test-lobster
TOOLCHAIN_LEGS    += lobster
TOOLCHAIN_PINS_lobster := LOBSTER
CONFORMANCE_LEGS  += $(call unless_skipped,lobster,build/conformance-lobster)
BENCH_TABLES_LEGS += generated/bench/tables/lobster/.stamp
GOLDENS_LEGS      += update-goldens-lobster
