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

# J6 (docs/PORTING.md) — THE CARD CONFORMANCE CHECK, this leg's half
# (tables/cards/conformance/FORMAT.md). tables/cards/lobster is the facade
# (cards.lobster) and its driver (driver, driver.lobster) over the Card unit
# generated for Lobster; the driver stages both files into the unit's
# directory, where Lobster resolves their imports:
#
#   cards-lobster-generate                the Card unit generated for Lobster,
#                                         the runtime modules beside it
#   cards-lobster-test                    the facade's unit tests (no clock, no
#                                         socket, no process)
#   cards-lobster-conformance             the harness `cards` subcommand over
#                                         the Lobster driver, all nine surfaces
#   cards-lobster-conformance-negative-control, -upgrade-, -hash-
#                                         one damaged answer each, through the
#                                         driver's test-only switches, and the
#                                         check must go red on that surface and
#                                         on no other
#
# They live in this leg's own fragment (the wide-scalar refusal gate discovers
# every make/*.mk as a live port, docs/SPEC-TABLES.md §11). The unit is a
# STAMP, not a phony name: the targets that read it run side by side under -j
# and in sub-makes, and a phony rule's rm -rf would empty the directory under a
# sibling's driver mid-read.
CARDS_LOBSTER_GENERATED := build/tables-generated-lobster/cards
CARDS_REQUIRE_LOBSTER := load,save,render,parse,bounds,upgrade,edit,taskhash,view

.PHONY: cards-lobster-generate cards-lobster-test cards-lobster-conformance cards-lobster-conformance-negative-control cards-lobster-conformance-upgrade-negative-control cards-lobster-conformance-hash-negative-control
cards-lobster-generate: $(CARDS_LOBSTER_GENERATED)/.stamp

$(CARDS_LOBSTER_GENERATED)/.stamp: bin/schema tables/cards/Card.schema tables/cards/schema.lock $(wildcard internal/codegen/lobster/runtime/*.lobster) $(wildcard internal/codegen/lobstertable/runtime/*.lobster)
	@rm -rf $(CARDS_LOBSTER_GENERATED)
	@mkdir -p $(CARDS_LOBSTER_GENERATED)
	./bin/schema generate --lang lobster --out $(CARDS_LOBSTER_GENERATED) tables/cards
	@touch $@

# the unit tests are staged beside the facade, as the driver stages it
cards-lobster-test: lobster-dist-present cards-lobster-generate tables/cards/lobster/cards.lobster tables/cards/lobster/cards_test.lobster
	@cp tables/cards/lobster/cards.lobster $(CARDS_LOBSTER_GENERATED)/.cards.lobster.test && mv -f $(CARDS_LOBSTER_GENERATED)/.cards.lobster.test $(CARDS_LOBSTER_GENERATED)/cards.lobster
	@cp tables/cards/lobster/cards_test.lobster $(CARDS_LOBSTER_GENERATED)/cards_test.lobster
	$(LOBSTER) $(CARDS_LOBSTER_GENERATED)/cards_test.lobster -- $(CURDIR)

cards-lobster-conformance: lobster-dist-present build/conformance-harness cards-lobster-generate
	./build/conformance-harness cards --work build/cards-lobster-conformance --corpus tables/cards/conformance --driver 'tables/cards/lobster/driver' --require $(CARDS_REQUIRE_LOBSTER)

# $(1) the sabotage switch, $(2) the surface that must go red, $(3) the
# surfaces that must stay green, $(4) the work and log name.
define cards-lobster-control
	@if $(1)=1 ./build/conformance-harness cards --work build/cards-lobster-conformance-$(4) --corpus tables/cards/conformance --driver 'tables/cards/lobster/driver' --require $(CARDS_REQUIRE_LOBSTER) > build/cards-lobster-conformance-$(4).log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: the damaged $(2) answer left the card conformance check green'; exit 1; fi
	@grep -qE 'cards $(2) .*FAIL' build/cards-lobster-conformance-$(4).log || { echo 'NEGATIVE CONTROL FAILED: $(2) did not go red'; cat build/cards-lobster-conformance-$(4).log; exit 1; }
	@for s in $(3); do grep -qE "cards $$s pass" build/cards-lobster-conformance-$(4).log || { echo "NEGATIVE CONTROL FAILED: $$s did not stay green"; cat build/cards-lobster-conformance-$(4).log; exit 1; }; done
	@echo "cards Lobster negative control ($(2)): the damaged answer fails $(2) and no other surface"
endef

cards-lobster-conformance-negative-control: lobster-dist-present build/conformance-harness cards-lobster-generate
	$(call cards-lobster-control,CARDS_LOBSTER_SABOTAGE_RENDER,render,load save parse bounds upgrade edit taskhash view,neg)

cards-lobster-conformance-upgrade-negative-control: lobster-dist-present build/conformance-harness cards-lobster-generate
	$(call cards-lobster-control,CARDS_LOBSTER_SABOTAGE_UPGRADE,upgrade,load save render parse bounds edit taskhash view,upgrade-neg)

cards-lobster-conformance-hash-negative-control: lobster-dist-present build/conformance-harness cards-lobster-generate
	$(call cards-lobster-control,CARDS_LOBSTER_SABOTAGE_HASH,taskhash,load save render parse bounds upgrade edit view,hash-neg)

test-lobster: cards-lobster-test cards-lobster-conformance cards-lobster-conformance-negative-control cards-lobster-conformance-upgrade-negative-control cards-lobster-conformance-hash-negative-control

TEST_LEGS         += test-lobster
TOOLCHAIN_LEGS    += lobster
TOOLCHAIN_PINS_lobster := LOBSTER
CONFORMANCE_LEGS  += $(call unless_skipped,lobster,build/conformance-lobster)
BENCH_TABLES_LEGS += generated/bench/tables/lobster/.stamp
GOLDENS_LEGS      += update-goldens-lobster
