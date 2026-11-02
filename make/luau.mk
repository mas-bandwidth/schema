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

# THE STRICT TEXT READ (docs/SPEC-TABLES.md §6.5, §2.3): J.FromJson is the
# plain name and refuses what the strict read refuses, J.FromJsonTolerant keeps
# the recovered value, and both count a missing required field. The gate,
# TestLuauJsonPlainReadIsStrict, runs inside test-luau against schema-go's
# generated FromJSON and FromJSONTolerant; its control plants the plain name
# made tolerant and the required close removed in a scratch copy of the
# emitted runtime and requires each to red exactly its own cases.
.PHONY: tables-luau-json-strict-negative-control
tables-luau-json-strict-negative-control: toolchain-luau $(SERIALIZE_GO_RUNTIME)/go.mod
	SERIALIZE_GO=$(abspath $(SERIALIZE_GO_RUNTIME)) sh test/slowgate/proof tables-luau-json-strict-negative-control \
		'TestLuauJsonStrictNegativeControl' \
		./internal/codegen/luautable -run '^TestLuauJsonStrictNegativeControl$$' -count=1 -timeout 600s

# I13, THE TEXT DIFFERENTIAL (docs/PORTING.md): the emitted ToJson and save
# against the compiler's own engine (internal/tabletext) over random
# float-only instances, both directions, at the Go cell's seed and seed 1, a
# variable root and a fixed root. The row runs inside test-luau; its control
# writes a float32 at six significant digits in a scratch runtime and requires
# red on the text comparison.
.PHONY: tables-luau-text-differential tables-luau-text-differential-negative-control
tables-luau-text-differential: toolchain-luau
	sh test/slowgate/proof tables-luau-text-differential \
		'TestLuauTextDifferentialAgainstTheGoEngine' \
		./internal/codegen/luautable -run '^TestLuauTextDifferentialAgainstTheGoEngine$$' -count=1 -timeout 600s
tables-luau-text-differential-negative-control: toolchain-luau
	sh test/slowgate/proof tables-luau-text-differential-negative-control \
		'TestLuauTextDifferentialNegativeControl' \
		./internal/codegen/luautable -run '^TestLuauTextDifferentialNegativeControl$$' -count=1 -timeout 600s

# M9, THE GENERIC-WALK GATE (docs/PORTING.md): the Luau text walk is one
# runtime module, so every manifest unit that carries the text form must emit
# it byte for byte as the embedded schema_json.luau spells it, and no
# generated module may define a walk of its own. Pure Go, no interpreter; it
# runs inside test-luau. The control plants an edited walk in one unit and a
# second walk in another unit's table module and requires exactly those two.
.PHONY: tables-luau-json-walk tables-luau-json-walk-negative-control
tables-luau-json-walk:
	sh test/slowgate/proof tables-luau-json-walk 'TestLuauJsonWalkIsOneRuntime' \
		./internal/codegen/luautable -run '^TestLuauJsonWalkIsOneRuntime$$' -count=1 -timeout 600s
tables-luau-json-walk-negative-control:
	sh test/slowgate/proof tables-luau-json-walk-negative-control 'TestLuauJsonWalkNegativeControl' \
		./internal/codegen/luautable -run '^TestLuauJsonWalkNegativeControl$$' -count=1 -timeout 600s

# §16.12, THE OUTSIDE ORACLE: the emitted walk's JSON layer over every
# vendored JSONTestSuite file against the standard's table (PENDING allowed and
# listed), and the byte-order mark through the plain FromJson. Its control
# lifts the depth cap in a scratch runtime and requires exactly
# i_structure_500_nested_arrays.json named.
.PHONY: tables-luau-jsontestsuite tables-luau-jsontestsuite-negative-control
tables-luau-jsontestsuite: toolchain-luau
	sh test/slowgate/proof tables-luau-jsontestsuite 'TestLuauJSONTestSuite' \
		./internal/codegen/luautable -run '^TestLuauJSONTestSuite$$' -count=1 -timeout 600s
tables-luau-jsontestsuite-negative-control: toolchain-luau
	sh test/slowgate/proof tables-luau-jsontestsuite-negative-control 'TestLuauJSONTestSuiteNegativeControl' \
		./internal/codegen/luautable -run '^TestLuauJSONTestSuiteNegativeControl$$' -count=1 -timeout 600s

