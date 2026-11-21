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

# THE PINNED COMPILER, fetched when it is missing and then probed: the
# readiness prerequisite (test/conformance/nightly-readiness goose).
.PHONY: goose-toolchain-ready
goose-toolchain-ready:
	@test -x "$(GOOSE)" || $(MAKE) dist-goose
	@$(call toolchain_probe,goose,GOOSE,$(GOOSE))

# THE HARNESS WITH THE TARGET'S TAG: `implementation --lang goose` reads
# compiler.Carried("goose"), which only a schema_leg_goose build registers
# (test/conformance/harness/implementation_goose.go).
build/conformance-harness-goose: $(wildcard test/conformance/harness/*.go) $(GO_SOURCES)
	@mkdir -p build
	go build -tags schema_leg_goose -o $@ ./test/conformance/harness

# test-goose runs both Goose packages: the type surface (internal/codegen/goose)
# and the table surface (internal/codegen/goosetable), each under the pinned
# compiler, AGAINST THE C++ REFERENCE'S BYTES: the fixed form's suite
# (internal/codegen/goosetable/fixedform_test.go) reads every fixed root of
# build/fixedform-corpus and must save it back byte for byte, so the corpus is
# a prerequisite, and SCHEMA_REQUIRE_CORPUS=1 makes a missing oracle a failure
# rather than the skip a bare `go test` takes.
.PHONY: test-goose test-goose-recipe update-goldens-goose
test-goose: toolchain-goose build/fixedform-corpus/.stamp
	$(MAKE) test-goose-recipe
test-goose-recipe: toolchain-goose build/fixedform-corpus/.stamp
	SCHEMA_REQUIRE_CORPUS=1 go test -count=1 -timeout 600s ./internal/codegen/goose/...
	SCHEMA_REQUIRE_CORPUS=1 go test -count=1 -timeout 600s ./internal/codegen/goosetable -run '^TestGoose[A-C]'
	SCHEMA_REQUIRE_CORPUS=1 go test -count=1 -timeout 600s ./internal/codegen/goosetable -run '^TestGoose[D-E]'
	SCHEMA_REQUIRE_CORPUS=1 go test -count=1 -timeout 600s ./internal/codegen/goosetable -run '^TestGooseF'
	SCHEMA_REQUIRE_CORPUS=1 go test -count=1 -timeout 600s ./internal/codegen/goosetable -run '^TestGoose[G-P]'
	SCHEMA_REQUIRE_CORPUS=1 go test -count=1 -timeout 600s ./internal/codegen/goosetable -skip '^TestGoose[A-C]|^TestGoose[D-E]|^TestGooseF|^TestGoose[G-P]'
update-goldens-goose: ;

# THE FIXED FORM'S NEGATIVE CONTROL (make/negative-controls.json, group
# goose): TestGooseFixedFormNegativeControl plants a layout byte, a body byte,
# a count past its bound and a capacity one short into the reference's own
# fixed-form files and requires the Goose reader to refuse each by its reason
# and the oracle comparison to see the planted byte. It runs under
# SCHEMA_REQUIRE_CORPUS=1, so a missing corpus is a failure, never a pass.
\.PHONY: tables-goose-fixedform tables-goose-fixedform-negative-control
tables-goose-fixedform: build/fixedform-corpus/.stamp
	@test -x "$(GOOSE)" || $(MAKE) dist-goose
	SCHEMA_SLOW=1 SCHEMA_REQUIRE_CORPUS=1 go test -count=1 -timeout 600s -run "^(TestGooseFixedFormAgreesWithTheByteOracle|TestGooseFixedProofSuiteStrict|TestGooseFixedProofSuiteStrictNegativeControl)$$" ./internal/codegen/goosetable/

tables-goose-fixedform-negative-control: build/fixedform-corpus/.stamp
	@test -x "$(GOOSE)" || $(MAKE) dist-goose
	SCHEMA_REQUIRE_CORPUS=1 go test -count=1 -run '^TestGooseFixedFormNegativeControl$$' ./internal/codegen/goosetable/

# THE TABLE WIRE'S NEGATIVE CONTROL (make/negative-controls.json, group
# goose): TestGooseTableWireNegativeControl breaks the load's widened count,
# the save's elided-id rollback and the retained tail, one at a time, and
# requires each to turn red only the conformance rows it owns, and at least
# one (internal/codegen/goosetable/tablewire_test.go).
.PHONY: tables-goose-tablewire-negative-control
tables-goose-tablewire-negative-control:
	@test -x "$(GOOSE)" || $(MAKE) dist-goose
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -run '^TestGooseTableWireNegativeControl$$' ./internal/codegen/goosetable/

# build/conformance-goose is the manifest's units generated for goose, one
# directory per unit. The schema_leg_goose tag is the target's until the port
# joins the builtins (compiler/target_goose.go). A unit the target refuses is
# recorded as its refusal line, never dropped in silence. The units move with
# the compiler, the goose runtimes it embeds and the unit schemas, not only with
# the manifest.
GOOSE_MANIFEST := testdata/conformance/tables/MANIFEST.txt
build/conformance-goose: $(GOOSE_MANIFEST) make/goose.mk $(GO_SOURCES) \
	$(filter internal/codegen/goose/% internal/codegen/goosetable/%,$(EMBED_SOURCES)) \
	$(CONFORMANCE_UNIT_SCHEMAS)
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

# THE WHOLE CORPUS OVER AN EMPTY TREE (the Ruby and Python legs' rule, review
# F-086/F-087): every surface, every row, through the harness, with no Goose
# codec at all. The run is green (a missing codec is absence, never a failure
# or a pass) and every answer the driver leaves is an EMPTY <case>.absent.
# Before, `list` refused an empty tree outright, and the block and cook
# surfaces wrote nothing for a unit not generated. The driver's `list` over
# the empty tree is held equal to its `list` over the real one, which the
# goose programs answer.
.PHONY: conformance-goose-empty-tree
conformance-goose-empty-tree: build/conformance-harness build/conformance-cpp build/schema_test_cook build/conformance-goose
	@test -x "$(GOOSE)" || $(MAKE) dist-goose
	@mkdir -p build/conformance-goose-empty-gen
	@test -z "$$(ls -A build/conformance-goose-empty-gen)" || { echo 'build/conformance-goose-empty-gen is not empty'; exit 1; }
	@sh test/conformance/goose/driver $(GOOSE_MANIFEST) list > build/conformance-goose-list-real.txt || \
		{ echo 'conformance-goose-empty-tree: list over the real tree failed'; exit 1; }
	@SCHEMA_GOOSE_GENERATED=build/conformance-goose-empty-gen sh test/conformance/goose/driver $(GOOSE_MANIFEST) list > build/conformance-goose-list-empty.txt || \
		{ echo 'conformance-goose-empty-tree: list refuses an empty tree'; exit 1; }
	@cmp -s build/conformance-goose-list-real.txt build/conformance-goose-list-empty.txt || \
		{ echo 'conformance-goose-empty-tree: list over an empty tree differs from the goose programs list:'; \
		  diff build/conformance-goose-list-real.txt build/conformance-goose-list-empty.txt; exit 1; }
	@SCHEMA_GOOSE_GENERATED=build/conformance-goose-empty-gen \
		./build/conformance-harness run --only goose --work build/conformance-goose-empty > build/conformance-goose-empty.log 2>&1 || \
		{ echo 'conformance-goose-empty-tree: the run over an empty tree is not green'; tail -40 build/conformance-goose-empty.log; exit 1; }
	@answers=$$(find build/conformance-goose-empty/out/goose -type f | wc -l | tr -d ' '); \
	stray=$$(find build/conformance-goose-empty/out/goose -type f \( ! -name '*.absent' -o -size +0 \) | head -5); \
	test "$$answers" -gt 0 || { echo 'conformance-goose-empty-tree: the driver answered nothing'; exit 1; }; \
	test -z "$$stray" || { echo "conformance-goose-empty-tree: answers beside a missing codec that are not an EMPTY .absent: $$stray"; exit 1; }; \
	echo "conformance-goose-empty-tree: $$answers cases, every one an EMPTY .absent, run green"

test-goose: conformance-goose-empty-tree

TEST_LEGS         += test-goose
TOOLCHAIN_LEGS    += goose
TOOLCHAIN_PINS_goose := GOOSE
CONFORMANCE_LEGS  += $(call unless_skipped,goose,build/conformance-goose)
BENCH_TABLES_LEGS += generated/bench/tables/goose/.stamp
GOLDENS_LEGS      += update-goldens-goose

# THE LEG'S CONFORMANCE NEGATIVE CONTROLS (test/conformance/goose/negative-control):
# one clause of the Goose runtime broken in a copy of the generated units, and
# the harness must turn red exactly the surface that clause owns: `load` breaks
# the tolerant load's unknown-field count, so report and retain go red while
# wire and retain-save stay green.
.PHONY: conformance-negative-control-goose
conformance-negative-control-goose: build/conformance-harness build/conformance-goose
	@test -x "$(GOOSE)" || $(MAKE) dist-goose
	sh test/conformance/goose/negative-control load

# Whole-load discard (§4), including retaining reads, and the cyclic wire as
# damage (§3.1). The controls remove the emitted runtime's discard and its
# cycle check, and require the damaged and the cyclic roots to escape.
.PHONY: tables-goose-damage tables-goose-damage-negative-control
tables-goose-damage: goose-toolchain-ready
	SCHEMA_SLOW=1 go test -count=1 ./internal/codegen/goosetable -run '^(TestGooseDamagedLoadReturnsNoRoot|TestGooseCyclicWireIsDamage)$$'
tables-goose-damage-negative-control: goose-toolchain-ready
	SCHEMA_SLOW=1 go test -count=1 ./internal/codegen/goosetable -run '^(TestGooseDamagedLoadNegativeControl|TestGooseCyclicWireNegativeControl)$$'
test-goose: tables-goose-damage tables-goose-damage-negative-control

# THE ENUM-KEYED ARRAY's guard (docs/PORTING.md M5): schema_rt::table_keyed
# indexes a keyed array BY KEY, slot k-1, and refuses None and any key past
# E.Max by name with `abort` in every build; the probe is a `goose build -O2`
# executable. The control takes the None compare, the past-E.Max compare and
# the shift out of the runtime in turn, each turning exactly its own row red.
.PHONY: tables-goose-keyed tables-goose-keyed-negative-control
tables-goose-keyed: goose-toolchain-ready
	SCHEMA_SLOW=1 go test -count=1 ./internal/codegen/goosetable -run '^TestGooseKeyedAccessorCoversBothEnds$$'
tables-goose-keyed-negative-control: goose-toolchain-ready
	SCHEMA_SLOW=1 go test -count=1 ./internal/codegen/goosetable -run '^TestGooseKeyedNegativeControl$$'
test-goose: tables-goose-keyed tables-goose-keyed-negative-control

# A MAP IS A SORTED ENTRY ARRAY (docs/PORTING.md M19): every writer (the wire
# save, the text write, the cook into the holder's node extent) derives the
# ascending order from a value whose maps are reversed in storage and lands on
# the reference's bytes, both readers place ascending, and the wire reader
# refuses a descending key, a repeated key and a foreign key kind. The
# controls take each rule out of the runtime in turn, each turning exactly its
# own rows red.
.PHONY: tables-goose-maps tables-goose-maps-negative-controls
tables-goose-maps: goose-toolchain-ready
	SCHEMA_SLOW=1 go test -count=1 ./internal/codegen/goosetable -run '^TestGooseMapsAreSortedEntryArrays$$'
tables-goose-maps-negative-controls: goose-toolchain-ready
	SCHEMA_SLOW=1 go test -count=1 ./internal/codegen/goosetable -run '^TestGooseMapsNegativeControl$$'
test-goose: tables-goose-maps tables-goose-maps-negative-controls

# The variable-table wire-fuzz verdict pipe (§4.2, §6.6): both the plain
# (--builder) and retaining (--builder --retain) arms agree with the Go engine
# on every mutant's strict read verdict and canonical write-back bytes.
.PHONY: tables-goose-wire-fuzz-verdict tables-goose-wire-fuzz-verdict-negative-control
tables-goose-wire-fuzz-verdict: goose-toolchain-ready
	SCHEMA_SLOW=1 SCHEMA_REQUIRE_CORPUS=1 go test -count=1 ./internal/codegen/goosetable -run '^TestGooseWireFuzzVerdictPipe$$'
tables-goose-wire-fuzz-verdict-negative-control: goose-toolchain-ready
	SCHEMA_SLOW=1 SCHEMA_REQUIRE_CORPUS=1 go test -count=1 ./internal/codegen/goosetable -run '^TestGooseWireFuzzVerdictPipeNegativeControl$$'
test-goose: tables-goose-wire-fuzz-verdict tables-goose-wire-fuzz-verdict-negative-control
