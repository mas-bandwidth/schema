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
# (gitignored), run `make dist-lobster`, which takes the host's release archive
# from https://github.com/aardappel/lobster/releases/tag/v2026.8 and unzips it
# into dist/lobster-v2026.8/ (bin/lobster, with modules/ and data/ beside it; --pak reads data/shaders). One sha256 per
# archive, each the release's own published digest:
#   lobster_linux_release.zip    2f03fb96fdaf6935469c99e43ffb182cab0d797faec7b26eccc9a732df32ad31
#   lobster_osx_arm_release.zip  18088b1bfdfa4e93f972b806e240aa2eece6528a183124884e74b698c8218f31
#   lobster_osx_x64_release.zip  ab6c5e2489a64e0ff5d84fa508c065640738e2bd95ebabbfa50258000d85f584
# The macOS archives are what certify.yml's macos-latest `make test` installs.
LOBSTER_VERSION := v2026.8
LOBSTER_HOST    := $(shell uname -s)-$(shell uname -m)
ifeq ($(LOBSTER_HOST),Darwin-arm64)
LOBSTER_ZIP     := lobster_osx_arm_release.zip
LOBSTER_ZIP_SHA := 18088b1bfdfa4e93f972b806e240aa2eece6528a183124884e74b698c8218f31
else ifeq ($(LOBSTER_HOST),Darwin-x86_64)
LOBSTER_ZIP     := lobster_osx_x64_release.zip
LOBSTER_ZIP_SHA := ab6c5e2489a64e0ff5d84fa508c065640738e2bd95ebabbfa50258000d85f584
else
LOBSTER_ZIP     := lobster_linux_release.zip
LOBSTER_ZIP_SHA := 2f03fb96fdaf6935469c99e43ffb182cab0d797faec7b26eccc9a732df32ad31
endif
LOBSTER_ZIP_URL := https://github.com/aardappel/lobster/releases/download/$(LOBSTER_VERSION)/$(LOBSTER_ZIP)
LOBSTER_DIST    := $(CURDIR)/dist/lobster-$(LOBSTER_VERSION)
LOBSTER         ?= $(LOBSTER_DIST)/bin/lobster
export LOBSTER

# dist-lobster fetches the pinned release and REFUSES any archive whose sha256
# is not the pin's, by name: the archive is checked before it is unpacked, so
# a wrong download never reaches dist/.
.PHONY: dist-lobster
dist-lobster:
	@mkdir -p $(LOBSTER_DIST) build/dist-lobster
	@curl -fsSL -o build/dist-lobster/$(LOBSTER_ZIP) $(LOBSTER_ZIP_URL)
	@got=$$( { sha256sum build/dist-lobster/$(LOBSTER_ZIP) 2>/dev/null || shasum -a 256 build/dist-lobster/$(LOBSTER_ZIP); } | cut -d' ' -f1); \
	if [ "$$got" != "$(LOBSTER_ZIP_SHA)" ]; then \
		echo "dist-lobster REFUSES: $(LOBSTER_ZIP) sha256 $$got is not the pin $(LOBSTER_ZIP_SHA)"; \
		rm -f build/dist-lobster/$(LOBSTER_ZIP); exit 1; fi
	@unzip -o -q build/dist-lobster/$(LOBSTER_ZIP) 'bin/*' 'modules/*' 'data/*' -d $(LOBSTER_DIST)
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
# directory per unit.
# port joins the builtins (compiler/target_lobster.go). A unit the target
# refuses is recorded as its refusal line, never dropped in silence.
LOBSTER_MANIFEST := testdata/conformance/tables/MANIFEST.txt
build/conformance-lobster: $(LOBSTER_MANIFEST) make/lobster.mk
	@rm -rf $@ && mkdir -p $@
	go build -o build/schema-lobster ./cmd/schema
	@grep '^unit ' $(LOBSTER_MANIFEST) | while read -r _ key paths; do \
		if ./build/schema-lobster generate --lang lobster --out $@/$$key $$paths >$@/$$key.log 2>&1; then \
			rm -f $@/$$key.log; \
		else \
			echo "lobster refuses $$key: $$(head -1 $@/$$key.log)"; mv $@/$$key.log $@/$$key.refused; \
		fi; \
	done
# THE BENCH UNITS (bench/README.md, bench/tables/README.md; docs/PORT-LOBSTER.md,
# THE HARNESS). The packet leg (bench/lobster/run) measures BenchMixed through
# generated/bench/lobster; the tables leg (bench/tables/lobster/leg) measures
# TableMixed on the tolerant wire through generated/bench/tables/lobster. Each
# unit is self-contained: the runtime modules are emitted beside it. The trees
# are committed, like every port's, and test/generated-tree/verify holds them to
# the compiler.
generated/bench/lobster/.stamp: bin/schema $(SCHEMAS_BENCH)
	@mkdir -p generated/bench/lobster
	./bin/schema generate --lang lobster --out generated/bench/lobster bench/corpus/Bench.schema
	@touch $@

generated/bench/tables/lobster/.stamp: bin/schema bench/corpus/BenchTable.schema
	@mkdir -p generated/bench/tables/lobster
	./bin/schema generate --lang lobster --out generated/bench/tables/lobster bench/corpus/BenchTable.schema
	@touch $@


TEST_LEGS         += test-lobster
TOOLCHAIN_LEGS    += lobster
TOOLCHAIN_PINS_lobster := LOBSTER
CONFORMANCE_LEGS  += $(call unless_skipped,lobster,build/conformance-lobster)
BENCH_TABLES_LEGS += generated/bench/tables/lobster/.stamp
GOLDENS_LEGS      += update-goldens-lobster