test-luau: tables-luau-json-walk tables-luau-json-walk-negative-control \
	tables-luau-text-differential tables-luau-text-differential-negative-control \
	tables-luau-jsontestsuite tables-luau-jsontestsuite-negative-control \
	tables-luau-json-strict-negative-control

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
# directory per unit. A unit the target refuses is
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
	go build -o build/schema-luau ./cmd/schema
	@grep '^unit ' $(LUAU_MANIFEST) | while read -r _ key paths; do \
		if ./build/schema-luau generate --lang luau --out $@/$$key $$paths >$@/$$key.log 2>&1; then \
			rm -f $@/$$key.log; \
		else \
			echo "luau refuses $$key: $$(head -1 $@/$$key.log)"; mv $@/$$key.log $@/$$key.refused; \
		fi; \
	done
# THE BENCH UNITS (bench/README.md, bench/tables/README.md; docs/PORT-LUAU.md,
# THE HARNESS). The packet leg (bench/luau/run) measures BenchMixed through
# generated/bench/luau; the tables leg (bench/tables/luau/leg) measures
# TableMixed on the tolerant wire through generated/bench/tables/luau. Each unit
# is self-contained: the runtime modules are emitted beside it. The trees are
# committed, like every port's, and test/generated-tree/verify holds them to the
# compiler. The gate and its control are make/luau-bench.mk's.
generated/bench/luau/.stamp: bin/schema $(SCHEMAS_BENCH)
	@mkdir -p generated/bench/luau
	./bin/schema generate --lang luau --out generated/bench/luau bench/corpus/Bench.schema
	@touch $@

generated/bench/tables/luau/.stamp: bin/schema bench/corpus/BenchTable.schema
	@mkdir -p generated/bench/tables/luau
	./bin/schema generate --lang luau --out generated/bench/tables/luau bench/corpus/BenchTable.schema
	@touch $@

# PACKET FINITE FLOATS (SPEC §4.3, serialize format 2.0). The Luau packet target
# enforces the rule through its vendored serialize.luau runtime (v2.1.3): a NaN
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
	go build -o build/packet-finite/luau/schema ./cmd/schema
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

# J6: native Luau owning card facade. The Go host supplies only CLI file IO;
# parser/render/upgrade/edit/hash/view execute under the pinned interpreter.
CARDS_LUAU_GENERATED := build/tables-generated-luau/cards
CARDS_LUAU_REQUIRE := load,save,render,parse,bounds,upgrade,edit,taskhash,view
export CARDS_LUAU_GENERATED
.PHONY: cards-luau-generate cards-luau-test cards-luau-conformance cards-luau-conformance-negative-control cards-luau-conformance-upgrade-negative-control cards-luau-conformance-hash-negative-control
cards-luau-generate: $(CARDS_LUAU_GENERATED)/.stamp
$(CARDS_LUAU_GENERATED)/.stamp: bin/schema tables/cards/Card.schema tables/cards/schema.lock
	@mkdir -p $(CARDS_LUAU_GENERATED)
	./bin/schema generate --lang luau --out $(CARDS_LUAU_GENERATED) tables/cards
	@touch $@

build/cards-driver-luau: tables/cards/luau/host/main.go
	@mkdir -p build
	go build -o $@ tables/cards/luau/host/main.go

cards-luau-test: toolchain-luau cards-luau-generate build/cards-driver-luau cards-corpus-check
	@mkdir -p build/cards-luau-analyze
	@cp $(CARDS_LUAU_GENERATED)/*.luau tables/cards/luau/*.luau build/cards-luau-analyze/
	@printf 'return {surface="unit",rows={},files={}}\n' > build/cards-luau-analyze/inputs.luau
	$(LUAU_ANALYZE) build/cards-luau-analyze/cards.luau build/cards-luau-analyze/cards_codec.luau build/cards-luau-analyze/cards_test.luau build/cards-luau-analyze/driver.luau
	./build/cards-driver-luau tables/cards/luau/UNIT_INPUTS.txt unit build/cards-luau-unit
	@cat build/cards-luau-unit/unit.txt

cards-luau-conformance: toolchain-luau cards-luau-generate build/cards-driver-luau build/conformance-harness cards-corpus-check
	SCHEMA_REQUIRE_CARDS_CORPUS=1 SCHEMA_CARDS_SURFACES=$(CARDS_LUAU_REQUIRE) ./build/conformance-harness cards --work build/cards-luau-conformance --corpus tables/cards/conformance --driver tables/cards/luau/driver --require $(CARDS_LUAU_REQUIRE)

cards-luau-conformance-negative-control: cards-luau-test cards-luau-conformance
	sh tables/cards/luau/negative-control.sh render
cards-luau-conformance-upgrade-negative-control: cards-luau-test cards-luau-conformance
	sh tables/cards/luau/negative-control.sh upgrade
cards-luau-conformance-hash-negative-control: cards-luau-test cards-luau-conformance
	sh tables/cards/luau/negative-control.sh taskhash

test-luau: cards-luau-test cards-luau-conformance
