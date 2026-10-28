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
	$(LUAU_CONFORMANCE_DRIVER)/inputs.luau \
	$(LUAU_CONFORMANCE_DRIVER)/units.luau \
	$(SERIALIZE_GO_RUNTIME)/go.mod
	SCHEMA_REQUIRE_CORPUS=1 SERIALIZE_GO=$(abspath $(SERIALIZE_GO_RUNTIME)) \
		go test -count=1 -timeout 600s ./internal/codegen/luau/... ./internal/codegen/luautable/...
update-goldens-luau: ;

# THE FIXED FORM AND ITS §5 VERSIONING ROWS, THE TWO GATES EVERY LEG OWES
# (docs/FIXED-FORM-ALGORITHM.md §5.7 step 6, §5.9 #11; ci-fast.yml's leg-gate
# and ci-full.yml's versioning row run them by these names).
#
# tables-luau-fixed-form is the byte gate: every fixed corpus root written back
# to the C++ reference's bytes, the lineage read of the FU pair, each named
# refusal of a lineage entry that will not build, the integrity trailer, the
# layout bounds and every refusal name against schema-go's own reader, and the
# ragged tail. tables-luau-versioning is §5: both read columns of every row of
# docs/FIXED-FORM-VERSIONING-TESTS.md against the reference's bytes and its
# manifest, the floor, the hash, lineage_merge and the families
# (internal/codegen/luautable/fixedversioning_*_test.go).
#
# `go test ./...` ASSERTS NOTHING HERE: the harness skips without the corpus,
# which is right for a bare run and is how a §5 regression would ride into a
# green. So each target builds the oracle first and sets SCHEMA_REQUIRE_CORPUS=1,
# under which the skip is a FAILURE.

.PHONY: tables-luau-fixed-form tables-luau-versioning
tables-luau-fixed-form: toolchain-luau tables-fixedform-corpus $(SERIALIZE_GO_RUNTIME)/go.mod
	SCHEMA_REQUIRE_CORPUS=1 SERIALIZE_GO=$(abspath $(SERIALIZE_GO_RUNTIME)) \
		go test ./internal/codegen/luautable/ -count=1 -run '^(TestLuauFixedFormAgreesWithTheByteOracle|TestLuauFixedFormVersioning|TestLuauFixedFormRefusals|TestLuauFixedFormNegativeControl|TestLuauFixedFormRaggedTail|TestLuauFixedFormIntegrity|TestLuauFixedLayoutBounds|TestLuauFixedLoadNamesEveryRefusal|TestLuauFixedPlainReadIsStrict)$$'
	@echo 'tables Luau fixed form: every corpus root on the C++ reference bytes, every refusal by name'

tables-luau-versioning: toolchain-luau tables-fixedform-corpus
	SCHEMA_REQUIRE_CORPUS=1 go test ./internal/codegen/luautable/ -count=1 -run '^(TestLuauFixedVersioning.*|TestLuauFixedCompiledPlanTagPastArmSet)$$'
	@echo 'tables Luau versioning: §5 read both columns of every row against the C++ reference bytes'

# THE TWO GATES' NEGATIVE CONTROLS (§5.9 #11: a gate nobody has watched fail may
# be comparing a file with itself), registered in make/negative-controls.json's
# luau group. Each replaces ONE line of the emitted runtime for one run through
# `go test -overlay` (the runtime reaches the generator by go:embed, which the
# overlay covers), so the tree is never touched, and each requires its gate red
# for the reason it plants and no other.
#
# The fixed-form control writes every text length one too long, so the writer
# leaves the reference's bytes; the versioning control answers a hash outside
# the lineage as layout_malformed, the one name that carries §5's direction, so
# the OLD-REFUSES-NEW column must go red naming layout_unknown.
.PHONY: tables-luau-fixed-form-negative-control tables-luau-versioning-negative-control
tables-luau-fixed-form-negative-control: toolchain-luau tables-fixedform-corpus
	@rm -rf build/luau-fixed-nc && mkdir -p build/luau-fixed-nc
	@sed 's|buffer.writeu32(buf, n.off, length)|buffer.writeu32(buf, n.off, length + 1) -- SABOTAGED|' \
		internal/codegen/luautable/runtime/schema_fixed.luau > build/luau-fixed-nc/schema_fixed.luau
	@test "$$(grep -c SABOTAGED build/luau-fixed-nc/schema_fixed.luau)" -ge 1 || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage matched nothing — the runtime moved"; exit 1; }
	@printf '{"Replace":{"%s/internal/codegen/luautable/runtime/schema_fixed.luau":"%s/build/luau-fixed-nc/schema_fixed.luau"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/luau-fixed-nc/overlay.json
	@if SCHEMA_REQUIRE_CORPUS=1 go test -overlay build/luau-fixed-nc/overlay.json ./internal/codegen/luautable/ \
			-count=1 -run '^TestLuauFixedFormAgreesWithTheByteOracle$$' > build/luau-fixed-nc/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a text length one off left the fixed form green"; \
		cat build/luau-fixed-nc/log; exit 1; \
	fi
	@grep -q 'differs at byte' build/luau-fixed-nc/log || \
		{ echo "NEGATIVE CONTROL FAILED: the fixed form went red for another reason"; \
		  cat build/luau-fixed-nc/log; exit 1; }
	@grep -m1 'differs at byte' build/luau-fixed-nc/log
	@echo "negative control: one byte off the Luau writer reds the reference byte match"

tables-luau-versioning-negative-control: toolchain-luau tables-fixedform-corpus
	@rm -rf build/luau-versioning-nc && mkdir -p build/luau-versioning-nc
	@sed 's|return no("layout_unknown")|return no("layout_malformed") -- SABOTAGED|' \
		internal/codegen/luautable/runtime/schema_fixed.luau > build/luau-versioning-nc/schema_fixed.luau
	@grep -q SABOTAGED build/luau-versioning-nc/schema_fixed.luau || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage matched nothing — the runtime moved"; exit 1; }
	@printf '{"Replace":{"%s/internal/codegen/luautable/runtime/schema_fixed.luau":"%s/build/luau-versioning-nc/schema_fixed.luau"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/luau-versioning-nc/overlay.json
	@if SCHEMA_REQUIRE_CORPUS=1 go test -overlay build/luau-versioning-nc/overlay.json ./internal/codegen/luautable/ \
			-count=1 -run '^TestLuauFixedVersioningOldRefusesNew$$' > build/luau-versioning-nc/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the wrong refusal name left the versioning gate green"; \
		cat build/luau-versioning-nc/log; exit 1; \
	fi
	@grep -q 'owes layout_unknown, not layout_malformed' build/luau-versioning-nc/log || \
		{ echo "NEGATIVE CONTROL FAILED: the versioning gate went red for another reason"; \
		  cat build/luau-versioning-nc/log; exit 1; }
	@echo "negative control: layout_unknown answered as layout_malformed reds the OLD-REFUSES-NEW column"

# the leg runs both gates' controls beside its packages
test-luau: tables-luau-fixed-form-negative-control tables-luau-versioning-negative-control

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

TEST_LEGS         += test-luau
TOOLCHAIN_LEGS    += luau
TOOLCHAIN_PINS_luau := LUAU
CONFORMANCE_LEGS  += $(call unless_skipped,luau,build/conformance-luau)
BENCH_TABLES_LEGS += generated/bench/tables/luau/.stamp
GOLDENS_LEGS      += update-goldens-luau
