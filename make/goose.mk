# make/goose.mk — the GOOSE leg (docs/CONTRIBUTING.md, "Adding a language";
# docs/PORT-GOOSE.md is the port's design).
# Included by the Makefile's wildcard include; the Makefile names no language.
# The leg registers itself at the end of this file.
#
# Goose v2026.2, pinned (aardappel/goose tag v2026.2, commit 7ad96d6d47f9).
# Generated Goose is self-contained (the runtime modules are emitted beside
# the generated ones), so the compiler is the only Goose dependency: `goose
# run` compiles into its own process with the bundled TinyCC, `goose build`
# with the machine's C compiler. The default points at the repo-local
# unpacked release; the fleet installs the same pin and may override with
# GOOSE=<path>. To populate dist/ (gitignored), run `make dist-goose`:
#   Goose v2026.2 (aardappel/goose release, linux x64)
#   url:    https://github.com/aardappel/goose/releases/download/v2026.2/goose-v2026.2-linux-x64.tar.gz
#   sha256: f9f0b448800b27b9d86fcd8341a08e8cda790ff86cd500c90e287fa1a616197c
#   untar into dist/ (dist/goose-v2026.2-linux-x64/goose, its stdlib/ and
#   tcclib/ beside it)
GOOSE_VERSION := v2026.2
GOOSE_TGZ     := goose-$(GOOSE_VERSION)-linux-x64.tar.gz
GOOSE_TGZ_URL := https://github.com/aardappel/goose/releases/download/$(GOOSE_VERSION)/$(GOOSE_TGZ)
GOOSE_TGZ_SHA := f9f0b448800b27b9d86fcd8341a08e8cda790ff86cd500c90e287fa1a616197c
GOOSE_DIST    := $(CURDIR)/dist/goose-$(GOOSE_VERSION)-linux-x64
GOOSE        ?= $(GOOSE_DIST)/goose
export GOOSE

# dist-goose fetches the pinned release and REFUSES any archive whose sha256
# is not the pin's, by name: the archive is checked before it is unpacked, so
# a wrong download never reaches dist/.
.PHONY: dist-goose
dist-goose:
	@mkdir -p dist build/dist-goose
	@curl -fsSL -o build/dist-goose/$(GOOSE_TGZ) $(GOOSE_TGZ_URL)
	@got=$$(sha256sum build/dist-goose/$(GOOSE_TGZ) | cut -d' ' -f1); \
	if [ "$$got" != "$(GOOSE_TGZ_SHA)" ]; then \
		echo "dist-goose REFUSES: $(GOOSE_TGZ) sha256 $$got is not the pin $(GOOSE_TGZ_SHA)"; \
		rm -f build/dist-goose/$(GOOSE_TGZ); exit 1; fi
	@tar -xzf build/dist-goose/$(GOOSE_TGZ) -C dist
	@test -x $(GOOSE_DIST)/goose
	@echo "dist-goose: goose $(GOOSE_VERSION) at $(GOOSE_DIST) sha256 $(GOOSE_TGZ_SHA)"

# THE TOOLCHAIN GATE, this leg's half (issue #599; the Makefile's header and
# docs/CONTRIBUTING.md, "Adding a language"): one probe per pin.
.PHONY: toolchain-goose
toolchain-goose:
	@$(call toolchain_probe,goose,GOOSE,$(GOOSE))

# test-goose runs both Goose packages: the type surface (internal/codegen/goose)
# and the table surface (internal/codegen/goosetable), each under the pinned
# compiler, AGAINST THE C++ REFERENCE'S BYTES: the fixed form's suite
# (internal/codegen/goosetable/fixedform_test.go) reads every fixed root of
# build/fixedform-corpus and must save it back byte for byte, so the corpus is
# a prerequisite, and SCHEMA_REQUIRE_CORPUS=1 makes a missing oracle a failure
# rather than the skip a bare `go test` takes.
.PHONY: test-goose update-goldens-goose
test-goose: toolchain-goose build/fixedform-corpus/.stamp
	SCHEMA_REQUIRE_CORPUS=1 go test -count=1 -timeout 600s ./internal/codegen/goose/... ./internal/codegen/goosetable/...
update-goldens-goose: ;

# THE FIXED FORM'S NEGATIVE CONTROL (make/negative-controls.json, group
# goose): TestGooseFixedFormNegativeControl plants a layout byte, a body byte,
# a count past its bound and a capacity one short into the reference's own
# fixed-form files and requires the Goose reader to refuse each by its reason
# and the oracle comparison to see the planted byte. It runs under
# SCHEMA_REQUIRE_CORPUS=1, so a missing corpus is a failure, never a pass.
.PHONY: tables-goose-fixedform-negative-control
tables-goose-fixedform-negative-control: build/fixedform-corpus/.stamp
	@test -x "$(GOOSE)" || $(MAKE) dist-goose
	SCHEMA_REQUIRE_CORPUS=1 go test -count=1 -run '^TestGooseFixedFormNegativeControl$$' ./internal/codegen/goosetable/

# build/conformance-goose is the manifest's units generated for goose, one
# directory per unit. The schema_leg_goose tag is the target's until the port
# joins the builtins (compiler/target_goose.go). A unit the target refuses is
# recorded as its refusal line, never dropped in silence.
GOOSE_MANIFEST := testdata/conformance/tables/MANIFEST.txt
build/conformance-goose: $(GOOSE_MANIFEST) make/goose.mk
	@rm -rf $@ && mkdir -p $@
	go build -tags schema_leg_goose -o build/schema-goose ./cmd/schema
	@grep '^unit ' $(GOOSE_MANIFEST) | while read -r _ key paths; do \
		if ./build/schema-goose generate --lang goose --out $@/$$key $$paths >$@/$$key.log 2>&1; then \
			rm -f $@/$$key.log; \
		else \
			echo "goose refuses $$key: $$(head -1 $@/$$key.log)"; mv $@/$$key.log $@/$$key.refused; \
		fi; \
	done
generated/bench/tables/goose/.stamp: ;

TEST_LEGS         += test-goose
TOOLCHAIN_LEGS    += goose
TOOLCHAIN_PINS_goose := GOOSE
CONFORMANCE_LEGS  += $(call unless_skipped,goose,build/conformance-goose)
BENCH_TABLES_LEGS += generated/bench/tables/goose/.stamp
GOLDENS_LEGS      += update-goldens-goose
